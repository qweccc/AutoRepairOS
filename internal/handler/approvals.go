package handler

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/cron"
	"autorepairos/internal/db"
	"autorepairos/internal/integration/line"
	"autorepairos/internal/model"
)

// approvalItem 是追加維修項目在 approval_requests.item_summary 裡的元素。
//
// 為什麼要結構化：原設計把 item_summary 當自由文字，導致客戶同意後無法可靠地
// 還原成工單項目（approvals.go 的 TODO 就是卡在這裡）。存 JSON 讓「客戶同意
// 什麼」與「工單加了什麼」是同一份資料，不會各說各話。
type approvalItem struct {
	Description string  `json:"description"`
	Quantity    int     `json:"quantity"`
	UnitPrice   float64 `json:"unit_price"`
	ItemType    string  `json:"item_type"` // labor / part / subcontract
}

// parseApprovalItems 驗證並解析追加項目 JSON。
//
// 集中在這裡驗證（而不是 UI 與解析端各寫一份）：授權一旦成立就會寫進工單並
// 影響帳務，兩邊規則不一致會產生「客戶同意的內容」與「工單實際項目」不符。
func parseApprovalItems(raw string) ([]approvalItem, error) {
	var items []approvalItem
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("追加項目不可為空")
	}
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("追加項目格式錯誤: %w", err)
	}
	if len(items) == 0 {
		return nil, errors.New("追加項目不可為空")
	}
	for i, it := range items {
		if strings.TrimSpace(it.Description) == "" {
			return nil, fmt.Errorf("第 %d 項缺少品名", i+1)
		}
		if it.Quantity <= 0 {
			return nil, fmt.Errorf("第 %d 項數量必須大於 0", i+1)
		}
		if it.UnitPrice < 0 {
			return nil, fmt.Errorf("第 %d 項單價不可為負", i+1)
		}
		switch it.ItemType {
		case "labor", "part", "subcontract":
		default:
			return nil, fmt.Errorf("第 %d 項類別無效: %q", i+1, it.ItemType)
		}
	}
	return items, nil
}

