package cron

import (
	"database/sql"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"autorepairos/internal/db"
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
