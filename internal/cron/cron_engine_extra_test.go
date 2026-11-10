package cron

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"autorepairos/internal/db"
	_ "modernc.org/sqlite"
)

func engineTestDB(t *testing.T) *sql.DB {
	t.Helper()
	original := db.DB
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	db.DB = d
	if _, err := d.Exec(`CREATE TABLE scheduled_tasks (
		task_key TEXT PRIMARY KEY, name TEXT,
		interval_minutes INTEGER, enabled INTEGER DEFAULT 1,
		last_run_at TEXT, next_run_at TEXT, run_count INTEGER DEFAULT 0,
		last_error TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.DB = original
		_ = d.Close()
	})
	return d
}

func snapshotRegistry(t *testing.T) {
	t.Helper()
	orig := make(map[string]Task, len(registry))
	for k, v := range registry {
		orig[k] = v
	}
	t.Cleanup(func() {
		for k := range registry {
			delete(registry, k)
		}
		for k, v := range orig {
			registry[k] = v
		}
	})
}

func TestBoolToInt(t *testing.T) {
	if boolToInt(true) != 1 || boolToInt(false) != 0 {
		t.Fatal("boolToInt mapping wrong")
	}
}

func TestFormatInterval(t *testing.T) {
	for mins, want := range map[int]string{
		0:    "停用",
		-5:   "停用",
		1:    "1 分鐘",
		30:   "30 分鐘",
		59:   "59 分鐘",
		60:   "1 小時",
		90:   "1 小時",
		1440: "1 天",
		2880: "2 天",
	} {
		if got := formatInterval(mins); got != want {
			t.Errorf("formatInterval(%d)=%q want %q", mins, got, want)
		}
	}
}

func TestParseDBTime(t *testing.T) {
	if _, err := parseDBTime("2026-01-02T15:04:05Z"); err != nil {
		t.Errorf("RFC3339: %v", err)
	}
	if _, err := parseDBTime(" 2026-01-02 15:04:05 "); err != nil {
		t.Errorf("sql datetime with spaces: %v", err)
	}
	if _, err := parseDBTime("not-a-time"); err == nil {
		t.Error("invalid input should error")
	}
	// nowDBTime must round-trip through parseDBTime
	if _, err := parseDBTime(nowDBTime()); err != nil {
		t.Errorf("nowDBTime round-trip: %v", err)
	}
}

func TestNewSyncsTasks(t *testing.T) {
	d := engineTestDB(t)
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if s == nil || s.stop == nil {
		t.Fatal("New returned nil scheduler")
	}
	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM scheduled_tasks`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != len(registry) {
		t.Fatalf("synced %d tasks, registry has %d", count, len(registry))
	}
	// syncTask with zero interval: interval_minutes NULL, next_run_at NULL
	if err := syncTask(Task{Key: "zero", Name: "Zero", Interval: 0}); err != nil {
		t.Fatal(err)
	}
	var mins, next sql.NullInt64
	if err := d.QueryRow(`SELECT interval_minutes, next_run_at FROM scheduled_tasks WHERE task_key='zero'`).
		Scan(&mins, &next); err != nil {
		t.Fatal(err)
	}
	if mins.Valid || next.Valid {
		t.Fatalf("zero interval should store NULLs, got mins=%v next=%v", mins, next)
	}
	// Re-sync an existing (disabled) task with interval: enabled flips back to 1.
	if _, err := d.Exec(`UPDATE scheduled_tasks SET enabled=0 WHERE task_key='zero'`); err != nil {
		t.Fatal(err)
	}
	if err := syncTask(Task{Key: "zero", Name: "Zero2", Interval: 10 * time.Minute}); err != nil {
		t.Fatal(err)
	}
	var en int
	if err := d.QueryRow(`SELECT enabled FROM scheduled_tasks WHERE task_key='zero'`).Scan(&en); err != nil {
		t.Fatal(err)
	}
	if en != 1 {
		t.Fatal("re-sync with interval should re-enable task")
	}
}

func TestNewErrorWhenTableMissing(t *testing.T) {
	d := engineTestDB(t)
	if _, err := d.Exec(`DROP TABLE scheduled_tasks`); err != nil {
		t.Fatal(err)
	}
	if s, err := New(); err == nil || s != nil {
		t.Fatalf("New with missing table: s=%v err=%v; want nil/error", s, err)
	}
}

func TestIsEnabledAndIsDue(t *testing.T) {
	d := engineTestDB(t)
	// Missing row: not enabled, but due (uninitialized).
	if isEnabled("nope") {
		t.Error("missing row should not be enabled")
	}
	if !isDue("nope") {
		t.Error("missing row should be due")
	}
	past := dbTime(time.Now().Add(-time.Hour))
	future := dbTime(time.Now().Add(time.Hour))
	for _, tc := range []struct {
		key     string
		en      int
		next    interface{}
		wantEn  bool
		wantDue bool
	}{
		{"a", 1, past, true, true},
		{"b", 1, future, true, false},
		{"c", 0, past, false, true},
		{"d", 1, nil, true, true},
		{"e", 1, "  ", true, true},
		{"f", 1, "garbage", true, true},
		{"g", 1, "2020-01-01 00:00:00", true, true}, // old SQL format
	} {
		if _, err := d.Exec(`INSERT INTO scheduled_tasks(task_key,enabled,next_run_at) VALUES(?,?,?)`,
			tc.key, tc.en, tc.next); err != nil {
			t.Fatal(err)
		}
		if got := isEnabled(tc.key); got != tc.wantEn {
			t.Errorf("%s isEnabled=%v want %v", tc.key, got, tc.wantEn)
		}
		if got := isDue(tc.key); got != tc.wantDue {
			t.Errorf("%s isDue=%v want %v", tc.key, got, tc.wantDue)
		}
	}
}

func TestRunTask(t *testing.T) {
	d := engineTestDB(t)
	// Success: Run returns nil, row updated with next_run and run_count.
	if _, err := d.Exec(`INSERT INTO scheduled_tasks(task_key,enabled) VALUES('t1',1)`); err != nil {
		t.Fatal(err)
	}
	if err := runTask(Task{Key: "t1", Name: "T1", Interval: time.Minute, Run: func() error { return nil }}); err != nil {
		t.Fatal(err)
	}
	var runCount int
	var next sql.NullString
	if err := d.QueryRow(`SELECT run_count, next_run_at FROM scheduled_tasks WHERE task_key='t1'`).
		Scan(&runCount, &next); err != nil {
		t.Fatal(err)
	}
	if runCount != 1 || !next.Valid || next.String == "" {
		t.Fatalf("runCount=%d next=%v", runCount, next)
	}
	// Error return: recorded in last_error, still counts as run.
	if err := runTask(Task{Key: "t1", Name: "T1", Interval: time.Minute,
		Run: func() error { return errors.New("boom") }}); err != nil {
		t.Fatal(err)
	}
	var lastErr string
	if err := d.QueryRow(`SELECT last_error FROM scheduled_tasks WHERE task_key='t1'`).Scan(&lastErr); err != nil {
		t.Fatal(err)
	}
	if lastErr != "boom" {
		t.Fatalf("last_error=%q", lastErr)
	}
	// Zero interval: next_run_at stays NULL (cleared via SQL NULL binding? value is nil interface).
	if err := runTask(Task{Key: "t1", Name: "T1", Interval: 0, Run: func() error { return nil }}); err != nil {
		t.Fatal(err)
	}
	// Panic: recovered, returns nil, does not crash.
	if err := runTask(Task{Key: "t1", Name: "T1", Interval: time.Minute,
		Run: func() error { panic("kaboom") }}); err != nil {
		t.Fatalf("panic should be recovered, got %v", err)
	}
	// Update failure (table dropped): returns wrapped error.
	if _, err := d.Exec(`DROP TABLE scheduled_tasks`); err != nil {
		t.Fatal(err)
	}
	if err := runTask(Task{Key: "t1", Name: "T1", Interval: time.Minute,
		Run: func() error { return nil }}); err == nil {
		t.Fatal("expected update error")
	}
}

func TestTickSkipsAndRuns(t *testing.T) {
	d := engineTestDB(t)
	snapshotRegistry(t)
	for k := range registry {
		delete(registry, k)
	}
	ran := map[string]int{}
	mk := func(key string, interval time.Duration) Task {
		return Task{Key: key, Name: key, Interval: interval, Run: func() error {
			ran[key]++
			return nil
		}}
	}
	Register(mk("skip-disabled", time.Minute))
	Register(mk("skip-notdue", time.Minute))
	Register(mk("skip-zero", 0))
	Register(mk("run-me", time.Minute))
	Register(mk("run-err", time.Minute))
	// registry["run-err"] variant returns error to cover tick error log
	registry["run-err"] = Task{Key: "run-err", Name: "run-err", Interval: time.Minute,
		Run: func() error { ran["run-err"]++; return errors.New("fail") }}
	past := dbTime(time.Now().Add(-time.Hour))
	future := dbTime(time.Now().Add(time.Hour))
	for _, row := range []struct {
		key string
		en  int
		nr  string
	}{
		{"skip-disabled", 0, past},
		{"skip-notdue", 1, future},
		{"skip-zero", 1, past},
		{"run-me", 1, past},
		{"run-err", 1, past},
	} {
		if _, err := d.Exec(`INSERT INTO scheduled_tasks(task_key,enabled,next_run_at) VALUES(?,?,?)`,
			row.key, row.en, row.nr); err != nil {
			t.Fatal(err)
		}
	}
	s := &Scheduler{stop: make(chan struct{})}
	s.tick()
	if ran["run-me"] != 1 || ran["run-err"] != 1 {
		t.Fatalf("ran=%v", ran)
	}
	for _, k := range []string{"skip-disabled", "skip-notdue", "skip-zero"} {
		if ran[k] != 0 {
			t.Fatalf("%s should not run, ran=%v", k, ran)
		}
	}
}

func TestSchedulerTickerFires(t *testing.T) {
	// The ticker fires every minute; waiting for one tick covers the
	// ticker case in Start's loop (tickWG.Add/tick/Done). Tasks are
	// disabled so the tick itself is a no-op.
	d := engineTestDB(t)
	if _, err := New(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE scheduled_tasks SET enabled=0`); err != nil {
		t.Fatal(err)
	}
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	s.Start()
	time.Sleep(65 * time.Second)
	s.Stop()
}

func TestSchedulerStartStop(t *testing.T) {
	d := engineTestDB(t)
	// Keep all builtin tasks disabled so the immediate tick is a no-op for them.
	if _, err := New(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE scheduled_tasks SET enabled=0`); err != nil {
		t.Fatal(err)
	}
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	s.Start()
	// Let the goroutine's initial tick finish before Stop; Stop's WaitGroup
	// does not cover a tick whose Add has not run yet.
	time.Sleep(100 * time.Millisecond)
	s.Stop()
	s.Stop() // second call is a no-op via sync.Once
}

func TestTasksSorted(t *testing.T) {
	snapshotRegistry(t)
	for k := range registry {
		delete(registry, k)
	}
	Register(Task{Key: "b", Name: "B"})
	Register(Task{Key: "a", Name: "A"})
	got := Tasks()
	if len(got) != 2 || got[0].Key != "a" || got[1].Key != "b" {
		t.Fatalf("Tasks not sorted: %+v", got)
	}
}

func TestStatus(t *testing.T) {
	d := engineTestDB(t)
	if got := Status(); len(got) != 0 {
		t.Fatalf("Status on empty table should be empty, got %v", got)
	}
	rowsSeed := []struct {
		key  string
		name string
		mins int
		en   int
	}{
		{"k0", "Zero", 0, 0},
		{"k1", "Minutes", 30, 1},
		{"k2", "Hours", 60, 1},
		{"k3", "Days", 1440, 1},
		{"k4", "Days2", 2880, 1},
	}
	for _, r := range rowsSeed {
		if _, err := d.Exec(`INSERT INTO scheduled_tasks(task_key,name,interval_minutes,enabled,last_error) VALUES(?,?,?,?,?)`,
			r.key, r.name, r.mins, r.en, "x"); err != nil {
			t.Fatal(err)
		}
	}
	got := Status()
	if len(got) != len(rowsSeed) {
		t.Fatalf("Status returned %d rows", len(got))
	}
	labels := map[string]string{}
	for _, s := range got {
		labels[s.Key] = s.IntervalLabel
		if s.LastError != "x" {
			t.Errorf("%s LastError=%q", s.Key, s.LastError)
		}
	}
	want := map[string]string{"k0": "停用", "k1": "30 分鐘", "k2": "1 小時", "k3": "1 天", "k4": "2 天"}
	for k, v := range want {
		if labels[k] != v {
			t.Errorf("label %s=%q want %q", k, labels[k], v)
		}
	}
	if !strings.Contains(labels["k1"], "分鐘") {
		t.Fatal("label sanity")
	}
	// Query error → nil.
	if _, err := d.Exec(`DROP TABLE scheduled_tasks`); err != nil {
		t.Fatal(err)
	}
	if got := Status(); got != nil {
		t.Fatalf("Status with missing table should be nil, got %v", got)
	}
}