// CreateApprovalFromWorkOrder 由櫃檯在工單詳情頁建立追加維修授權。
//
// 為什麼需要這支：CreateApprovalRequest 原本沒有任何呼叫點（全 repo 只出現在
// 一行註解裡），所以「產生連結 → 客戶確認 → 工單加入追加項目」這條流程從 UI
// 根本走不完，手冊描述的步驟無法操作。
func CreateApprovalFromWorkOrder(w http.ResponseWriter, r *http.Request) {
	woID := mustInt(r.PathValue("id"))
	woPath := "/work-orders/" + strconv.FormatInt(woID, 10)

	var customerID int64
	var status string
	if err := db.DB.QueryRow(
		`SELECT customer_id, status FROM active_work_orders WHERE id = ?`, woID,
	).Scan(&customerID, &status); err != nil {
		http.NotFound(w, r)
		return
	}
	// 已結帳的工單是財務憑證，不能再追加項目（與 requireOrderMutable 一致）
	if status == "paid" {
		redirectErrMsg(w, r, woPath, "此工單已結帳，不可再追加項目。如需更正請另行開立沖銷單")
		return
	}

	item := approvalItem{
		Description: strings.TrimSpace(r.FormValue("description")),
		Quantity:    atoiDefault(r.FormValue("quantity"), 0),
		UnitPrice:   atofDefault(r.FormValue("unit_price"), 0),
		ItemType:    strings.TrimSpace(r.FormValue("item_type")),
	}
	raw, err := json.Marshal([]approvalItem{item})
	if err != nil {
		fail(w, "建立授權失敗")
		return
	}
	// 用同一支驗證函式，UI 與解析端不會各有一套規則
	if _, err := parseApprovalItems(string(raw)); err != nil {
		redirectErrMsg(w, r, woPath, err.Error())
		return
	}

	req, err := CreateApprovalRequest(woID, customerID, string(raw))
	if err != nil {
		log.Printf("建立追加維修授權失敗（工單 #%d）: %v", woID, err)
		redirectErrMsg(w, r, woPath, "建立授權失敗："+err.Error())
		return
	}

	// 推播給客戶。走通知佇列（與其他通知一致）而不是直接外送：
	// 外送失敗時佇列會重試，而且車廠端不需要知道 LINE 當下是否可達。
	approveURL := "/approval/" + req.Token

	// 客戶沒綁 LINE：推播根本送不出去。這正是「客戶電話同意」存在的理由——
	// 授權仍然建立（不可猜測的 token 與待決狀態才是紀錄的重點），
	// 並把連結直接給櫃檯，讓他想辦法送到客戶手上。
	if !customerHasLINE(customerID) {
		log.Printf("追加維修授權已建立（客戶未綁 LINE，未推播）: id=%d work_order=%d token=%s",
			req.ID, woID, req.Token)
		redirectMsg(w, r, woPath, fmt.Sprintf(
			"已建立追加維修授權。客戶未綁定 LINE，通知沒有送出——"+
				"請以電話取得同意後按「客戶電話同意」，或把這個連結給客戶：%s", approveURL))
		return
	}

	subject := "追加維修項目授權"
	body := fmt.Sprintf("您的車輛需要追加維修項目：\n%s × %d，單價 %s\n\n"+
		"請點選以下連結確認是否同意（24 小時內有效）：\n%s\n\n"+
		"若不同意，車廠將不進行此項維修。",
		item.Description, item.Quantity, line.Money(item.UnitPrice), approveURL)
	if err := cron.QueueNotification("approval_request", "line", subject, body, customerID, woID, time.Now()); err != nil {
		// 授權**已經建立**了，只是通知沒進佇列。
		// 這裡刻意用成功樣式的訊息而不是錯誤：顯示成錯誤會讓櫃檯以為整件事失敗
		// 而重新建立一次，結果變成兩筆未決授權同時擋住完工。
		log.Printf("追加維修授權 %d 已建立，但通知進佇列失敗: %v", req.ID, err)
		redirectMsg(w, r, woPath, fmt.Sprintf(
			"授權已建立，但通知客戶失敗——請以電話取得同意後按「客戶電話同意」，"+
				"或把這個連結給客戶：%s", approveURL))
		return
	}

	log.Printf("追加維修授權已建立: id=%d work_order=%d token=%s", req.ID, woID, req.Token)
	redirectMsg(w, r, woPath, "已建立追加維修授權，並已通知客戶（連結 24 小時內有效）")
}

// generateSecureToken 產生密碼學隨機 token（32 bytes = 64 hex chars）
func generateSecureToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// customerHasLINE 回報客戶是否已綁定 LINE——決定追加維修的授權連結
// 送不送得出去（送不出去時只能走電話同意）。
func customerHasLINE(customerID int64) bool {
	var lineUserID string
	if err := db.DB.QueryRow(
		`SELECT COALESCE(line_user_id,'') FROM active_customers WHERE id = ?`, customerID,
	).Scan(&lineUserID); err != nil {
		return false
	}
	return strings.TrimSpace(lineUserID) != ""
}

