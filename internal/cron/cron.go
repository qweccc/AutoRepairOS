package cron

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"autorepairos/internal/config"
	"autorepairos/internal/db"
	"autorepairos/internal/integration/line"
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

// dedupSince 回傳去重時間界線：現在往前一個排程週期（取最長的間隔）。
// 用它取代 status='pending' 判斷，可避免已送達的通知在下一輪掃描被重複排入。
//
// 格式必須與 notifications.created_at 完全一致。該欄位的 DEFAULT 是
// CURRENT_TIMESTAMP，寫出來是 SQL datetime（"2026-10-05 06:51:07"），
// 而 dbTime() 產出 RFC3339（"2026-10-05T06:51:07Z"）。用字串比較時
// 空格(0x20) < 'T'(0x54)，所以 SQL 格式永遠被判為「早於」RFC3339，
// 會讓去重條件恆成立、每次掃描都跳過所有車輛。
func dedupSince() string {
	interval := 6 * time.Hour
	for _, t := range registry {
		if t.Interval > interval {
			interval = t.Interval
		}
	}
	return time.Now().Add(-interval).UTC().Format(sqlTimeLayout)
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
	id, custID, workOrderID int64
	channel, kind, subject  string
	body, lineUserID        string
}

func processPendingNotifications() (int, error) {
	cfg, client := line.Shared()
	return processPendingNotificationsWithSender(func(it notificationItem) error {
		return deliver(cfg, client, it)
	})
}

