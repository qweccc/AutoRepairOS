package handler

import (
	"fmt"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/integration/line"
)

// line_binding.go — LINE binding helpers.

// normalizePhone 把使用者輸入的電話整理成可比對的形式。
//
// 使用者可能輸入「0912345678」「0912-345-678」「+886 912 345 678」「0912 345 678」。
// 全部轉成 09 開頭的 10 位數字後比對，才能容忍這些常見寫法。
// 回傳空字串表示這不是一個看起來像手機號碼的輸入。
func normalizePhone(s string) string {
	var digits []rune
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		}
	}
	n := string(digits)
	// 台灣手機為 09xxxxxxxx（10 碼）。帶國碼時：
	//   886 + 912345678  = 12 碼 → 去掉 886 後要補回前導 0，才是 0912345678
	//   886 + 0912345678 = 13 碼 → 去掉 886 後已是 10 碼，直接用
	if strings.HasPrefix(n, "886") {
		switch len(n) {
		case 12: // 886 + 9 碼（少了前導 0）
			n = "0" + n[3:]
		case 13: // 886 + 10 碼（含前導 0）
			n = n[3:]
		}
	}
	if len(n) != 10 || !strings.HasPrefix(n, "09") {
		return ""
	}
	return n
}

// phoneMatchSQL 產生「電話欄位正規化後等於 ?」的 SQL 片段。
//
// 為什麼需要它：normalizePhone 只正規化「使用者輸入」，
// 但 customers.phone 存的是舊系統原樣 —— 帶分隔符與註記。
// 實測 6,512 筆電話中，2,746 筆是「0905-121161」這種格式，
// 另有 1,201 筆帶「#1」後綴（同門號的第二位聯絡人）。
// 只用 normalizePhone(輸入) 去比對 phone 欄，這 2,746 筆永遠查不到，
// 客戶回報手機號碼一律得到「查無此號碼」。
//
// 正規化規則（必須與 normalizePhone 一致）：
//  1. 只留數字，去掉 '-'、' '、'#' 等
//  2. 移除結尾的「#N」續號 —— 續號是舊系統用來區分同門號的第二人，
//     語意上仍是同一支手機
//  3. 10 碼且 09 開頭者直接比對；否則不視為手機
//
// 回傳的片段不含參數佔位符，呼叫端自行 append phone。
func phoneMatchSQL(col string) string {
	// 步驟：去掉 '-' ' ' '.' 等分隔符 → 去掉 '#' 之後的續號 → 取前 10 碼。
	// 用 replace 串接而非 regexp：SQLite 沒有內建 regexp，且這幾種符號
	// 已涵蓋實際資料（實測只出現 '-' 與 '#'）。
	return `(CASE
		WHEN length(replace(replace(replace(replace(` + col + `, '-', ''), ' ', ''), '.', ''), '#', '')) >= 10
		THEN substr(replace(replace(replace(replace(` + col + `, '-', ''), ' ', ''), '.', ''), '#', ''), 1, 10)
		ELSE '' END) = ?`
}

// customerCountByPhone 回傳該號碼對應的客戶筆數。
//
// 用 phoneMatchSQL 正規化「資料庫那一側」再比對，不是直接 phone = ?：
// customers.phone 存的是舊系統原樣（'0905-121161'、'07002-232#1'），
// 直接比對會讓 2,746 筆帶分隔符的手機永遠查不到。
func customerCountByPhone(phone string) int {
	var n int
	db.DB.QueryRow("SELECT COUNT(1) FROM active_customers WHERE "+phoneMatchSQL("phone"), phone).Scan(&n)
	return n
}

// isBound 查詢某個 LINE user ID 是否已綁定客戶。
func isBound(lineUserID string) bool {
	return countRows("SELECT COUNT(1) FROM active_customers WHERE line_user_id = ?", lineUserID) > 0
}

