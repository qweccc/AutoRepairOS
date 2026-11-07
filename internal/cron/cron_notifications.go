package cron

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/integration/line"
	"autorepairos/internal/integration/telegram"
)

// --- 通知佇列 ---

// QueueNotification 將通知寫入佇列，待 processNotifications 處理。
func QueueNotification(kind, channel, subject, body string, customerID, workOrderID int64, scheduledAt time.Time) error {
	var custArg, woArg interface{}
	if customerID > 0 {
		custArg = customerID
	}
	if workOrderID > 0 {
		woArg = workOrderID
	}

	_, err := db.DB.Exec(`INSERT INTO notifications
		(work_order_id, customer_id, channel, kind, subject, body, scheduled_at)
		VALUES (?,?,?,?,?,?,?)`,
		woArg, custArg, channel, kind, subject, body,
		scheduledAt.UTC().Format(dbTimeFormat))
	return err
}

// QueueWorkOrderCompletedNotification 寫入完工通知卡，並以工單 ID 冪等去重。
// 未設定有效的客戶通道綁定時只保留 internal 紀錄，不送往任何預設收件人。
func QueueWorkOrderCompletedNotification(woID, customerID int64) error {
	_, err := queueWorkOrderCompletedNotification(woID, customerID)
	return err
}

func queueWorkOrderCompletedNotification(woID, customerID int64) (bool, error) {
	var customerName, plate, preference, lineUserID, telegramUserID string
	var totalAmount float64
	err := db.DB.QueryRow(`SELECT COALESCE(c.name,''), COALESCE(v.plate_number,''),
		COALESCE(wo.total_amount,0), COALESCE(c.notification_preference,'none'),
		COALESCE(c.line_user_id,''), COALESCE(c.telegram_user_id,'')
		FROM active_work_orders wo
		JOIN active_customers c ON c.id = wo.customer_id
		JOIN active_vehicles v ON v.id = wo.vehicle_id AND v.customer_id = wo.customer_id
		WHERE wo.id = ? AND wo.customer_id = ? AND wo.status = 'completed'`, woID, customerID).
		Scan(&customerName, &plate, &totalAmount, &preference, &lineUserID, &telegramUserID)
	if err != nil {
		return false, fmt.Errorf("讀取完工工單 #%d 通知資料失敗: %w", woID, err)
	}

	rows, err := db.DB.Query(`SELECT description, COALESCE(quantity,1)
		FROM work_order_items WHERE work_order_id = ? ORDER BY id`, woID)
	if err != nil {
		return false, fmt.Errorf("讀取工單 #%d 項目摘要失敗: %w", woID, err)
	}
	type completedItem struct {
		description string
		quantity    int
	}
	var items []completedItem
	for rows.Next() {
		var item completedItem
		if err := rows.Scan(&item.description, &item.quantity); err != nil {
			rows.Close()
			return false, fmt.Errorf("讀取工單 #%d 項目摘要失敗: %w", woID, err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, fmt.Errorf("列舉工單 #%d 項目摘要失敗: %w", woID, err)
	}
	if err := rows.Close(); err != nil {
		return false, fmt.Errorf("關閉工單 #%d 項目摘要查詢失敗: %w", woID, err)
	}

	channel := "internal"
	switch {
	case preference == "line" && strings.TrimSpace(lineUserID) != "":
		channel = "line"
	case preference == "telegram" && strings.TrimSpace(telegramUserID) != "":
		channel = "telegram"
	}

	var body strings.Builder
	fmt.Fprintf(&body, "親愛的 %s 您好\n車牌：%s\n工單編號：#%d\n維修項目：",
		customerName, plate, woID)
	if len(items) == 0 {
		body.WriteString("\n- 無明細")
	} else {
		for i, item := range items {
			if i == 3 {
				fmt.Fprintf(&body, "\n另有 %d 項", len(items)-3)
				break
			}
			fmt.Fprintf(&body, "\n- %s x%d", item.description, item.quantity)
		}
	}
	fmt.Fprintf(&body, "\n工單總金額：$%.0f\n車輛已完成維修，歡迎來廠取車並結帳", totalAmount)

	subject := "完工通知 — 您的愛車已完成維修"
	result, err := db.DB.Exec(`INSERT INTO notifications
		(work_order_id, customer_id, channel, kind, subject, body, scheduled_at)
		SELECT ?, ?, ?, 'work_order_completed', ?, ?, ?
		WHERE NOT EXISTS (SELECT 1 FROM notifications
			WHERE kind='work_order_completed' AND work_order_id=?)`,
		woID, customerID, channel, subject, body.String(), dbTime(time.Now()), woID)
	if err != nil {
		return false, fmt.Errorf("寫入工單 #%d 完工通知失敗: %w", woID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("檢查工單 #%d 完工通知寫入結果失敗: %w", woID, err)
	}
	return affected == 1, nil
}

// PendingNotifications 列出待處理的佇列項目（供 UI 檢視）。
func PendingNotifications(limit int) []Notification {
	return queryNotifications(` WHERE status='pending' ORDER BY scheduled_at`, limit)
}

// RecentNotifications 列出最近的 notification 紀錄（含 pending/sent/cancelled/failed），
// 讓 UI 在排程器處理完佇列後仍能查到歷史。
func RecentNotifications(limit int) []Notification {
	return queryNotifications(` ORDER BY id DESC`, limit)
}

func queryNotifications(clause string, limit int) []Notification {
	rows, err := db.DB.Query(`SELECT id, COALESCE(work_order_id,0), COALESCE(customer_id,0),
		channel, kind, COALESCE(subject,''), body, scheduled_at, COALESCE(sent_at,''),
		status, COALESCE(retry_count,0), COALESCE(error_message,'')
		FROM notifications`+clause+` LIMIT ?`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.WorkOrderID, &n.CustomerID, &n.Channel, &n.Kind,
			&n.Subject, &n.Body, &n.ScheduledAt, &n.SentAt, &n.Status, &n.RetryCount, &n.ErrorMessage); err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

type Notification struct {
	ID           int64
	WorkOrderID  int64
	CustomerID   int64
	Channel      string
	Kind         string
	Subject      string
	Body         string
	ScheduledAt  string
	SentAt       string
	Status       string
	RetryCount   int
	ErrorMessage string
}

// processNotifications 由排程器定期呼叫；channel=internal 僅記錄不外送。
func processNotifications() error {
	_, err := processPendingNotifications()
	return err
}

// ProcessNotificationsNow 供 UI 手動觸發，回傳處理筆數。
func ProcessNotificationsNow() int {
	n, _ := processPendingNotifications()
	return n
}

const (
	maxNotificationRetries = 3
	notificationLease      = 2 * time.Minute
)

func notificationProcessingTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
}

var notificationRetryBackoff = [...]time.Duration{
	time.Minute,
	5 * time.Minute,
	15 * time.Minute,
}

type notificationItem struct {
	id, custID, workOrderID          int64
	channel, kind, subject           string
	body, lineUserID, telegramUserID string
}

func processPendingNotifications() (int, error) {
	cfg, client := line.Shared()
	telegramCfg, telegramClient := telegram.Shared()
	return processPendingNotificationsWithSender(func(it notificationItem) error {
		return deliver(cfg, client, telegramCfg, telegramClient, it)
	})
}

func processPendingNotificationsWithSender(send func(notificationItem) error) (int, error) {
	scanTime := time.Now().UTC()
	leaseExpired := notificationProcessingTime(scanTime.Add(-notificationLease))
	rows, err := db.DB.Query(`SELECT n.id, COALESCE(n.customer_id,0), n.channel, n.kind,
			COALESCE(n.subject,''), n.body, COALESCE(n.work_order_id,0),
			COALESCE(c.line_user_id,''), COALESCE(c.telegram_user_id,'')
		FROM notifications n
		LEFT JOIN active_customers c ON c.id = n.customer_id
		WHERE (n.status='pending' AND n.scheduled_at <= ?)
		   OR (n.status='processing' AND n.processing_at <= ?)
		ORDER BY n.scheduled_at LIMIT 50`, dbTime(scanTime), leaseExpired)
	if err != nil {
		return 0, err
	}

	var items []notificationItem
	for rows.Next() {
		var it notificationItem
		// Scan 的目標數必須與 SELECT 欄位数完全一致；不一致時 Scan 會回錯，
		// 而原本的 `continue` 會把整列靜默丟棄，結果是「查得到但一筆都沒處理」。
		if err := rows.Scan(&it.id, &it.custID, &it.channel, &it.kind,
			&it.subject, &it.body, &it.workOrderID, &it.lineUserID, &it.telegramUserID); err != nil {
			log.Printf("通知 #%d 欄位掃描失敗，放棄處理: %v", it.id, err)
			continue
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	var sent, failed, claimed int
	for _, it := range items {
		now := time.Now().UTC()
		leaseExpired = notificationProcessingTime(now.Add(-notificationLease))
		ok, claimToken, err := claimNotification(it.id, now, leaseExpired)
		if err != nil {
			return claimed, err
		}
		if !ok {
			continue
		}
		claimed++

		switch it.channel {
		case "internal", "none":
			// 不外送，直接標記完成，讓佇列不阻塞。
			if err := markSent(it.id, claimToken); err != nil {
				return claimed, err
			}
			sent++

		case "line":
			if it.lineUserID == "" {
				if err := markFailed(it.id, "客戶未綁定 LINE user ID", true, now, claimToken); err != nil {
					return claimed, err
				}
				failed++
				continue
			}
			if err := send(it); err != nil {
				if markErr := markFailed(it.id, err.Error(), isPermanentNotificationError(err), now, claimToken); markErr != nil {
					return claimed, markErr
				}
				failed++
				continue
			}
			if err := markSent(it.id, claimToken); err != nil {
				return claimed, err
			}
			sent++

		case "sms":
			// SMS 通道已停用（FR-CRM-04），必須標記為永久失敗並提示更新
			if err := markFailed(it.id, "SMS 通道已停用，請將客戶通知偏好更新為 LINE 或 Telegram", true, now, claimToken); err != nil {
				return claimed, err
			}
			failed++
			continue

		case "telegram":
			if strings.TrimSpace(it.telegramUserID) == "" {
				if err := markFailed(it.id, "客戶未綁定 Telegram user ID", true, now, claimToken); err != nil {
					return claimed, err
				}
				failed++
				continue
			}
			if err := send(it); err != nil {
				if markErr := markFailed(it.id, err.Error(), isPermanentNotificationError(err), now, claimToken); markErr != nil {
					return claimed, markErr
				}
				failed++
				continue
			}
			if err := markSent(it.id, claimToken); err != nil {
				return claimed, err
			}
			sent++

		default:
			if err := markFailed(it.id, "不支援的通知通道: "+it.channel, true, now, claimToken); err != nil {
				return claimed, err
			}
			failed++
		}
	}

	if claimed > 0 {
		log.Printf("處理 %d 筆通知：送達 %d、失敗 %d", claimed, sent, failed)
	}
	return claimed, nil
}

// deliver 實際外送一則通知。LINE 未設定憑證時由 client 進入 MOCK，只記錄不外送。
func deliver(cfg *line.Config, client *line.Client, telegramCfg *telegram.Config, telegramClient *telegram.Client, it notificationItem) error {
	if it.channel == "telegram" {
		// 未設定 token 時為永久失敗（不應假成功）
		if telegramCfg == nil || !telegramCfg.Valid() {
			return permanentNotificationError(fmt.Errorf("Telegram Bot Token 未設定"))
		}
		return telegramClient.SendMessage(it.telegramUserID, it.body)
	}
	// SMS 通道已停用（FR-CRM-04），應在 processPendingNotifications 中標記失敗
	// 不應到達此處。若到達視為程式錯誤。
	if it.channel == "sms" {
		return permanentNotificationError(fmt.Errorf("SMS 通道已停用"))
	}
	return client.PushMessage(it.lineUserID, it.body)
}

func claimNotification(id int64, now time.Time, leaseExpired string) (bool, string, error) {
	claimToken := notificationProcessingTime(now)
	res, err := db.DB.Exec(`UPDATE notifications
		SET status='processing', processing_at=?
		WHERE id=? AND ((status='pending' AND scheduled_at<=?)
			OR (status='processing' AND processing_at<=?))`,
		claimToken, id, dbTime(now), leaseExpired)
	if err != nil {
		return false, "", err
	}
	n, err := res.RowsAffected()
	return n == 1, claimToken, err
}

func markSent(id int64, claimToken string) error {
	res, err := db.DB.Exec(`UPDATE notifications SET status='sent', sent_at=CURRENT_TIMESTAMP,
		processing_at=NULL, error_message=NULL WHERE id=? AND status='processing' AND processing_at=?`,
		id, claimToken)
	return requireClaimedUpdate(res, err, id)
}

func markFailed(id int64, reason string, permanent bool, now time.Time, claimToken string) error {
	var status string
	var scheduledAt string
	if permanent {
		status = "permanent_failure"
	} else {
		retryCount, err := currentRetryCount(id, claimToken)
		if err != nil {
			return err
		}
		if retryCount >= maxNotificationRetries {
			status = "permanent_failure"
		} else {
			status = "pending"
			scheduledAt = dbTime(now.Add(notificationRetryBackoff[retryCount]))
		}
	}
	res, err := db.DB.Exec(`UPDATE notifications SET status=?, retry_count=retry_count+1,
		scheduled_at=CASE WHEN ?='' THEN scheduled_at ELSE ? END,
		processing_at=NULL, error_message=? WHERE id=? AND status='processing' AND processing_at=?`,
		status, scheduledAt, scheduledAt, reason, id, claimToken)
	return requireClaimedUpdate(res, err, id)
}

func requireClaimedUpdate(res sql.Result, err error, id int64) error {
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("通知 #%d 已不由此 worker 持有", id)
	}
	return nil
}

func currentRetryCount(id int64, claimToken string) (int, error) {
	var count int
	err := db.DB.QueryRow(`SELECT retry_count FROM notifications
		WHERE id=? AND status='processing' AND processing_at=?`, id, claimToken).Scan(&count)
	return count, err
}

type permanentDeliveryError struct{ err error }

func (e permanentDeliveryError) Error() string { return e.err.Error() }
func (e permanentDeliveryError) Unwrap() error { return e.err }

func permanentNotificationError(err error) error {
	return permanentDeliveryError{err: err}
}

func isPermanentNotificationError(err error) bool {
	var permanent permanentDeliveryError
	if errors.As(err, &permanent) {
		return true
	}
	var apiErr *line.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 &&
			apiErr.StatusCode != 408 && apiErr.StatusCode != 429
	}
	return false
}
