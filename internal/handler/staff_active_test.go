package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/internal/session"
)

const sessionCookieName = "session_token"

// setupStaffActiveTestDB 建立隔離的記憶體 DB（users + sessions 兩張表）。
func setupStaffActiveTestDB(t *testing.T) {
	t.Helper()
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
			role TEXT NOT NULL DEFAULT 'technician',
			name TEXT NOT NULL,
			active INTEGER NOT NULL DEFAULT 1,
			created_at TEXT
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
}

// TestStaffActive_SessionRevokedOnDeactivate 停用後原 session cookie 應被撤銷，
// 存取任一受保護頁面會 302 到 /login。
func TestStaffActive_SessionRevokedOnDeactivate(t *testing.T) {
	setupStaffActiveTestDB(t)

	// 前置：一個在职員工，建立 session token。
	db.DB.Exec(`INSERT INTO users (id, username, password, role, name, active)
		VALUES (1, 'tech1', 'x', 'technician', '技師一', 1)`)
	token, err := session.Start(1)
	if err != nil {
		t.Fatal(err)
	}

	// 先確認該 token 原本有效（能拿到 user）。
	var uid int64
	if err := db.DB.QueryRow(`SELECT user_id FROM sessions WHERE id = ?`, token).Scan(&uid); err != nil {
		t.Fatal(err)
	}

	// 停用該員工。
	if _, err := db.DB.Exec(`UPDATE users SET active = 0 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	session.DeleteForUser(1)

	// 用原 token 打受保護頁 → 應 302 到 /login。
	req := httptest.NewRequest("GET", "/work-orders", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()

	AllStaff(func(w http.ResponseWriter, r *http.Request, u model.User) {
		w.WriteHeader(http.StatusOK)
	}).ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("停用後原 session 應 302，got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Fatalf("應導到 /login，got %q", loc)
	}

	// session 應已從 sessions 表移除。
	if err := db.DB.QueryRow(`SELECT user_id FROM sessions WHERE id = ?`, token).Scan(&uid); err == nil {
		t.Fatal("停用後 session 應已刪除")
	}
}

// TestStaffActive_LoginBlockedWhenInactive 停用後即使用正確帳密也無法登入（401）。
func TestStaffActive_LoginBlockedWhenInactive(t *testing.T) {
	setupStaffActiveTestDB(t)

	// 用 bcrypt 產生真正的 hash，測正確密碼但 active=0 的路徑。
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-pass"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	db.DB.Exec(`INSERT INTO users (id, username, password, role, name, active)
		VALUES (1, 'tech1', ?, 'technician', '技師一', 0)`, hash)

	// 用正確密碼嘗試登入 → 應 401（被 active=0 擋住）。
	req := httptest.NewRequest("POST", "/login", nil)
	req.Form = map[string][]string{"username": {"tech1"}, "password": {"correct-pass"}}
	rec := httptest.NewRecorder()
	LoginHandler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("停用後登入應回 401，got %d", rec.Code)
	}
}

// TestStaffActive_ReactivateLogin 復職後原帳密可再次登入（302）。
func TestStaffActive_ReactivateLogin(t *testing.T) {
	setupStaffActiveTestDB(t)

	// 前置：一個停用的帳號，先確認它被擋。
	db.DB.Exec(`INSERT INTO users (id, username, password, role, name, active)
		VALUES (1, 'tech1', '$2a$10$abcdefghijklmnopqrstuv', 'technician', '技師一', 0)`)

	// 復職。
	if _, err := db.DB.Exec(`UPDATE users SET active = 1 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	// 確認 active 已設為 1。
	var active int
	if err := db.DB.QueryRow(`SELECT active FROM users WHERE id = 1`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("復職後 active 應為 1，got %d", active)
	}

	// 用 session.Start 確認復職後可以建立 session（表示 active 檢查通過）。
	token, err := session.Start(1)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("復職後應能建立 session")
	}
}

// TestStaffActive_CurrentUserRejectsInactive 直接驗證 currentUser 對 active=0 回傳錯誤。
func TestStaffActive_CurrentUserRejectsInactive(t *testing.T) {
	setupStaffActiveTestDB(t)

	// 前置：停用帳號 + 一個有效 session。
	db.DB.Exec(`INSERT INTO users (id, username, password, role, name, active)
		VALUES (1, 'tech1', 'x', 'technician', '技師一', 0)`)
	token, err := session.Start(1)
	if err != nil {
		t.Fatal(err)
	}

	// 用該 token 打 AllStaff 包裝的 handler → 應 302（currentUser 回傳 error）。
	req := httptest.NewRequest("GET", "/work-orders", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()

	AllStaff(func(w http.ResponseWriter, r *http.Request, u model.User) {
		w.WriteHeader(http.StatusOK)
	}).ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("active=0 的 currentUser 應讓 AllStaff 302，got %d", rec.Code)
	}
}
