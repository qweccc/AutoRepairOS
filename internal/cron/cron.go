package cron

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/integration/line"
	"autorepairos/internal/integration/telegram"
)

// --- 內建任務註冊表 ---

type Task struct {
	Key      string
	Name     string
	Interval time.Duration // 每 N 分鐘執行一次；0 表示停用
	Run      func() error
}

var registry = make(map[string]Task)

// Register 新增一個排程任務。init() 階段呼叫。
func Register(t Task) {
	registry[t.Key] = t
}

// Scheduler 定期檢查並執行到期任務。
type Scheduler struct {
	stop   chan struct{}
	once   sync.Once
	tickWG sync.WaitGroup // 追蹤進行中的 tick；Stop 等它完成，避免關閉時併發存取 DB
}

// New 建立排程器並把任務同步到 scheduled_tasks 表。
func New() (*Scheduler, error) {
	for _, t := range registry {
		if err := syncTask(t); err != nil {
			return nil, err
		}
	}
	return &Scheduler{stop: make(chan struct{})}, nil
}

// dbTimeFormat 必須與 SQLite 驅動儲存 datetime 參數的格式一致。
// modernc.org/sqlite 會把 time.Time 轉成 RFC3339（2026-09-30T10:00:06Z），
// 若解析時用不同格式比對，就會每次都誤判為「已到期」而重複執行。
const dbTimeFormat = time.RFC3339

// dbTime 統一以 UTC 格式化時間字串。
func dbTime(t time.Time) string {
	return t.UTC().Format(dbTimeFormat)
}

func nowDBTime() string {
	return dbTime(time.Now())
}

// sqlTimeLayout 與 SQLite CURRENT_TIMESTAMP 相同：YYYY-MM-DD HH:MM:SS (UTC)。
const sqlTimeLayout = "2006-01-02 15:04:05"

func syncTask(t Task) error {
	var interval interface{}
	if t.Interval > 0 {
		interval = int(t.Interval.Minutes())
	}

	var nextRun interface{}
	if t.Interval > 0 {
		nextRun = nowDBTime()
	}

	_, err := db.DB.Exec(`INSERT INTO scheduled_tasks
		(task_key, name, interval_minutes, enabled, next_run_at)
		VALUES (?,?,?,?,?)
		ON CONFLICT(task_key) DO UPDATE SET
			name=excluded.name,
			interval_minutes=excluded.interval_minutes,
			enabled=CASE WHEN scheduled_tasks.interval_minutes IS NULL
			              AND excluded.interval_minutes IS NOT NULL THEN 1
			             ELSE scheduled_tasks.enabled END`,
		t.Key, t.Name, interval, boolToInt(t.Interval > 0), nextRun)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Start 啟動背景執行迴圈，每分鐘檢查一次到期任務。
func (s *Scheduler) Start() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()

		// 啟動時先跑一次到期檢查
		s.tickWG.Add(1)
		s.tick()
		s.tickWG.Done()

		for {
			select {
			case <-ticker.C:
				s.tickWG.Add(1)
				s.tick()
				s.tickWG.Done()
			case <-s.stop:
				log.Println("排程器已停止")
				return
			}
		}
	}()
	log.Printf("排程器已啟動，註冊 %d 個任務", len(registry))
}

// Stop 停止排程器：不再啟動新 tick，並等待進行中的 tick（可能正在跑備份）
// 完成後才返回，避免關閉服務時 DB 被併發存取（VACUUM INTO / checkpoint 撞
// SQLITE_BUSY）。最多等 60 秒，避免單一任務卡死拖住整個關閉流程。
func (s *Scheduler) Stop() {
	s.once.Do(func() {
		close(s.stop)
		done := make(chan struct{})
		go func() {
			s.tickWG.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(60 * time.Second):
			log.Println("排程任務未在 60 秒內結束，強制繼續關閉")
		}
	})
}

func (s *Scheduler) tick() {
	for key, t := range registry {
		if t.Interval <= 0 {
			continue
		}
		if !isEnabled(key) {
			continue
		}
		if !isDue(key) {
			continue
		}

		if err := runTask(t); err != nil {
			log.Printf("任務 %s 執行失敗: %v", t.Name, err)
		}
	}
}

func runTask(t Task) error {
	defer func() {
		// panic 防護：單一任務崩潰不影響排程器
		if r := recover(); r != nil {
			log.Printf("任務 %s panic: %v", t.Name, r)
		}
	}()

	var errMsg interface{}
	if err := t.Run(); err != nil {
		errMsg = err.Error()
	}

	var nextRun interface{}
	if t.Interval > 0 {
		nextRun = dbTime(time.Now().Add(t.Interval))
	}

	// 必須顯式檢查錯誤：VACUUM INTO 等操作會暫時鎖住唯一連線，
	// 若靜默失敗 next_run_at 不會更新，任務會每分鐘重複執行。
	if _, err := db.DB.Exec(`UPDATE scheduled_tasks SET
		last_run_at = CURRENT_TIMESTAMP,
		next_run_at = ?,
		run_count = COALESCE(run_count,0) + 1,
		last_error = ?
		WHERE task_key = ?`, nextRun, errMsg, t.Key); err != nil {
		return fmt.Errorf("更新任務狀態失敗（next_run_at 可能未更新，該任務將重複執行）: %w", err)
	}

	return nil
}

