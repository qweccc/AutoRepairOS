package handler

import (
	"fmt"
	"io"
	"log"
	"net/http"
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
		log_line("webhook 簽名驗證失敗（可能是不合法的請求）")
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

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
	for _, ev := range event.FollowEvents() {
		bindLineUser(ev.UserID)
		handled++
		if ev.ReplyToken != "" {
			_ = client.ReplyMessage(ev.ReplyToken, welcomeMessage())
		}
	}

	// 2) 客戶點按鈕（例如估價單的「線上核准」）
	for _, ev := range event.PostbackEvents() {
		handlePostback(client, ev)
		handled++
	}

	// 3) 客戶傳訊息 → 查詢工單進度
	for _, ev := range event.MessageEvents() {
		reply := handleCustomerMessage(client, ev)
		if ev.ReplyToken != "" && reply != "" {
			_ = client.ReplyMessage(ev.ReplyToken, reply)
		}
		handled++
	}

	log_line("webhook 處理完成：%d 個事件", handled)
	w.WriteHeader(http.StatusOK)
}

// bindLineUser 記錄（或更新）某個 LINE user ID 綁定的客戶。
//
// 一個 LINE 帳號理論上對應一台車的業主。若該 ID 已被其他客戶佔用，
// 就不再改綁，避免有人加好友就把別人的資料搶走。
func bindLineUser(lineUserID string) {
	if strings.TrimSpace(lineUserID) == "" {
		return
	}

	var existing int
	err := db.DB.QueryRow(
		"SELECT COUNT(1) FROM customers WHERE line_user_id = ?", lineUserID,
	).Scan(&existing)
	if err != nil {
		log_line("查詢 line_user_id 時失敗: %v", err)
		return
	}

	if existing > 0 {
		log_line("LINE user %s 已綁定客戶，略過", lineUserID)
		return
	}

	// 還沒綁定。找出台數最少的車主（車行裡常見：一個人可能同時是幾台車的業主），
	// 但沒有明確對應時不自動猜——請櫃檯在客戶頁手動指定，避免綁錯人。
	//
	// 這裡採保守做法：記錄成一筆「待綁定」訊息，由櫃檯在 UI 處理。
	log_line("收到新的 LINE user %s（尚未對應客戶，需手動綁定）", lineUserID)
	db.MustExec(`INSERT INTO notifications
		(customer_id, channel, kind, subject, body, scheduled_at, status)
		VALUES (NULL, 'line', 'line_follow', '新的 LINE 好友',
			?, CURRENT_TIMESTAMP, 'pending')`,
		"LINE user ID: "+lineUserID+" — 請於客戶頁填入對應客戶")
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

func welcomeMessage() string {
	return "您好，歡迎加入汽車修的 LINE。為您建立通知關聯後，車輛保養與維修進度會自動通知您。"
}
