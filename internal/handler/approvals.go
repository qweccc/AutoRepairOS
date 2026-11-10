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
	"autorepairos/web/pages"
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
		log.Printf("追加維修授權已建立（客戶未綁 LINE，未推播）: id=%d work_order=%d",
			req.ID, woID)
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

	log.Printf("追加維修授權已建立: id=%d work_order=%d", req.ID, woID)
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

// ApprovalPageHandler 授權頁面（公開，不需登入）
func ApprovalPageHandler(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/approval/")
	if token == "" {
		http.Error(w, "無效的授權連結", http.StatusBadRequest)
		return
	}

	req, err := GetApprovalRequestByToken(token)
	if err != nil {
		data := pages.ApprovalPageData{ErrorMsg: err.Error()}
		renderApprovalPage(w, data)
		return
	}

	// 檢查是否已決定
	if req.Status == "approved" || req.Status == "rejected" {
		statusText := map[string]string{"approved": "已同意", "rejected": "已拒絕"}[req.Status]
		data := pages.ApprovalPageData{
			Request:     req,
			IsDecided:   true,
			DecidedText: statusText,
		}
		renderApprovalPage(w, data)
		return
	}

	// 檢查是否過期
	if req.Status == "expired" {
		data := pages.ApprovalPageData{
			Request:   req,
			IsExpired: true,
		}
		renderApprovalPage(w, data)
		return
	}

	// 檢查是否 pending
	if req.Status != "pending" {
		data := pages.ApprovalPageData{
			Request:  req,
			ErrorMsg: "授權狀態異常",
		}
		renderApprovalPage(w, data)
		return
	}

	// 取得工單資訊
	wo, err := getWorkOrder(req.WorkOrderID)
	if err != nil {
		data := pages.ApprovalPageData{
			Request:  req,
			ErrorMsg: "工單資訊載入失敗",
		}
		renderApprovalPage(w, data)
		return
	}

	// 取得客戶資訊
	customer, err := getCustomer(req.CustomerID)
	if err != nil {
		data := pages.ApprovalPageData{
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

	data := pages.ApprovalPageData{
		Request:   req,
		WorkOrder: wo,
		Customer:  customer,
		Items:     items,
	}
	renderApprovalPage(w, data)
}
