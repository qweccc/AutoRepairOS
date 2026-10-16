package line

import (
	"database/sql"
	"log"
	"sync"

	"autorepairos/internal/db"
)

// Notification 為 LINE 通知佇列項目（與 cron.Notification 欄位對齊，避免循環匯入）。
type Notification struct {
	ID          int64
	CustomerID  int64
	Channel     string
	Kind        string
	Subject     string
	Body        string
}

// DeliverNotification 把一條 LINE 通道通知送到 LINE（用客戶的 line_user_id）。
// 無 token 時走 Mock，只 logging，不阻塞佇列。
func DeliverNotification(n Notification) error {
	var lineUserID string
	err := db.DB.QueryRow(
		"SELECT COALESCE(line_user_id,'') FROM active_customers WHERE id=?", n.CustomerID,
	).Scan(&lineUserID)
	if err == sql.ErrNoRows {
		log.Printf("[line] 客戶 %d 不存在，略過: %v", n.CustomerID, err)
		return nil
	}
	if err != nil {
		log.Printf("[line] 找不到客戶 %d 的 line_user_id: %v", n.CustomerID, err)
		return nil
	}
	if lineUserID == "" {
		log.Printf("[line] 客戶 %d 未綁定 line_user_id，略過", n.CustomerID)
		return nil
	}

	if !shared.ready() {
		log.Printf("[line][MOCK] %s to=%s kind=%s", n.Kind, lineUserID, n.Subject)
		return nil
	}

	// 估價單類通知走 Flex Message（明細+金額+核准按鈕），其餘走純文字。
	if n.Kind == "estimate_approval" || n.Kind == "work_order_ready" {
		// 這些 kind 的 body 已由 handler 組成純文字版；Flex 由呼叫端另行處理。
		return shared.pushText(lineUserID, n.Subject, n.Body)
	}
	return shared.pushText(lineUserID, n.Subject, n.Body)
}

// shared 是共用 client，避免每次呼叫都重建（也確保讀到啟動時的設定）。
var shared = NewLazyClient()

// LazyClient 延遲載入設定，讓測試可以在設定環境變數後再初始化。
type LazyClient struct {
	once sync.Once
	cfg  *Config
	cli  *Client
}

func NewLazyClient() *LazyClient { return &LazyClient{} }

func (l *LazyClient) load() (*Config, *Client) {
	l.once.Do(func() {
		l.cfg, l.cli = Shared()
	})
	return l.cfg, l.cli
}

// ready 判斷憑證是否足夠實際外送。
func (l *LazyClient) ready() bool {
	cfg, _ := l.load()
	return cfg.Valid()
}

// pushText 送出純文字訊息。
func (l *LazyClient) pushText(to, title, body string) error {
	_, cli := l.load()
	text := body
	if title != "" {
		text = title + "\n" + body
	}
	return cli.PushMessage(to, text)
}