// CreateApprovalRequest 為工單建立授權請求
//
// 必須驗證：
// - 客戶存在
// - 工單存在且屬於該客戶
//
// **不要求**客戶綁定 LINE：沒綁 LINE 時仍建立授權（走電話同意），
// 由呼叫端決定要不要推播。見本函式內部的說明。
//
// 授權連結格式：/approval/<token>
// 預設有效期：24 小時
func CreateApprovalRequest(workOrderID, customerID int64, itemSummary string) (*model.ApprovalRequest, error) {
	// 1. 客戶必須存在。
	//
	// 注意：這裡**不再**要求客戶綁定 LINE。原本沒綁 LINE 就直接失敗，但那會讓
	// 「客戶電話同意」完全無路可走——而電話同意正是為沒綁 LINE 的客戶（多數是
	// 臨時進廠、從沒加過 LINE 的客人）設計的：既然推不了 LINE，就只能打電話。
	// 授權紀錄本身（不可猜測的 token + 待決狀態）才是重點，LINE 只是送達管道之一。
	// 送不送得出去由呼叫端判斷（見 CreateApprovalFromWorkOrder）。
	var exists int64
	err := db.DB.QueryRow(
		"SELECT id FROM active_customers WHERE id = ?", customerID,
	).Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("客戶不存在")
		}
		return nil, fmt.Errorf("查詢客戶失敗: %w", err)
	}

	// 2. 驗證工單存在且屬於該客戶
	var foundID int64
	err = db.DB.QueryRow(
		"SELECT id FROM active_work_orders WHERE id = ? AND customer_id = ?",
		workOrderID, customerID,
	).Scan(&foundID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("工單不存在或不屬於該客戶")
		}
		return nil, fmt.Errorf("查詢工單失敗: %w", err)
	}

	// 3. 產生安全 token
	token, err := generateSecureToken()
	if err != nil {
		return nil, fmt.Errorf("產生 token 失敗: %w", err)
	}

	// 4. 建立授權請求（24 小時有效期）
	expiresAt := time.Now().Add(24 * time.Hour).Format("2006-01-02 15:04:05")
	now := time.Now().Format("2006-01-02 15:04:05")

	result, err := db.DB.Exec(
		`INSERT INTO approval_requests
			(work_order_id, customer_id, token, item_summary, status, expires_at, created_at, decision_source)
			VALUES (?, ?, ?, ?, 'pending', ?, ?, 'line')`,
		workOrderID, customerID, token, itemSummary, expiresAt, now,
	)
	if err != nil {
		return nil, fmt.Errorf("建立授權請求失敗: %w", err)
	}

	id, _ := result.LastInsertId()
	return &model.ApprovalRequest{
		ID:             id,
		WorkOrderID:    workOrderID,
		CustomerID:     customerID,
		Token:          token,
		ItemSummary:    itemSummary,
		Status:         "pending",
		ExpiresAt:      expiresAt,
		CreatedAt:      now,
		DecisionSource: "line",
	}, nil
}

// GetApprovalRequestByToken 透過 token 取得授權請求（含驗證）
func GetApprovalRequestByToken(token string) (*model.ApprovalRequest, error) {
	var req model.ApprovalRequest
	var decidedAt sql.NullString
	var notes sql.NullString
	err := db.DB.QueryRow(
		`SELECT id, work_order_id, customer_id, token, item_summary, status,
			expires_at, created_at, decided_at, decision_source, notes
			FROM approval_requests WHERE token = ? AND deleted_at IS NULL`,
		token,
	).Scan(
		&req.ID, &req.WorkOrderID, &req.CustomerID, &req.Token, &req.ItemSummary,
		&req.Status, &req.ExpiresAt, &req.CreatedAt, &decidedAt,
		&req.DecisionSource, &notes,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("授權連結無效")
		}
		return nil, fmt.Errorf("查詢授權請求失敗: %w", err)
	}

	if decidedAt.Valid {
		req.DecidedAt = decidedAt.String
	}
	if notes.Valid {
		req.Notes = notes.String
	}

	// 檢查是否過期
	expiresTime, err := time.Parse("2006-01-02 15:04:05", req.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("解析過期時間失敗: %w", err)
	}
	if time.Now().After(expiresTime) {
		// 自動標記為過期
		_, _ = db.DB.Exec(
			"UPDATE approval_requests SET status='expired' WHERE id = ?",
			req.ID,
		)
		req.Status = "expired"
	}

	return &req, nil
}

// ApprovalPageData 授權頁面所需的資料
type ApprovalPageData struct {
	Request     *model.ApprovalRequest
	WorkOrder   *model.WorkOrder
	Customer    *model.Customer
	Items       []model.WorkOrderItem
	IsExpired   bool
	IsDecided   bool
	ErrorMsg    string
	DecidedText string
}

