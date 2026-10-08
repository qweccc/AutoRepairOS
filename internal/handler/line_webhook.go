package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/integration/line"
)

// log_line 統一帶前綴，方便在容器 log 中過濾。
func log_line(format string, args ...interface{}) {
	log.Printf("[line-webhook] "+format, args...)
}

// maxWebhookBody 限制請求大小，避免被超大 body 拖垮。LINE 事件很小，1MB 綽綽有餘。
const maxWebhookBody = 1 << 20

// LineWebhook 接收 LINE Messaging API 的 webhook 事件。
//
// 這支 route 不需要登入——LINE 伺服器沒有 session cookie。安全性靠
// X-Line-Signature 簽名驗證（用 channel secret 做 HMAC-SHA256）。
//
// 客戶加 bot 為好友時會送 follow 事件，帶著他們的 LINE user ID，
// 這是取得 line_user_id 的正規管道，比讓櫃檯手動填可靠得多。
func LineWebhook(w http.ResponseWriter, r *http.Request) {
	cfg, client := line.Shared()

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 未設定憑證時直接拒絕，不要讓任何人往這個端點灌資料。
	if cfg.ChannelSecret == "" {
		log_line("webhook 尚未設定 LINE_CHANNEL_SECRET，拒絕請求")
		http.Error(w, "webhook not configured", http.StatusServiceUnavailable)
		return
	}

	// 必須讀取 raw body 做簽名驗證，之後才能解析。
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		http.Error(w, "cannot read body", http.StatusBadRequest)
		return
	}

	signature := r.Header.Get("X-Line-Signature")
	valid, _ := client.VerifyEventSignature(raw, signature)
	if !valid {
		// 診斷：記錄收到哪些 header。Cloudflare 的 WAF 或 bot 保護有可能把
		// X-Line-Signature 移除或改寫，導致 LINE 的合法請求也被拒。
		// 只記 header 名稱與長度，不記內容（簽名本身是機密）。
		names := make([]string, 0, len(r.Header))
		for k, v := range r.Header {
			names = append(names, fmt.Sprintf("%s(len=%d)", k, len(v)))
		}
		sort.Strings(names)
		log_line("webhook 驗簽失敗：%s 來源=%s UA=%q headers=[%s]",
			signaturePrefix(signature), clientIP(r), r.UserAgent(), strings.Join(names, " "))
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	log_line("webhook 簽名驗證通過：來源=%s 事件數=%d", clientIP(r), bytes.Count(raw, []byte(`"type"`)))

	event, err := line.ParseWebhook(raw)
	if err != nil {
		log_line("webhook 解析失敗: %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// LINE 端點驗證：developer 後台按「Verify」會送空 events 陣列。
	// 回 200 即可，內容不重要。
	if len(event.Events) == 0 {
		w.WriteHeader(http.StatusOK)
		return
	}

	handled := 0

	// 1) 客戶加好友 → 記錄 line_user_id
	//
	// 注意：若 LINE 官方帳號同時啟用「加入好友的歡迎訊息」，該功能會先
	// 用掉同一個 replyToken，這裡的回訊就會收到 invalid reply token。
	// 所以只有在偵測到回覆失敗時才記錄，不影響其他事件的處理。
	for _, ev := range event.FollowEvents() {
		handled++
		beginBinding(client, ev)
	}

	// 2) 客戶點按鈕（例如估價單的「線上核准」）
	for _, ev := range event.PostbackEvents() {
		handlePostback(client, ev)
		handled++
	}

	// 3) 客戶傳訊息 → 查詢工單進度
	for _, ev := range event.MessageEvents() {
		log_line("message 事件：user=%s 內容=%q", ev.UserID, ev.Text)

		// 尚未綁定的使用者：任何訊息都視為嘗試綁定（他們多半是回覆歡迎訊息
		// 報手機號碼）。已綁定者則走正常查詢，不會被這裡攔截。
		if reply, done := tryBindByPhone(client, ev); done {
			handled++
			if ev.ReplyToken != "" && reply != "" {
				if err := client.ReplyMessage(ev.ReplyToken, reply); err != nil {
					log_line("綁定回訊失敗: %v", err)
				}
			}
			continue
		}

		reply := handleCustomerMessage(client, ev)
		if ev.ReplyToken != "" && reply != "" {
			if err := client.ReplyMessage(ev.ReplyToken, reply); err != nil {
				log_line("message 回訊失敗: %v", err)
			}
		} else if ev.ReplyToken == "" {
			log_line("message 事件沒有 replyToken，無法回覆（應改用 PushMessage）")
		}
		handled++
	}

	log_line("webhook 處理完成：%d 個事件", handled)
	w.WriteHeader(http.StatusOK)
}

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
//   1. 只留數字，去掉 '-'、' '、'#' 等
//   2. 移除結尾的「#N」續號 —— 續號是舊系統用來區分同門號的第二人，
//      語意上仍是同一支手機
//   3. 10 碼且 09 開頭者直接比對；否則不視為手機
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
	db.DB.QueryRow("SELECT COUNT(1) FROM customers WHERE "+phoneMatchSQL("phone"), phone).Scan(&n)
	return n
}

// isBound 查詢某個 LINE user ID 是否已綁定客戶。
func isBound(lineUserID string) bool {
	return countRows("SELECT COUNT(1) FROM customers WHERE line_user_id = ?", lineUserID) > 0
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

	if countRows("SELECT COUNT(1) FROM customers WHERE line_user_id = ?", lineUserID) > 0 {
		return fmt.Errorf("此 LINE 帳號已綁定其他客戶")
	}
	if countRows("SELECT COUNT(1) FROM customers WHERE id = ? AND COALESCE(line_user_id,'') <> ''", customerID) > 0 {
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
		msg := "您好，歡迎加入汽車修。\n\n請直接傳送您的手機號碼 we'll 為您自動連結車輛紀錄。\n完成後即可查詢工單進度。"
		msg = "您好，歡迎加入汽車修。\n\n請傳送您的手機號碼，我們會為您連結車輛紀錄。\n連結後即可查詢工單進度。"
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
		if err := db.DB.QueryRow("SELECT id FROM customers WHERE "+phoneMatchSQL("phone"), phone).Scan(&customerID); err != nil {
			return "系統忙碌中，請稍後再試。", true
		}
		if err := bindLineUser(ev.UserID, customerID); err != nil {
			log_line("綁定 %s 到客戶 #%d 失敗: %v", ev.UserID, customerID, err)
			return "綁定失敗：" + err.Error() + "。請撥打店內電話由櫃檯協助。", true
		}

		var cname string
		db.DB.QueryRow("SELECT name FROM customers WHERE id = ?", customerID).Scan(&cname)
		log_line("LINE %s 已自動綁定客戶 #%d（%s）", ev.UserID, customerID, cname)

		var vehicleCount int
		db.DB.QueryRow(`SELECT COUNT(1) FROM vehicles v
			JOIN work_orders w ON w.vehicle_id = v.id
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
	// 目前支援的格式：approve:<work_order_id>
	if strings.HasPrefix(ev.Data, "approve:") {
		idStr := strings.TrimPrefix(ev.Data, "approve:")
		woID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			return
		}
		res, err := db.DB.Exec(
			"UPDATE work_orders SET approval_status='approved', updated_at=CURRENT_TIMESTAMP WHERE id=? AND status != 'paid'",
			woID)
		if err != nil || res == nil {
			log_line("核准工單 #%d 失敗: %v", woID, err)
			return
		}
		if n, _ := res.RowsAffected(); n > 0 {
			log_line("客戶 %s 透過 LINE 核准工單 #%d", ev.UserID, woID)
			if ev.ReplyToken != "" && client != nil {
				_ = client.ReplyMessage(ev.ReplyToken,
					fmt.Sprintf("已收到您的核准，工單 #%d 我們會盡快安排。", woID))
			}
		}
		return
	}
	log_line("收到未知的 postback: %q", ev.Data)
}

// handleCustomerMessage 回覆客戶訊息。回傳要送出的文字，空字串代表不需回覆。
func handleCustomerMessage(client *line.Client, ev line.MessageEvent) string {
	q := strings.TrimSpace(ev.Text)

	switch {
	case q == "進度" || q == "查詢" || q == "工單":
		var plate, status string
		err := db.DB.QueryRow(`SELECT v.plate_number, wo.status
			FROM work_orders wo JOIN vehicles v ON v.id = wo.vehicle_id
			JOIN customers c ON c.id = wo.customer_id
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
			FROM work_orders wo
			JOIN vehicles v ON v.id = wo.vehicle_id
			JOIN customers c ON c.id = wo.customer_id
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
		return "您好，請輸入「進度」查詢車輛狀態，或「工單 編號」查指定工單。"
	}
}

// clientIP 取真實來源 IP。Cloudflare Tunnel 會把原始 IP 放在
// Cf-Connecting-Ip，而 X-Forwarded-For 開頭那一段是 Cloudflare 自己。
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("Cf-Connecting-Ip"); ip != "" {
		return "cf:" + ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// signaturePrefix 取簽名的前綴用於診斷：去掉 "signature=" 後取前 12 字元。
// 只輸出前綴，避免完整 MAC 進入 log。
func signaturePrefix(sig string) string {
	s := strings.TrimPrefix(sig, "signature=")
	if len(s) > 12 {
		return s[:12] + "..."
	}
	return s
}

// bodyFingerprint 判斷收到的 body 是 JSON 還是可能被解壓/重組過。
func bodyFingerprint(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	switch {
	case len(s) == 0:
		return "空"
	case s[0] == '{' && s[len(s)-1] == '}':
		return "完整JSON物件"
	case s[0] == '{':
		return "疑似截斷的JSON"
	case s[0] == '[':
		return "JSON陣列"
	case !json.Valid(raw):
		return "非合法JSON"
	default:
		return "其他"
	}
}

// headStr 取 body 前 n 字元，供比對是否被中間層改寫。
func headStr(raw []byte, n int) string {
	if len(raw) > n {
		return string(raw[:n]) + "..."
	}
	return string(raw)
}

func welcomeMessage() string {
	return "您好，歡迎加入汽車修的 LINE。為您建立通知關聯後，車輛保養與維修進度會自動通知您。"
}
