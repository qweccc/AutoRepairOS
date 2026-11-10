package handler

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sort"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/integration/line"
)

const maxWebhookBody = 1 << 20 // 限制請求大小，避免被超大 body 拖垮。LINE 事件很小，1MB 綽綽有餘。

// log_line 統一帶前綴，方便在容器 log 中過濾。
func log_line(format string, args ...interface{}) {
	log.Printf("[line-webhook] "+format, args...)
}

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

func customerPassbookSummary(lineUserID string) string {
	var customerID int64
	if err := db.DB.QueryRow(`SELECT id FROM active_customers WHERE line_user_id = ?`, lineUserID).
		Scan(&customerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "查不到您的客戶綁定資料，請確認手機號碼是否已登錄。"
		}
		log_line("查詢維修履歷綁定失敗: %v", err)
		return "目前無法查詢維修履歷，請稍後再試。"
	}

	rows, err := db.DB.Query(`SELECT COALESCE(v.plate_number,''), COALESCE(date(pb.created_at),''),
		COALESCE(pb.service_type,''), COALESCE(pb.description,''),
		COALESCE(pb.total_amount,0), COALESCE(pb.status,'')
		FROM passbook pb
		JOIN active_customers c ON c.id = pb.customer_id
		JOIN active_vehicles v ON v.id = pb.vehicle_id AND v.customer_id = c.id
		JOIN active_work_orders wo ON wo.id = pb.work_order_id AND wo.vehicle_id = v.id
		WHERE c.id = ? AND c.line_user_id = ?
		ORDER BY pb.created_at DESC, pb.id DESC`, customerID, lineUserID)
	if err != nil {
		log_line("查詢客戶 #%d 維修履歷失敗: %v", customerID, err)
		return "目前無法查詢維修履歷，請稍後再試。"
	}
	defer rows.Close()

	var summary strings.Builder
	summary.WriteString("您的維修履歷：")
	count := 0
	for rows.Next() {
		var plate, date, serviceType, description, status string
		var amount float64
		if err := rows.Scan(&plate, &date, &serviceType, &description, &amount, &status); err != nil {
			log_line("讀取客戶 #%d 維修履歷失敗: %v", customerID, err)
			return "目前無法查詢維修履歷，請稍後再試。"
		}
		count++
		fmt.Fprintf(&summary, "\n\n%s｜%s｜%s\n項目：%s\n總金額：$%.0f｜狀態：%s",
			plate, date, passbookServiceTypeLabel(serviceType), description,
			amount, passbookStatusLabel(status))
	}
	if err := rows.Err(); err != nil {
		log_line("列舉客戶 #%d 維修履歷失敗: %v", customerID, err)
		return "目前無法查詢維修履歷，請稍後再試。"
	}
	if count == 0 {
		return "目前沒有維修履歷紀錄"
	}
	return summary.String()
}

func passbookServiceTypeLabel(serviceType string) string {
	switch serviceType {
	case "inspection":
		return "檢查"
	case "maintenance":
		return "保養"
	case "parts":
		return "零件"
	default:
		return "維修"
	}
}

func passbookStatusLabel(status string) string {
	switch status {
	case "pending":
		return "待付款"
	case "approved":
		return "已核准"
	case "paid":
		return "已付款"
	case "rejected":
		return "已拒絕"
	default:
		return status
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

func generateBindingCode() (string, error) {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	num := int(b[0])*256*256 + int(b[1])*256 + int(b[2])
	return fmt.Sprintf("%06d", num%1000000), nil
}