// bindLineUser 把 LINE user ID 綁到指定客戶。
//
// 若該 LINE ID 或該客戶已被其他人使用則拒絕：
//   - 同一個 LINE 帳號不能同時對應兩個客戶（否則 A 會收到 B 的維修通知）
//   - 同一個客戶不能被兩個 LINE 帳號綁定（否則通知會重複送出）
func bindLineUser(lineUserID string, customerID int64) error {
	if strings.TrimSpace(lineUserID) == "" {
		return fmt.Errorf("LINE user ID 為空")
	}

	if countRows("SELECT COUNT(1) FROM active_customers WHERE line_user_id = ?", lineUserID) > 0 {
		return fmt.Errorf("此 LINE 帳號已綁定其他客戶")
	}
	if countRows("SELECT COUNT(1) FROM active_customers WHERE id = ? AND COALESCE(line_user_id,'') <> ''", customerID) > 0 {
		return fmt.Errorf("此客戶已綁定其他 LINE 帳號")
	}

	res, err := db.DB.Exec(
		"UPDATE customers SET line_user_id = ? WHERE id = ?",
		lineUserID, customerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("找不到該客戶")
	}
	return nil
}

// beginBinding 客戶加好友後引導他完成綁定。
//
// 不自動綁定：車行常見一位車主有多台車，也可能有多位車主共用門號。
// 沒有明確對應時猜測只會把維修通知送錯人，所以改成請客戶報手機號碼，
// 由程式比對；比對得到唯一客戶才自動綁定。
func beginBinding(client *line.Client, ev line.FollowEvent) {
	if isBound(ev.UserID) {
		log_line("follow：%s 已綁定，略過", ev.UserID)
		if ev.ReplyToken != "" {
			_ = client.ReplyMessage(ev.ReplyToken, "您已綁定帳戶。傳「進度」可查詢車輛狀態。")
		}
		return
	}

	log_line("follow：%s 新增好友，等待報手機號碼以完成綁定", ev.UserID)

	// 留下待辦讓櫃檯知道有未綁定的使用者，避免客戶忘記報號碼而没人發現。
	db.MustExec(`INSERT INTO notifications
		(customer_id, channel, kind, subject, body, scheduled_at, status)
		VALUES (NULL, 'internal', 'line_follow', '新的 LINE 好友（待綁定）',
			?, CURRENT_TIMESTAMP, 'pending')`,
		"LINE user ID: "+ev.UserID+" — 等待客戶報手機號碼，或請櫃檯手動綁定")

	if ev.ReplyToken != "" {
		msg := "您好，歡迎加入汽車修。\n\n請傳送您的手機號碼，我們會為您連結車輛紀錄。\n連結後即可查詢工單進度。"
		if err := client.ReplyMessage(ev.ReplyToken, msg); err != nil {
			log_line("follow 回訊失敗（可能是官方帳號的歡迎訊息已消耗 token）: %v", err)
		}
	}
}

// tryBindByPhone 嘗試用客戶回報的手機號碼完成綁定。
// 回傳 (回覆文字, 是否已處理此訊息)。
func tryBindByPhone(client *line.Client, ev line.MessageEvent) (string, bool) {
	if isBound(ev.UserID) {
		return "", false
	}

	phone := normalizePhone(ev.Text)
	if phone == "" {
		// 還沒綁定，又不是手機號碼。提示一次，避免客戶不知道要傳什麼。
		return "", false
	}

	n := customerCountByPhone(phone)
	switch {
	case n == 0:
		msg := "查無此號碼「" + phone + "」的客戶紀錄。\n請確認號碼是否正確，或撥打店內電話由櫃檯協助。"
		return msg, true

	case n > 1:
		// 同一門號有多筆客戶，無法自動判斷是誰。
		msg := "此號碼對應 " + strconv.Itoa(n) + " 筆客戶紀錄，為避免通知送錯人，需由櫃檯協助綁定。\n請撥打店內電話告知。"
		log_line("綁定中止：號碼 %s 對應 %d 個客戶，需人工處理", phone, n)
		db.MustExec(`INSERT INTO notifications
			(customer_id, channel, kind, subject, body, scheduled_at, status)
			VALUES (NULL, 'internal', 'line_follow', 'LINE 綁定需人工處理',
				?, CURRENT_TIMESTAMP, 'pending')`,
			"號碼 "+phone+" 對應 "+strconv.Itoa(n)+" 個客戶，LINE user "+ev.UserID+" 無法自動綁定")
		return msg, true

	default:
		var customerID int64
		if err := db.DB.QueryRow("SELECT id FROM active_customers WHERE "+phoneMatchSQL("phone"), phone).Scan(&customerID); err != nil {
			return "系統忙碌中，請稍後再試。", true
		}
		if err := bindLineUser(ev.UserID, customerID); err != nil {
			log_line("綁定 %s 到客戶 #%d 失敗: %v", ev.UserID, customerID, err)
			return "綁定失敗：" + err.Error() + "。請撥打店內電話由櫃檯協助。", true
		}

		var cname string
		db.DB.QueryRow("SELECT name FROM active_customers WHERE id = ?", customerID).Scan(&cname)
		log_line("LINE %s 已自動綁定客戶 #%d（%s）", ev.UserID, customerID, cname)

		var vehicleCount int
		db.DB.QueryRow(`SELECT COUNT(1) FROM active_vehicles v
			JOIN active_work_orders w ON w.vehicle_id = v.id
			WHERE v.customer_id = ?`, customerID).Scan(&vehicleCount)

		msg := "已為您連結客戶資料（" + cname + "）。\n\n傳「進度」可查詢車輛狀態，傳「工單 編號」可查指定工單。"
		if vehicleCount == 0 {
			msg += "\n\n目前查無您的車輛紀錄，若您的車已進廠請與櫃檯聯繫。"
		}
		return msg, true
	}
}