// ApprovalPageHandler 授權頁面（公開，不需登入）
func ApprovalPageHandler(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/approval/")
	if token == "" {
		http.Error(w, "無效的授權連結", http.StatusBadRequest)
		return
	}

	req, err := GetApprovalRequestByToken(token)
	if err != nil {
		data := ApprovalPageData{ErrorMsg: err.Error()}
		renderApprovalPage(w, data)
		return
	}

	// 檢查是否已決定
	if req.Status == "approved" || req.Status == "rejected" {
		statusText := map[string]string{"approved": "已同意", "rejected": "已拒絕"}[req.Status]
		data := ApprovalPageData{
			Request:     req,
			IsDecided:   true,
			DecidedText: statusText,
		}
		renderApprovalPage(w, data)
		return
	}

	// 檢查是否過期
	if req.Status == "expired" {
		data := ApprovalPageData{
			Request:   req,
			IsExpired: true,
		}
		renderApprovalPage(w, data)
		return
	}

	// 檢查是否 pending
	if req.Status != "pending" {
		data := ApprovalPageData{
			Request:  req,
			ErrorMsg: "授權狀態異常",
		}
		renderApprovalPage(w, data)
		return
	}

	// 取得工單資訊
	wo, err := getWorkOrder(req.WorkOrderID)
	if err != nil {
		data := ApprovalPageData{
			Request:  req,
			ErrorMsg: "工單資訊載入失敗",
		}
		renderApprovalPage(w, data)
		return
	}

	// 取得客戶資訊
	customer, err := getCustomer(req.CustomerID)
	if err != nil {
		data := ApprovalPageData{
			Request:  req,
			ErrorMsg: "客戶資訊載入失敗",
		}
		renderApprovalPage(w, data)
		return
	}

	// 取得工單項目
	items, err := getWorkOrderItems(req.WorkOrderID)
	if err != nil {
		items = []model.WorkOrderItem{} // 空列表不擋頁面
	}

	data := ApprovalPageData{
		Request:   req,
		WorkOrder: wo,
		Customer:  customer,
		Items:     items,
	}
	renderApprovalPage(w, data)
}

// ApproveRequestHandler 同意授權端點
func ApproveRequestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := strings.TrimPrefix(r.URL.Path, "/approval/approve/")
	if token == "" {
		http.Error(w, "無效的授權連結", http.StatusBadRequest)
		return
	}

	req, err := GetApprovalRequestByToken(token)
	if err != nil {
		renderApprovalResult(w, "error", err.Error())
		return
	}

	// 驗證狀態
	if req.Status == "expired" {
		renderApprovalResult(w, "expired", "授權已過期，請連絡車廠")
		return
	}
	if req.Status == "approved" || req.Status == "rejected" {
		renderApprovalResult(w, "decided", fmt.Sprintf("此授權已%s", map[string]string{"approved": "同意", "rejected": "拒絕"}[req.Status]))
		return
	}

	// 開始交易
	tx, err := db.DB.Begin()
	if err != nil {
		renderApprovalResult(w, "error", "交易失敗")
		return
	}
	defer tx.Rollback()

	// 1. 更新授權狀態
	now := time.Now().Format("2006-01-02 15:04:05")
	result, err := tx.Exec(
		`UPDATE approval_requests
			SET status='approved', decided_at=?, decision_source='web'
			WHERE id = ? AND status = 'pending' AND expires_at > ?`,
		now, req.ID, now,
	)
	if err != nil {
		renderApprovalResult(w, "error", "更新授權狀態失敗")
		return
	}

	// 檢查是否真的更新了（可能狀態已經不是 pending 或已過期）
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		tx.Rollback()
		// 重新查詢狀態
		var currentStatus string
		_ = db.DB.QueryRow("SELECT status FROM approval_requests WHERE id = ?", req.ID).Scan(&currentStatus)
		if currentStatus == "expired" {
			renderApprovalResult(w, "expired", "授權已過期，請連絡車廠")
		} else {
			renderApprovalResult(w, "decided", "此授權已決定")
		}
		return
	}

	// 2. 把授權內容寫進工單項目並重算總額。
	//
	//    必須與「把授權標成 approved」在同一個交易內：分開做的話，中途失敗會
	//    留下「已核准但工單沒有項目」或反向的不一致狀態，而這是錢的問題
	//    （客戶以為同意了、工單卻沒加，或反之）。
	//
	//    與「客戶電話同意」共用 applyApprovalItems：兩條路徑各寫一份的話，
	//    遲早有一邊忘了重算總額——項目進得了明細卻不算進 total_amount＝漏收。
	if err := applyApprovalItems(tx, req); err != nil {
		log.Printf("授權 %d 套用追加項目失敗，已中止: %v", req.ID, err)
		renderApprovalResult(w, "error", "追加項目資料異常，已通知車廠處理")
		return
	}

	_, err = tx.Exec(
		"UPDATE work_orders SET approval_status='approved', updated_at=? WHERE id = ?",
		now, req.WorkOrderID,
	)
	if err != nil {
		renderApprovalResult(w, "error", "更新工單狀態失敗")
		return
	}

	// 提交交易
	if err := tx.Commit(); err != nil {
		renderApprovalResult(w, "error", "提交交易失敗")
		return
	}

	log.Printf("授權已核准: token=%s, work_order_id=%d", token, req.WorkOrderID)
	renderApprovalResult(w, "success", "您的同意已送出，車廠會盡快安排維修")
}