func processPendingNotificationsWithSender(send func(notificationItem) error) (int, error) {
	scanTime := time.Now().UTC()
	leaseExpired := notificationProcessingTime(scanTime.Add(-notificationLease))
	rows, err := db.DB.Query(`SELECT n.id, COALESCE(n.customer_id,0), n.channel, n.kind,
			COALESCE(n.subject,''), n.body, COALESCE(n.work_order_id,0),
			COALESCE(c.line_user_id,'')
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
			&it.subject, &it.body, &it.workOrderID, &it.lineUserID); err != nil {
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

		case "line", "sms":
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
func deliver(cfg *line.Config, client *line.Client, it notificationItem) error {
	// SMS 通道尚未接電信商商；LINE 已接。SMS 改走 LINE 推播並在訊息標註，
	// 避免整筆失敗造成佇列堆積。真正接 SMS 時應拆成獨立分支。
	if it.channel == "sms" && !cfg.Ready() {
		return permanentNotificationError(fmt.Errorf("SMS 通道尚未整合，且 LINE 未設定憑證"))
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

var backupCfg = BackupConfig{BackupDir: "data/backups", PhotoDir: "data/photos"}

// SetBackupDir 覆寫備份輸出目錄。
func SetBackupDir(dir string) {
	if dir != "" {
		backupCfg.BackupDir = dir
	}
}

// DailyBackup 備份 SQLite DB 與統計照片數量/大小。
func DailyBackup() error {
	if err := os.MkdirAll(backupCfg.BackupDir, 0755); err != nil {
		return recordBackup("", 0, 0, 0, "建立備份目錄失敗: "+err.Error())
	}

	stamp := time.Now().Format("20060102_150405")
	dbDst := filepath.Join(backupCfg.BackupDir, "autorepair_"+stamp+".db")

	// WAL 模式下直接複製 .db 會漏掉尚未 checkpoint 的資料（-wal/-shm），
	// 必須走 SQLite 線上備份 API 或先做 WAL checkpoint。
	size, err := backupDB(dbDst)
	if err != nil {
		return recordBackup(dbDst, 0, 0, 0, "備份 DB 失敗: "+err.Error())
	}

	// 保留最近 30 天
	pruneBackups(30 * 24 * time.Hour)

	// 同一天重複執行（例如排程器誤判或手動重試）只保留最新一份，避免堆積無用備份。
	pruneSameDay(stamp[:8])

	photoCount, photoSize := dirStats(backupCfg.PhotoDir)
	log.Printf("每日備份完成: %s (%d bytes, 照片 %d 張 / %d bytes)", dbDst, size, photoCount, photoSize)
	return recordBackup(dbDst, int64(size), int64(photoCount), int64(photoSize), "")
}

func recordBackup(path string, size int64, photoCount, photoSize int64, errMsg string) error {
	status := "success"
	if errMsg != "" {
		status = "failed"
	}
	_, err := db.DB.Exec(`INSERT INTO backup_records
		(backup_date, db_backup_path, db_size, photo_count, photo_size, status, error_message)
		VALUES (?,?,?,?,?,?,?)`,
		time.Now().Format("2006-01-02"), path, size, photoCount, photoSize, status, errMsg)
	return err
}

// pruneSameDay 刪除同一日期（YYYYMMDD）的舊備份，只保留剛寫入的 keepFile。
func pruneSameDay(day string) {
	entries, err := os.ReadDir(backupCfg.BackupDir)
	if err != nil {
		return
	}
	prefix := "autorepair_" + day
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".db") {
			continue
		}
		if !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		// 檔名格式 autorepair_YYYYMMDD_HHMMSS.db；跳過目前時間戳（最新那份）
		if len(e.Name()) == len(prefix)+10 {
			continue
		}
		os.Remove(filepath.Join(backupCfg.BackupDir, e.Name()))
	}
}

func pruneBackups(maxAge time.Duration) {
	entries, err := os.ReadDir(backupCfg.BackupDir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".db") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(backupCfg.BackupDir, e.Name()))
		}
	}
}

// backupDB 使用 SQLite 線上備份 API（VACUUM INTO），確保 WAL 內的資料一併寫入。
func backupDB(dst string) (int64, error) {
	var size int64

	// VACUUM INTO 會產生完整且一致的快照，含所有已提交的交易。
	if _, err := db.DB.Exec(`VACUUM INTO ?`, dst); err != nil {
		// 某些 SQLite 版本/驅動不支援 VACUUM INTO，退回 checkpoint + 複製。
		db.MustExec("PRAGMA wal_checkpoint(TRUNCATE)")
		n, cerr := copyFile(filepath.Join(backupSrcPath(), "autorepair.db"), dst)
		if cerr != nil {
			return 0, fmt.Errorf("VACUUM INTO 失敗 (%v) 且複製備份也失敗: %v", err, cerr)
		}
		return n, nil
	}

	info, err := os.Stat(dst)
	if err != nil {
		return 0, fmt.Errorf("查詢備份檔大小失敗: %v", err)
	}
	size = info.Size()
	return size, nil
}

// backupSrcPath 從設定取得來源 DB 目錄，移除 file: 與 query string。
func backupSrcPath() string {
	raw := config.Load().DBPath
	raw = strings.TrimPrefix(raw, "file:")
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		raw = raw[:i]
	}
	if raw == "" {
		return "data"
	}
	return filepath.Dir(raw)
}

func copyFile(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, fmt.Errorf("開啟來源失敗: %v", err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return 0, fmt.Errorf("建立備份檔失敗: %v", err)
	}
	defer out.Close()

	n, err := io.Copy(out, in)
	if err != nil {
		return n, err
	}
	return n, nil
}

func dirStats(dir string) (count int, size int64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		count++
		if info, err := e.Info(); err == nil {
			size += info.Size()
		}
	}
	return
}

// --- 內建任務 ---

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
		Name:     "定期保養到期掃描",
		Interval: 6 * time.Hour,
		Run:      scanMaintenanceDue,
	})
}

// scanMaintenanceDue 找出 7 天內到期或已逾期的定期保養，寫入通知佇列。
func scanMaintenanceDue() error {
	_, err := scanMaintenanceDueInternal()
	return err
}

// ScanMaintenanceDueNow 供 UI 手動觸發，回傳新增筆數。
func ScanMaintenanceDueNow() (int, error) {
	return scanMaintenanceDueInternal()
}

func scanMaintenanceDueInternal() (int, error) {
	// 以台北當地日期為準（車行在台灣），不能用 SQLite 的 date('now')（UTC，會差 8 小時）。

	// next_maintenance_date 可能以兩種格式存在：
	//   - 純日期 "2026-10-04"
	//   - 驅動寫入的 RFC3339 "2026-10-04T00:00:00Z"
	// SQLite 的 date() 對後者回傳 NULL，因此在 SQL 內比較會靜默失效。
	// 這裡改成全部撈回來、在 Go 端用字串前 10 碼比較，行為可預期且不受驅動格式影響。
	rows, err := db.DB.Query(`SELECT v.id, v.customer_id, v.plate_number, v.next_maintenance_date,
		COALESCE(c.name,''), COALESCE(c.notification_preference,'line')
		FROM active_vehicles v
		JOIN active_customers c ON c.id = v.customer_id
		WHERE v.next_maintenance_date IS NOT NULL
		  AND TRIM(COALESCE(v.next_maintenance_date,'')) != ''`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	cutoff := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	today := time.Now().Format("2006-01-02")

	// 先把候選資料全部讀出來，再逐一處理。
	// 不能在 rows 迴圈內再對 db.DB 發查詢（去重檢查會另開連線），
	// 部分 SQLite 驅動會因此讓外層 rows 提前失效，導致只處理第一筆。
	type candidate struct {
		vehID, custID     int64
		plate, dueDate    string
		custName, notifPr string
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.vehID, &c.custID, &c.plate, &c.dueDate, &c.custName, &c.notifPr); err != nil {
			continue
		}

		// 取前 10 碼當作 YYYY-MM-DD；格式不符就跳過
		d := strings.TrimSpace(c.dueDate)
		if len(d) < 10 {
			continue
		}
		d = d[:10]
		if _, perr := time.Parse("2006-01-02", d); perr != nil {
			continue
		}
		// 只提醒今天（含逾期）到 7 天內到期
		if d < today || d > cutoff {
			continue
		}
		c.dueDate = d
		candidates = append(candidates, c)
	}
	rows.Close()

	queued := 0
	for _, c := range candidates {
		// 避免重複排程：同一台車在「最近一次排程週期」內只提醒一次。
		// 只看 status='pending' 會讓已送達的通知在下一輪掃描又被重新排入，
		// 造成客戶每 6 小時收到一次重複提醒。因此改為比對最近一次建立時間。
		var dup int
		if err := db.DB.QueryRow(`SELECT COUNT(1) FROM notifications
			WHERE kind='maintenance_due' AND datetime(created_at) >= datetime(?) AND body LIKE ?`,
			dedupSince(), "%"+c.plate+"%").Scan(&dup); err != nil {
			continue
		}
		if dup > 0 {
			continue
		}

		channel := c.notifPr
		if channel != "line" && channel != "sms" {
			channel = "internal"
		}

		msg := fmt.Sprintf("親愛的 %s 您好，您的車輛 %s 定期保養日期為 %s，即將到期，歡迎預約保養。",
			c.custName, c.plate, c.dueDate)

		if err := QueueNotification("maintenance_due", channel, "定期保養提醒", msg,
			c.custID, 0, time.Now()); err != nil {
			continue
		}
		queued++
	}

	if queued > 0 {
		log.Printf("定期保養到期掃描：新增 %d 筆提醒", queued)
	}
	return queued, nil
}

// scanWorkOrderReady 找出「已完成」但尚未「結帳」的工單，寫入完工通知佇列。
func scanWorkOrderReady() error {
	_, err := scanWorkOrderReadyInternal()
	return err
}

// ScanWorkOrderReadyNow 供 UI 手動觸發，回傳新增筆數。
func ScanWorkOrderReadyNow() (int, error) {
	return scanWorkOrderReadyInternal()
}

func scanWorkOrderReadyInternal() (int, error) {
	// 以台北當地時間為準，不能用 SQLite 的 now()（UTC，會差 8 小時）。
	now := time.Now()
	cutoff := now.Add(-1 * time.Hour)

	// 篩選條件改在 SQL 內用 datetime() 解析後比較，不做字串比對。
	//
	// work_orders.created_at 的 DEFAULT 是 CURRENT_TIMESTAMP，實際寫出來是
	// SQL datetime（"2026-10-05 06:55:07"）；而這裡若帶入 RFC3339
	// cutoff，因為 空格(0x20) < 'T'(0x54)，每筆 SQL datetime 都會被判為
	// 「小於」cutoff，條件恆不成立、掃描永遠找不到工單。
	// datetime() 兩邊都解析成時間點再比，才不受字面格式影響。
	rows, err := db.DB.Query(`SELECT wo.id, wo.customer_id, wo.total_amount,
		COALESCE(c.name,''), COALESCE(c.phone,''), COALESCE(c.notification_preference,'line'),
		wo.created_at
		FROM active_work_orders wo
		JOIN active_customers c ON c.id = wo.customer_id
		WHERE wo.status = 'completed'
		  AND datetime(wo.created_at) > datetime(?)`, cutoff.UTC().Format(sqlTimeLayout))
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	queued := 0
	for rows.Next() {
		var woID, custID int64
		var amount float64
		var custName, phone, pref string
		var createdAt time.Time
		if err := rows.Scan(&woID, &custID, &amount, &custName, &phone, &pref, &createdAt); err != nil {
			continue
		}

		// 避免重複排程：檢查是否已有未處理的同工單通知
		dup := 0
		db.DB.QueryRow(`SELECT COUNT(1) FROM notifications
			WHERE kind='work_order_ready' AND status='pending' AND body LIKE ?`, "%工單 #"+strconv.FormatInt(woID, 10)+"").Scan(&dup)
		if dup > 0 {
			continue
		}

		channel := pref
		if channel != "line" && channel != "sms" {
			channel = "internal"
		}

		msg := fmt.Sprintf("親愛的 %s 您好，您的工單 #%d 已完成，歡迎來取車並結帳。", custName, woID)

		if err := QueueNotification("work_order_ready", channel, "完工通知", msg, custID, woID, now); err != nil {
			continue
		}
		queued++
	}

	if queued > 0 {
		log.Printf("完工通知掃描：新增 %d 筆提醒", queued)
	}
	return queued, nil
}

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