func isEnabled(key string) bool {
	var en int
	if err := db.DB.QueryRow("SELECT enabled FROM scheduled_tasks WHERE task_key = ?", key).Scan(&en); err != nil {
		return false
	}
	return en == 1
}

func isDue(key string) bool {
	var nextRun sql.NullString
	if err := db.DB.QueryRow("SELECT next_run_at FROM scheduled_tasks WHERE task_key = ?", key).
		Scan(&nextRun); err != nil {
		return true // 尚未初始化，立即執行
	}
	if !nextRun.Valid || strings.TrimSpace(nextRun.String) == "" {
		return true
	}

	t, err := parseDBTime(nextRun.String)
	if err != nil {
		log.Printf("任務 %s 的 next_run_at 格式錯誤 (%q): %v，下次重設", key, nextRun.String, err)
		return true
	}
	return time.Now().UTC().After(t)
}

// parseDBTime 支援驅動可能寫入的兩種格式：RFC3339 與舊的 SQL datetime。
func parseDBTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	// 無時區資訊時視為 UTC，與 CURRENT_TIMESTAMP 一致。
	return time.ParseInLocation("2006-01-02 15:04:05", s, time.UTC)
}

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

// --- 每日備份 ---

// BackupConfig 設定備份目標。
type BackupConfig struct {
	BackupDir string
	PhotoDir  string
}

var backupCfg = defaultBackupConfig()


func init() {
	Register(Task{
		Key:      "process_notifications",
		Name:     "通知佇列處理",
		Interval: 1 * time.Minute,
		Run:      processNotifications,
	})

	Register(Task{
		Key:      "daily_backup",
		Name:     "每日自動備份",
		Interval: 24 * time.Hour,
		Run:      DailyBackup,
	})

	Register(Task{
		Key:      "work_order_ready",
		Name:     "完工通知",
		Interval: 6 * time.Hour,
		Run:      scanWorkOrderReady,
	})

	Register(Task{
		Key:      "maintenance_due_scan",
		Name:     "車輛到期提醒掃描",
		Interval: 24 * time.Hour,
		Run:      scanMaintenanceDue,
	})

	Register(Task{
		Key:      "appointment_reminder_scan",
		Name:     "預約前日提醒掃描",
		Interval: 24 * time.Hour,
		Run:      scanAppointmentReminders,
	})

	Register(Task{
		Key:      "birthday_reminder_scan",
		Name:     "客戶生日關懷掃描",
		Interval: 24 * time.Hour,
		Run:      scanBirthdayReminders,
	})
}

// scanMaintenanceDue 每日掃描保養、定檢及保單到期項目，寫入通知佇列。

// --- 除錯用 ---

// Tasks 回傳目前註冊的所有任務（供 UI 顯示）。
func Tasks() []Task {
	keys := make([]string, 0, len(registry))
	for k := range registry {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]Task, 0, len(keys))
	for _, k := range keys {
		out = append(out, registry[k])
	}
	return out
}

// TaskStatus 讀取資料庫中的執行狀態。
type TaskStatus struct {
	Task
	Enabled       bool
	LastRunAt     string
	NextRunAt     string
	RunCount      int
	LastError     string
	IntervalLabel string
}

func Status() []TaskStatus {
	rows, err := db.DB.Query(`SELECT task_key, name, COALESCE(interval_minutes,0),
		COALESCE(enabled,0), COALESCE(last_run_at,''), COALESCE(next_run_at,''),
		COALESCE(run_count,0), COALESCE(last_error,'')
		FROM scheduled_tasks ORDER BY name`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []TaskStatus
	for rows.Next() {
		var s TaskStatus
		var mins, en int
		if err := rows.Scan(&s.Key, &s.Name, &mins, &en, &s.LastRunAt, &s.NextRunAt,
			&s.RunCount, &s.LastError); err != nil {
			continue
		}
		s.Enabled = en == 1
		s.Interval = time.Duration(mins) * time.Minute
		s.IntervalLabel = formatInterval(mins)
		out = append(out, s)
	}
	return out
}

func formatInterval(mins int) string {
	switch {
	case mins <= 0:
		return "停用"
	case mins < 60:
		return strconv.Itoa(mins) + " 分鐘"
	case mins%1440 == 0:
		return strconv.Itoa(mins/1440) + " 天"
	default:
		return strconv.Itoa(mins/60) + " 小時"
	}
}