// RejectRequestHandler 拒絕授權端點
func RejectRequestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := strings.TrimPrefix(r.URL.Path, "/approval/reject/")
	if token == "" {
		http.Error(w, "無效的授權連結", http.StatusBadRequest)
		return
	}

	req, err := GetApprovalRequestByToken(token)
	if err != nil {
		renderApprovalResult(w, "error", err.Error())
		return
	}

	// 驗證狀態
	if req.Status == "expired" {
		renderApprovalResult(w, "expired", "授權已過期，請連絡車廠")
		return
	}
	if req.Status == "approved" || req.Status == "rejected" {
		renderApprovalResult(w, "decided", fmt.Sprintf("此授權已%s", map[string]string{"approved": "同意", "rejected": "拒絕"}[req.Status]))
		return
	}

	// 更新授權狀態
	now := time.Now().Format("2006-01-02 15:04:05")
	result, err := db.DB.Exec(
		`UPDATE approval_requests
			SET status='rejected', decided_at=?, decision_source='web'
			WHERE id = ? AND status = 'pending' AND expires_at > ?`,
		now, req.ID, now,
	)
	if err != nil {
		renderApprovalResult(w, "error", "更新授權狀態失敗")
		return
	}

	// 檢查是否真的更新了（可能狀態已經不是 pending 或已過期）
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		// 重新查詢狀態
		var currentStatus string
		_ = db.DB.QueryRow("SELECT status FROM approval_requests WHERE id = ?", req.ID).Scan(&currentStatus)
		if currentStatus == "expired" {
			renderApprovalResult(w, "expired", "授權已過期，請連絡車廠")
		} else {
			renderApprovalResult(w, "decided", "此授權已決定")
		}
		return
	}

	log.Printf("授權已拒絕: token=%s, work_order_id=%d", token, req.WorkOrderID)
	renderApprovalResult(w, "success", "您的拒絕已送出，車廠會與您聯繫")
}

// renderApprovalPage 渲染授權頁面（簡單 HTML）
func renderApprovalPage(w http.ResponseWriter, data ApprovalPageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	html := `<!DOCTYPE html>
<html lang="zh-TW">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>追加維修授權</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #1a1a1a; color: #e0e0e0; margin: 0; padding: 20px; }
        .container { max-width: 600px; margin: 0 auto; background: #2d2d2d; border-radius: 8px; padding: 30px; }
        h1 { color: #4a9eff; margin-top: 0; }
        .error { color: #ff6b6b; background: #3d1f1f; padding: 15px; border-radius: 4px; }
        .success { color: #51cf66; background: #1f3d2f; padding: 15px; border-radius: 4px; }
        .info { background: #1f2d3d; padding: 15px; border-radius: 4px; margin: 15px 0; }
        .btn { display: inline-block; padding: 12px 24px; margin: 10px 5px; border: none; border-radius: 4px; cursor: pointer; font-size: 16px; text-decoration: none; }
        .btn-approve { background: #51cf66; color: white; }
        .btn-reject { background: #ff6b6b; color: white; }
        .btn:hover { opacity: 0.9; }
        .item { border-bottom: 1px solid #3d3d3d; padding: 10px 0; }
        .item:last-child { border-bottom: none; }
    </style>
</head>
<body>
    <div class="container">
        <h1>🔧 追加維修授權</h1>`

	if data.ErrorMsg != "" {
		html += fmt.Sprintf(`<div class="error">%s</div>`, data.ErrorMsg)
	} else if data.IsExpired {
		html += `<div class="error">授權已過期，請連絡車廠</div>`
	} else if data.IsDecided {
		html += fmt.Sprintf(`<div class="success">此授權%s（%s）</div>`, data.DecidedText, data.Request.DecidedAt)
	} else if data.Request != nil && data.WorkOrder != nil {
		html += fmt.Sprintf(`
        <div class="info">
            <strong>工單編號：</strong>#%d<br>
            <strong>客戶：</strong>%s<br>
            <strong>車牌：</strong>%s<br>
            <strong>過期時間：</strong>%s
        </div>`,
			data.WorkOrder.ID, data.Customer.Name, data.WorkOrder.PlateNumber, data.Request.ExpiresAt)

		if data.Request.ItemSummary != "" {
			html += fmt.Sprintf(`
        <div class="info">
            <strong>追加項目：</strong><br>
            %s
        </div>`, data.Request.ItemSummary)
		}

		if len(data.Items) > 0 {
			html += `<div class="info"><strong>工單項目：</strong><br>`
			for _, item := range data.Items {
				html += fmt.Sprintf(`<div class="item">%s x%d - $%.0f</div>`,
					item.Description, item.Quantity, item.Subtotal)
			}
			html += `</div>`
		}

		html += `
        <form method="POST" action="/approval/approve/` + data.Request.Token + `">
            <button type="submit" class="btn btn-approve">✅ 同意</button>
        </form>
        <form method="POST" action="/approval/reject/` + data.Request.Token + `">
            <button type="submit" class="btn btn-reject">❌ 拒絕</button>
        </form>`
	}

	html += `
    </div>
</body>
</html>`

	w.Write([]byte(html))
}