// handlePostback 處理按鈕點擊。
//
// client 允許為 nil：核准本身不依賴 LINE 連線，只有要回覆客戶時才需要。
func handlePostback(client *line.Client, ev line.PostbackEvent) {
	// 舊的 approve:<work_order_id> 格式已停用（T2.11）
	// 現在使用 token 授權流程，透過 CreateApprovalRequest 產生授權連結
	if strings.HasPrefix(ev.Data, "approve:") {
		if ev.ReplyToken != "" && client != nil {
			_ = client.ReplyMessage(ev.ReplyToken,
				"舊版授權格式已停用，請使用新的授權連結。如有問題請連絡車廠。")
		}
		log_line("收到舊版 approve postback，已拒絕: %q", ev.Data)
		return
	}
	log_line("收到未知的 postback: %q", ev.Data)
}

// handleCustomerMessage 回覆客戶訊息。回傳要送出的文字，空字串代表不需回覆。
func handleCustomerMessage(client *line.Client, ev line.MessageEvent) string {
	q := strings.TrimSpace(ev.Text)

	switch {
	case q == "履歷" || q == "維修紀錄" || q == "保養紀錄":
		return customerPassbookSummary(ev.UserID)

	case q == "進度" || q == "查詢" || q == "工單":
		var plate, status string
		err := db.DB.QueryRow(`SELECT v.plate_number, wo.status
			FROM active_work_orders wo JOIN active_vehicles v ON v.id = wo.vehicle_id
			JOIN active_customers c ON c.id = wo.customer_id
			WHERE c.line_user_id = ? ORDER BY wo.id DESC LIMIT 1`, ev.UserID).
			Scan(&plate, &status)
		if err != nil {
			return "查不到您的車輛紀錄，請確認手機號碼是否已登錄。"
		}
		label := workOrderStatusLabel[status]
		if label == "" {
			label = status
		}
		return fmt.Sprintf("您的車輛 %s 目前狀態：%s", plate, label)

	case strings.HasPrefix(q, "工單"):
		// 「工單 12」查指定單號
		idStr := strings.TrimSpace(strings.TrimPrefix(q, "工單"))
		woID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			return "請輸入工單編號，例如「工單 12」。"
		}
		var plate, status string
		err = db.DB.QueryRow(`SELECT v.plate_number, wo.status
			FROM active_work_orders wo
			JOIN active_vehicles v ON v.id = wo.vehicle_id
			JOIN active_customers c ON c.id = wo.customer_id
			WHERE wo.id = ? AND c.line_user_id = ?`, woID, ev.UserID).Scan(&plate, &status)
		if err != nil {
			return "找不到這張工單，或它不是您的車輛。"
		}
		label := workOrderStatusLabel[status]
		if label == "" {
			label = status
		}
		return fmt.Sprintf("工單 #%d（%s）狀態：%s", woID, plate, label)

	default:
		return "您好，請輸入「進度」查詢車輛狀態、「工單 編號」查指定工單，或「履歷」查詢維修紀錄。"
	}
}
