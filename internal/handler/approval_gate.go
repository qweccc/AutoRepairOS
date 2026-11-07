package handler

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

// 這個檔案放「追加維修授權」的共用邏輯與兩道關卡：
//  1. 未決授權的查詢與提醒（pendingApprovalRequests）
//  2. 未決授權阻擋完工／結帳（CheckApprovalForStatus）
//  3. 客戶電話同意的記錄路徑（ApproveApprovalByPhone）
//
// 為什麼電話同意是必要的：LINE 連結要靠客戶自己點。客戶在電話上說「好，你修」，
// 但沒點連結、或根本沒在看 LINE 時，工單會永遠卡在「待客戶同意」——
// 而車子已經在頂車機上了。櫃檯必須能把「電話中取得的同意」記錄下來。

// approvalRequestByID 依 id 取授權請求。
func approvalRequestByID(id int64) (*model.ApprovalRequest, error) {
	var req model.ApprovalRequest
	err := db.DB.QueryRow(`SELECT id, work_order_id, customer_id, token,
		COALESCE(item_summary,''), status, expires_at,
		COALESCE(created_at,''), COALESCE(decided_at,''), COALESCE(decision_source,''),
		COALESCE(notes,'')
		FROM active_approval_requests WHERE id = ?`, id).
		Scan(&req.ID, &req.WorkOrderID, &req.CustomerID, &req.Token,
			&req.ItemSummary, &req.Status, &req.ExpiresAt,
			&req.CreatedAt, &req.DecidedAt, &req.DecisionSource, &req.Notes)
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// pendingApprovalRequests 回傳這張工單「還等不到客戶回覆」的授權請求
// （status=pending 且未過期），依建立時間由舊到新。
//
// 過期的不算：客戶不會再回覆，它不該繼續擋住完工，但仍留在頁面上讓櫃檯知道
// 曾經送出過（由呼叫端決定顯示方式）。
func pendingApprovalRequests(woID int64) ([]model.ApprovalRequest, error) {
	rows, err := db.DB.Query(`SELECT id, work_order_id, customer_id, token,
		COALESCE(item_summary,''), status, expires_at,
		COALESCE(created_at,''), COALESCE(decided_at,''), COALESCE(decision_source,''),
		COALESCE(notes,'')
		FROM active_approval_requests
		WHERE work_order_id = ? AND status = 'pending' AND expires_at > ?
		ORDER BY id`, woID, nowStamp())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.ApprovalRequest
	for rows.Next() {
		var req model.ApprovalRequest
		if err := rows.Scan(&req.ID, &req.WorkOrderID, &req.CustomerID, &req.Token,
			&req.ItemSummary, &req.Status, &req.ExpiresAt,
			&req.CreatedAt, &req.DecidedAt, &req.DecisionSource, &req.Notes); err != nil {
			continue
		}
		out = append(out, req)
	}
	return out, rows.Err()
}

// CheckApprovalForStatus 判斷這張工單能不能進入目標狀態。
//
// 判準是「有沒有未決的 approval_requests」，**不是**工單上的 approval_status 欄位：
// 後者的預設值就是 'pending'，絕大多數工單根本沒有追加項目，若照字面一律擋，
// 每一張沒走追加流程的工單都無法完工——那是災難而不是保護。
//
// 查詢失敗時不擋（與 CheckSOPCompletionForStatus 一致）：這是輔助關卡，
// 不該因為查詢問題讓車廠無法交車。
// isCompletionOrPayment checks if status is completed or paid.
func isCompletionOrPayment(status string) bool {
	return status == "completed" || status == "paid"
}

func CheckApprovalForStatus(woID int64, targetStatus string) error {
	if !isCompletionOrPayment(targetStatus) {
		return nil
	}
	pending, err := pendingApprovalRequests(woID)
	if err != nil {
		log.Printf("檢查工單 #%d 的未決授權失敗（不阻擋）: %v", woID, err)
		return nil
	}
	if len(pending) > 0 {
		return fmt.Errorf("這張工單有 %d 筆追加維修等待客戶同意，請先等客戶回覆、"+
			"以電話取得同意後按「客戶電話同意」，或取消授權", len(pending))
	}
	return nil
}

// pendingApprovalRows 把未決授權轉成頁面可顯示的列。
//
// 格式壞掉（item_summary 解析失敗）的列仍然要顯示：它同樣會擋住完工，
// 若因為解析不了就不顯示，櫃檯會看到「有 N 筆等待同意」的提醒卻找不到任何列、
// 也無法取消，只能等它過期。
func pendingApprovalRows(woID int64) []pages.PendingApproval {
	reqs, err := pendingApprovalRequests(woID)
	if err != nil {
		log.Printf("載入工單 #%d 的未決授權失敗: %v", woID, err)
		return nil
	}
	out := make([]pages.PendingApproval, 0, len(reqs))
	for _, r := range reqs {
		items, perr := parseApprovalItems(r.ItemSummary)
		if perr != nil || len(items) == 0 {
			out = append(out, pages.PendingApproval{
				ID: r.ID, Description: "（項目資料異常，請取消後重新建立）",
				ExpiresAt: r.ExpiresAt,
			})
			continue
		}
		// 建立時一次一項（見 CreateApprovalFromWorkOrder）；解析出多項時
		// 只顯示第一項，但取消／同意會套用全部——所以標明還有幾項。
		desc := items[0].Description
		if len(items) > 1 {
			desc += fmt.Sprintf("（共 %d 項）", len(items))
		}
		out = append(out, pages.PendingApproval{
			ID: r.ID, Description: desc, Quantity: items[0].Quantity,
			UnitPrice: items[0].UnitPrice, ItemType: items[0].ItemType,
			ExpiresAt: r.ExpiresAt,
		})
	}
	return out
}

// applyApprovalItems 把授權內容寫進工單項目並重算總額。
//
// 抽出來共用（客戶線上同意、櫃檯記錄電話同意都走這裡）：兩條路徑若各寫一份，
// 遲早會有一邊忘了重算總額——項目進得了明細卻不算進 total_amount＝帳務漏收。
// 呼叫端必須在同一個交易內呼叫，失敗時整筆回滾。
func applyApprovalItems(tx *sql.Tx, req *model.ApprovalRequest) error {
	items, err := parseApprovalItems(req.ItemSummary)
	if err != nil {
		return err
	}
	for _, it := range items {
		if _, err := tx.Exec(`INSERT INTO work_order_items
			(work_order_id, item_type, description, quantity, unit_price, subtotal)
			VALUES (?, ?, ?, ?, ?, ?)`,
			req.WorkOrderID, it.ItemType, it.Description, it.Quantity, it.UnitPrice,
			float64(it.Quantity)*it.UnitPrice); err != nil {
			return fmt.Errorf("加入工單項目失敗: %w", err)
		}
	}
	if err := recalcWorkOrderTotal(tx, req.WorkOrderID); err != nil {
		return fmt.Errorf("重算工單金額失敗: %w", err)
	}
	return nil
}

// ApproveApprovalByPhone 由櫃檯記錄「客戶在電話中同意」。
//
// 為什麼需要：LINE 連結要靠客戶自己點。客戶電話上說「好，你修」但沒點連結時，
// 工單會永遠卡在待客戶同意，而車子已經在頂車機上了。這是唯一的解套路徑，
// 所以 decision_source 記成 'phone' 而不是 'web'——事後追查「這個同意是怎麼來的」
// 時，電話同意與客戶自己點連結是兩件可信度不同的事。
func ApproveApprovalByPhone(w http.ResponseWriter, r *http.Request) {
	woID := mustInt(r.PathValue("id"))
	reqID := mustInt(r.PathValue("req_id"))
	woPath := "/work-orders/" + strconv.FormatInt(woID, 10)

	req, err := approvalRequestByID(reqID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// 授權必須屬於這張工單：否則可以拿別張工單的授權 id 去核准自己的工單
	if req.WorkOrderID != woID {
		redirectErrMsg(w, r, woPath, "這筆授權不屬於此工單")
		return
	}
	if req.Status != "pending" {
		label := map[string]string{"approved": "已同意", "rejected": "已拒絕", "expired": "已過期"}[req.Status]
		if label == "" {
			label = req.Status
		}
		redirectErrMsg(w, r, woPath, "這筆授權已"+label+"，不需要再記錄")
		return
	}
	if req.ExpiresAt <= nowStamp() {
		redirectErrMsg(w, r, woPath, "這筆授權已過期，請重新建立授權")
		return
	}

	note := strings.TrimSpace(r.FormValue("note"))
	if note == "" {
		redirectErrMsg(w, r, woPath, "請填寫電話同意的備註（例如：與車主陳先生通話確認）")
		return
	}

	now := nowStamp()
	tx, err := db.DB.Begin()
	if err != nil {
		redirectErrMsg(w, r, woPath, "資料庫忙碌，請稍後再試")
		return
	}
	defer tx.Rollback()

	// 條件更新：狀態必須仍是 pending 且未過期，避免兩人同時按（或先按了連結）
	res, err := tx.Exec(`UPDATE approval_requests
		SET status='approved', decided_at=?, decision_source='phone', notes=?
		WHERE id = ? AND status = 'pending' AND expires_at > ?`,
		now, "電話同意："+note, req.ID, now)
	if err != nil {
		log.Printf("記錄電話同意失敗（授權 %d）: %v", req.ID, err)
		redirectErrMsg(w, r, woPath, "記錄電話同意失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		redirectErrMsg(w, r, woPath, "這筆授權已被處理（可能客戶剛點了連結），請重新整理確認")
		return
	}

	if err := applyApprovalItems(tx, req); err != nil {
		log.Printf("電話同意後套用項目失敗（授權 %d）: %v", req.ID, err)
		redirectErrMsg(w, r, woPath, "記錄同意失敗："+err.Error())
		return
	}

	if _, err := tx.Exec(`UPDATE work_orders SET approval_status='approved', updated_at=? WHERE id = ?`,
		now, req.WorkOrderID); err != nil {
		redirectErrMsg(w, r, woPath, "更新工單狀態失敗")
		return
	}

	if err := tx.Commit(); err != nil {
		redirectErrMsg(w, r, woPath, "記錄電話同意失敗")
		return
	}

	log.Printf("已記錄電話同意: 授權 %d 工單 %d 備註=%q", req.ID, woID, note)
	redirectMsg(w, r, woPath, "已記錄客戶電話同意，追加項目已加入工單")
}

// CancelApprovalRequest 取消一筆未決的授權（客戶不修了、或櫃檯發現填錯）。
//
// 為什麼需要：未決授權會擋住完工（見 CheckApprovalForStatus）。若客戶最後決定
// 不修，或櫃檯發現品名／金額填錯，沒有取消路徑就只能等它過期 24 小時——
// 等於逼車廠用「重新建立一筆」繞過，留下一堆沒意義的紀錄。
func CancelApprovalRequest(w http.ResponseWriter, r *http.Request) {
	woID := mustInt(r.PathValue("id"))
	reqID := mustInt(r.PathValue("req_id"))
	woPath := "/work-orders/" + strconv.FormatInt(woID, 10)

	req, err := approvalRequestByID(reqID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if req.WorkOrderID != woID {
		redirectErrMsg(w, r, woPath, "這筆授權不屬於此工單")
		return
	}
	if req.Status != "pending" {
		redirectErrMsg(w, r, woPath, "只有等待中的授權可以取消")
		return
	}

	// 取消 = 標成 rejected 並註明來源。不刪除：這是「客戶不同意／作廢」的紀錄，
	// 事後要能說明為什麼這張工單沒有那個項目。
	now := nowStamp()
	note := strings.TrimSpace(r.FormValue("note"))
	if note == "" {
		note = "櫃檯取消"
	}
	res, err := db.DB.Exec(`UPDATE approval_requests
		SET status='rejected', decided_at=?, decision_source='counter', notes=?
		WHERE id = ? AND status = 'pending'`, now, "取消："+note, req.ID)
	if err != nil {
		log.Printf("取消授權失敗（授權 %d）: %v", req.ID, err)
		redirectErrMsg(w, r, woPath, "取消授權失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		redirectErrMsg(w, r, woPath, "這筆授權已被處理，請重新整理確認")
		return
	}

	log.Printf("已取消追加維修授權: 授權 %d 工單 %d 備註=%q", req.ID, woID, note)
	redirectMsg(w, r, woPath, "已取消追加維修授權（客戶未同意，項目不會加入工單）")
}
