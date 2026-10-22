package handler

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"autorepairos/internal/db"
	_ "modernc.org/sqlite"
)

// setupWorklogAssignmentDB 建立 A4 測試所需的最小 schema：users、work_orders
// （含 active_work_orders 視圖）、technician_worklogs。
func setupWorklogAssignmentDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := openTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	// openTestDB 只回傳 DB 卻沒有裝進全域 db.DB（handler 直接吃全域 db.DB），
	// 這裡自行裝入，用完由 openTestDB 註冊的 cleanup 還原。
	db.DB = d
	for _, s := range []string{
		`CREATE TABLE users(id INTEGER PRIMARY KEY, name TEXT, role TEXT)`,
		`CREATE TABLE work_orders(id INTEGER PRIMARY KEY, status TEXT, technician_id INTEGER, deleted_at TEXT)`,
		`CREATE VIEW active_work_orders AS SELECT * FROM work_orders WHERE deleted_at IS NULL`,
		`CREATE TABLE technician_worklogs(id INTEGER PRIMARY KEY AUTOINCREMENT, work_order_id INTEGER NOT NULL, technician_id INTEGER NOT NULL, work_date TEXT NOT NULL, hours REAL DEFAULT 0, hourly_rate REAL DEFAULT 0, labor_amount REAL DEFAULT 0, commission_rate REAL DEFAULT 0, commission_amount REAL DEFAULT 0, notes TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`,
	} {
		if _, err := d.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.Exec(`INSERT INTO users(id,name,role) VALUES (5,'技師甲','technician'),(9,'技師乙','technician')`); err != nil {
		t.Fatal(err)
	}
	// 工單 1 派給技師 5；工單 2 派給技師 9。
	if _, err := d.Exec(`INSERT INTO work_orders(id,status,technician_id) VALUES (1,'in_progress',5),(2,'in_progress',9)`); err != nil {
		t.Fatal(err)
	}
	return d
}

func countWorklogs(t *testing.T, d *sql.DB) int {
	t.Helper()
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM technician_worklogs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func postWorklog(d *sql.DB, woID string, actor Actor) int {
	form := url.Values{}
	form.Set("work_date", time.Now().Format("2006-01-02"))
	form.Set("hours", "2")
	form.Set("hourly_rate", "500")
	form.Set("commission_rate", "0")
	form.Set("technician_id", "999") // 技師身分時應被忽略，只能記到自己
	req := httptest.NewRequest(http.MethodPost, "/work-orders/"+woID+"/worklogs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", woID)
	rec := httptest.NewRecorder()
	AddTechnicianWorklog(rec, req, actor)
	return rec.Code
}

// TestTechnicianWorklogRequiresAssignment 守門 A4：技師只能對「自己被派工」的工單
// 登錄工時，避免對未參與的工單灌水佣金。紅燈驗證：移除 worklogs.go 的派工檢查後
// 本測試應對「未派工」case 失敗（會插入一筆）。
func TestTechnicianWorklogRequiresAssignment(t *testing.T) {
	tech := Actor{ID: 5, Role: "technician"}

	// 派工給技師 5 的工單：應成功，且記到的是 actor(5) 而非表單送來的 999。
	d := setupWorklogAssignmentDB(t)
	if code := postWorklog(d, "1", tech); code == http.StatusBadRequest {
		t.Fatalf("assigned WO rejected: status=%d", code)
	}
	if n := countWorklogs(t, d); n != 1 {
		t.Fatalf("assigned WO: queued %d worklogs, want 1", n)
	}
	var techID int
	if err := d.QueryRow(`SELECT technician_id FROM technician_worklogs`).Scan(&techID); err != nil {
		t.Fatal(err)
	}
	if techID != 5 {
		t.Fatalf("technician_id=%d, want 5 (actor identity, not form value 999)", techID)
	}

	// 未派工給技師 5 的工單（technician_id=9）：應被擋下，不插入。
	d2 := setupWorklogAssignmentDB(t)
	if code := postWorklog(d2, "2", tech); code != http.StatusBadRequest {
		t.Fatalf("unassigned WO: status=%d, want 400", code)
	}
	if n := countWorklogs(t, d2); n != 0 {
		t.Fatalf("unassigned WO: queued %d worklogs, want 0", n)
	}
}
