package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/session"
)

func TestUpdateStaff_InsuranceHistory(t *testing.T) {
	d, err := openTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	db.DB = d
	stmts := []string{
		`CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'owner',
			name TEXT NOT NULL,
			active INTEGER NOT NULL DEFAULT 1,
			staff_kind TEXT DEFAULT 'employee',
			staff_branch TEXT DEFAULT '',
			national_id TEXT DEFAULT '',
			birthday TEXT DEFAULT '',
			insurance_start TEXT DEFAULT '',
			insurance_end TEXT DEFAULT '',
			created_at TEXT
		)`,
		`CREATE TABLE insurance_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			event_type TEXT NOT NULL,
			event_date TEXT NOT NULL,
			note TEXT DEFAULT '',
			created_by INTEGER,
			created_at TEXT DEFAULT (datetime('now','localtime'))
		)`,
		`CREATE TABLE sessions (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			csrf_token TEXT NOT NULL DEFAULT '',
			created_at TEXT
		)`,
	}
	for _, s := range stmts {
		if _, err := db.DB.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	db.DB.Exec(`INSERT INTO users (id, username, password, role, name, active) VALUES (1,'owner','x','owner','Owner',1)`)
	db.DB.Exec(`INSERT INTO users (id, username, password, role, name, active, insurance_start, insurance_end) VALUES (2,'tech','x','technician','Tech',1,'','')`)
	token, _ := session.Start(1)

	form := url.Values{}
	form.Set("staff_kind", "employee")
	form.Set("staff_branch", "")
	form.Set("active", "1")
	form.Set("insurance_start", "2026-01-01")
	req := httptest.NewRequest(http.MethodPost, "/staff/2", strings.NewReader(form.Encode()))
	req.SetPathValue("id", "2")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rr := httptest.NewRecorder()
	UpdateStaff(rr, req)
	if rr.Code != http.StatusFound && rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	var n int
	db.DB.QueryRow(`SELECT COUNT(*) FROM insurance_history WHERE user_id=2`).Scan(&n)
	if n != 1 {
		t.Fatalf("expected 1 history row, got %d", n)
	}

	// no-op edit should not add history
	form2 := url.Values{}
	form2.Set("staff_kind", "employee")
	form2.Set("staff_branch", "")
	form2.Set("active", "1")
	form2.Set("insurance_start", "2026-01-01")
	req2 := httptest.NewRequest(http.MethodPost, "/staff/2", strings.NewReader(form2.Encode()))
	req2.SetPathValue("id", "2")
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rr2 := httptest.NewRecorder()
	UpdateStaff(rr2, req2)
	db.DB.QueryRow(`SELECT COUNT(*) FROM insurance_history WHERE user_id=2`).Scan(&n)
	if n != 1 {
		t.Fatalf("expected still 1 history row on no-op, got %d", n)
	}
}
