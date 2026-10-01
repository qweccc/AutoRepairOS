package line

import (
	"database/sql"
	"log"

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
		"SELECT COALESCE(line_user_id,'') FROM customers WHERE id=?", n.CustomerID,
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

	if !c().Valid() {
		log.Printf("[line][MOCK] %s to=%s kind=%s", n.Kind, lineUserID, n.Subject)
		return nil
	}

	// 實際推播邏輯（Flex 估價單 or 文本）留到 Phase 4 補全，這裡先用 PushMessage 占位。
	return c().PushMessage(lineUserID, n.Body)
}

// c 回傳全域 Client（Mock 模式）。
func c() *Client {
	return NewClient(nil)
}

// Valid 判斷是否為 Mock 模式（無 token 時為 true）。
func (c *Client) Valid() bool {
	return c.cfg.Valid()
}
