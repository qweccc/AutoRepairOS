package handler

// 追加維修授權的聊天回覆（FR-WO-06）：客戶在 LINE／Telegram／Discord／Slack
// 直接回「同意」／「拒絕」完成授權，取代點不到的相對路徑連結。
//
// 與 web／電話同意共用 applyApprovalItems（項目寫入＋總額重算在同一交易），
// decision_source='chat' 讓事後追查知道同意是客戶自己在聊天中回的。

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// handleApprovalReply 處理「同意」「拒絕」（可加註工單編號，如「同意 12」）。
// 回 (回覆, 是否已處理)。
func handleApprovalReply(channel, userID, q string) (string, bool) {
	fields := strings.Fields(q)
	if len(fields) == 0 {
		return "", false
	}
	var approve bool
	switch fields[0] {
	case "同意", "同意追加":
		approve = true
	case "拒絕", "拒絕追加":
		approve = false
	default:
		return "", false
	}
	var filterWO int64
	if len(fields) > 1 {
		id, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return "格式：「同意」或「拒絕」，多筆時請加註工單編號，例如「同意 12」。", true
		}
		filterWO = id
	}

	cid, ok := resolveCustomerByChannel(channel, userID)
	if !ok {
		return unboundReply, true
	}

	now := nowStamp()
	rows, err := db.DB.Query(`SELECT id, work_order_id, item_summary, expires_at
		FROM approval_requests
		WHERE customer_id = ? AND status = 'pending' AND deleted_at IS NULL AND expires_at > ?
		ORDER BY id DESC`, cid, now)
	if err != nil {
		log.Printf("查詢待決授權失敗（客戶 #%d）: %v", cid, err)
		return "目前無法查詢授權，請稍後再試或聯絡車廠。", true
	}
	type pending struct {
		id, woID    int64
		summary     string
		expiresAt   string
	}
	var list []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.woID, &p.summary, &p.expiresAt); err == nil {
			list = append(list, p)
		}
	}
	rows.Close()

	if filterWO > 0 {
		filtered := list[:0]
		for _, p := range list {
			if p.woID == filterWO {
				filtered = append(filtered, p)
			}
		}
		list = filtered
		if len(list) == 0 {
			return fmt.Sprintf("工單 #%d 沒有待確認的追加維修授權。", filterWO), true
		}
	}
	if len(list) == 0 {
		return "目前沒有待確認的追加維修授權。", true
	}
	if len(list) > 1 {
		ids := make([]string, 0, len(list))
		for _, p := range list {
			ids = append(ids, "#"+strconv.FormatInt(p.woID, 10))
		}
		return "您有多筆待確認的追加授權（工單 " + strings.Join(ids, "、") +
			"），請回覆「同意 工單編號」或「拒絕 工單編號」，例如「同意 " +
			strconv.FormatInt(list[0].woID, 10) + "」。", true
	}

	p := list[0]
	req := &model.ApprovalRequest{
		ID: p.id, WorkOrderID: p.woID, CustomerID: cid,
		ItemSummary: p.summary, Status: "pending", ExpiresAt: p.expiresAt,
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return "資料庫忙碌，請稍後再試。", true
	}
	defer tx.Rollback()

	status, verb := "rejected", "拒絕"
	if approve {
		status, verb = "approved", "同意"
	}
	res, err := tx.Exec(`UPDATE approval_requests
		SET status=?, decided_at=?, decision_source='chat', notes=?
		WHERE id = ? AND status = 'pending' AND expires_at > ?`,
		status, now, "客戶聊天回覆："+verb+"（"+channel+"）", req.ID, now)
	if err != nil {
		log.Printf("聊天授權更新失敗（授權 %d）: %v", req.ID, err)
		return "處理失敗，請稍後再試或聯絡車廠。", true
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "這筆授權已被處理或已過期，請聯絡車廠。", true
	}

	if approve {
		if err := applyApprovalItems(tx, req); err != nil {
			log.Printf("聊天同意後套用項目失敗（授權 %d）: %v", req.ID, err)
			return "追加項目資料異常，已通知車廠處理。", true
		}
		if _, err := tx.Exec(`UPDATE work_orders SET approval_status='approved', updated_at=? WHERE id = ?`,
			now, req.WorkOrderID); err != nil {
			return "更新工單狀態失敗，請聯絡車廠。", true
		}
	}
	if err := tx.Commit(); err != nil {
		return "處理失敗，請稍後再試或聯絡車廠。", true
	}

	log.Printf("聊天授權已%s: 授權 %d 工單 %d 通道=%s", verb, req.ID, req.WorkOrderID, channel)
	if approve {
		desc := ""
		if items, err := parseApprovalItems(req.ItemSummary); err == nil && len(items) > 0 {
			desc = fmt.Sprintf("：%s × %d", items[0].Description, items[0].Quantity)
		}
		return fmt.Sprintf("已同意工單 #%d 的追加維修%s。車廠會盡快安排，感謝您的確認。", req.WorkOrderID, desc), true
	}
	return fmt.Sprintf("已拒絕工單 #%d 的追加維修，車廠會與您聯繫。", req.WorkOrderID), true
}

// customerNotificationChannel 依客戶偏好與綁定選通知通道：
// 偏好優先（需已綁定），否則依 line→telegram→discord→slack 取第一個已綁定者。
// 全未綁定回 ""（櫃檯走電話同意）。
func customerNotificationChannel(customerID int64) string {
	var pref, lineID, tgID, dcID, slID string
	err := db.DB.QueryRow(`SELECT COALESCE(notification_preference,'none'),
		COALESCE(line_user_id,''), COALESCE(telegram_user_id,''),
		COALESCE(discord_user_id,''), COALESCE(slack_user_id,'')
		FROM active_customers WHERE id = ?`, customerID).
		Scan(&pref, &lineID, &tgID, &dcID, &slID)
	if err != nil {
		return ""
	}
	bound := map[string]string{"line": lineID, "telegram": tgID, "discord": dcID, "slack": slID}
	if pref != "none" && pref != "sms" && strings.TrimSpace(bound[pref]) != "" {
		return pref
	}
	for _, ch := range []string{"line", "telegram", "discord", "slack"} {
		if strings.TrimSpace(bound[ch]) != "" {
			return ch
		}
	}
	return ""
}