// renderApprovalResult 渲染授權結果頁面
func renderApprovalResult(w http.ResponseWriter, status, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	var cssClass string
	switch status {
	case "success":
		cssClass = "success"
	case "error", "expired", "decided":
		cssClass = "error"
	default:
		cssClass = "info"
	}

	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-TW">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>授權結果</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #1a1a1a; color: #e0e0e0; margin: 0; padding: 20px; }
        .container { max-width: 600px; margin: 0 auto; background: #2d2d2d; border-radius: 8px; padding: 30px; }
        h1 { color: #4a9eff; margin-top: 0; }
        .%s { padding: 15px; border-radius: 4px; margin: 20px 0; }
        .success { color: #51cf66; background: #1f3d2f; }
        .error { color: #ff6b6b; background: #3d1f1f; }
        .info { color: #4a9eff; background: #1f2d3d; }
    </style>
</head>
<body>
    <div class="container">
        <h1>🔧 授權結果</h1>
        <div class="%s">%s</div>
        <p>您可以關閉此頁面。</p>
    </div>
</body>
</html>`, cssClass, cssClass, message)

	w.Write([]byte(html))
}

// getWorkOrder 取得工單資訊（簡化版，只用於授權頁面）
func getWorkOrder(id int64) (*model.WorkOrder, error) {
	var wo model.WorkOrder
	err := db.DB.QueryRow(
		`SELECT id, vehicle_id, customer_id, current_mileage, status, approval_status,
			COALESCE(total_amount,0), created_at
			FROM active_work_orders WHERE id = ?`,
		id,
	).Scan(
		&wo.ID, &wo.VehicleID, &wo.CustomerID, &wo.CurrentMileage,
		&wo.Status, &wo.ApprovalStatus, &wo.TotalAmount, &wo.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	// 取得車牌號碼
	err = db.DB.QueryRow(
		"SELECT plate_number FROM active_vehicles WHERE id = ?",
		wo.VehicleID,
	).Scan(&wo.PlateNumber)
	if err != nil {
		wo.PlateNumber = "未知車牌"
	}

	return &wo, nil
}

// getCustomer 取得客戶資訊（簡化版，只用於授權頁面）
func getCustomer(id int64) (*model.Customer, error) {
	var c model.Customer
	err := db.DB.QueryRow(
		"SELECT id, name, phone FROM active_customers WHERE id = ?",
		id,
	).Scan(&c.ID, &c.Name, &c.Phone)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// getWorkOrderItems 取得工單項目（簡化版，只用於授權頁面）
func getWorkOrderItems(woID int64) ([]model.WorkOrderItem, error) {
	rows, err := db.DB.Query(
		`SELECT id, item_type, description, quantity, unit_price, subtotal
			FROM active_work_order_items WHERE work_order_id = ?`,
		woID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []model.WorkOrderItem
	for rows.Next() {
		var item model.WorkOrderItem
		err := rows.Scan(
			&item.ID, &item.ItemType, &item.Description,
			&item.Quantity, &item.UnitPrice, &item.Subtotal,
		)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}
