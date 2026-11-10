package line

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"autorepairos/internal/db"

	_ "modernc.org/sqlite"
)

// setupTestDB 把 db.DB 換成 in-memory SQLite（含 active_customers），回傳還原函數。
func setupTestDB(t *testing.T) func() {
	t.Helper()
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	_, _ = d.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY, line_user_id TEXT)`)
	_, _ = d.Exec(`CREATE VIEW active_customers AS SELECT * FROM customers`)
	old := db.DB
	db.DB = d
	return func() {
		db.DB = old
		_ = d.Close()
	}
}

func resetShared(t *testing.T) {
	t.Helper()
	ResetSharedForTest()
	old := shared
	shared = NewLazyClient()
	t.Cleanup(func() {
		shared = old
		ResetSharedForTest()
	})
}

func TestDeliverNotificationCustomerNotFound(t *testing.T) {
	restore := setupTestDB(t)
	defer restore()
	resetShared(t)
	setLineEnv(t, "", "", "", "")
	if err := DeliverNotification(Notification{CustomerID: 999, Kind: "k", Subject: "s", Body: "b"}); err != nil {
		t.Errorf("客戶不存在應略過回 nil: %v", err)
	}
}

func TestDeliverNotificationDBError(t *testing.T) {
	restore := setupTestDB(t)
	defer restore()
	resetShared(t)
	db.DB.Close() // 之後所有查詢都失敗
	if err := DeliverNotification(Notification{CustomerID: 1}); err != nil {
		t.Errorf("DB 錯誤應略過回 nil: %v", err)
	}
}

func TestDeliverNotificationNoLineID(t *testing.T) {
	restore := setupTestDB(t)
	defer restore()
	resetShared(t)
	_, _ = db.DB.Exec(`INSERT INTO customers (id, line_user_id) VALUES (1, '')`)
	setLineEnv(t, "", "", "", "")
	if err := DeliverNotification(Notification{CustomerID: 1, Subject: "s", Body: "b"}); err != nil {
		t.Errorf("未綁定應略過回 nil: %v", err)
	}
}

func TestDeliverNotificationMockNotReady(t *testing.T) {
	restore := setupTestDB(t)
	defer restore()
	resetShared(t)
	_, _ = db.DB.Exec(`INSERT INTO customers (id, line_user_id) VALUES (1, 'U1')`)
	setLineEnv(t, "", "", "", "")
	if err := DeliverNotification(Notification{CustomerID: 1, Kind: "reminder", Subject: "s", Body: "b"}); err != nil {
		t.Errorf("Mock 未 ready 應回 nil: %v", err)
	}
}

func TestDeliverNotificationReady(t *testing.T) {
	restore := setupTestDB(t)
	defer restore()
	var gotTo, gotText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&v)
		gotTo, _ = v["to"].(string)
		msgs := v["messages"].([]interface{})
		gotText = msgs[0].(map[string]interface{})["text"].(string)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	resetShared(t)
	_, _ = db.DB.Exec(`INSERT INTO customers (id, line_user_id) VALUES (1, 'U1')`)
	setLineEnv(t, "id", "sec", "tok", srv.URL)
	for _, kind := range []string{"estimate_approval", "work_order_ready", "reminder"} {
		if err := DeliverNotification(Notification{CustomerID: 1, Kind: kind, Subject: "標題", Body: "內容"}); err != nil {
			t.Fatalf("kind=%s: %v", kind, err)
		}
		if gotTo != "U1" || !strings.Contains(gotText, "標題") || !strings.Contains(gotText, "內容") {
			t.Errorf("kind=%s: to=%q text=%q", kind, gotTo, gotText)
		}
	}
}

func TestLazyClient(t *testing.T) {
	// Mock 模式：ready=false，pushText 走 mock push
	resetShared(t)
	setLineEnv(t, "", "", "", "")
	l := NewLazyClient()
	cfg, cli := l.load()
	if cfg == nil || cli == nil {
		t.Fatal("load 應回非 nil")
	}
	if l.ready() {
		t.Error("無憑證 ready 應為 false")
	}
	// load 第二次應回快取（同一 cli）
	if _, cli2 := l.load(); cli2 != cli {
		t.Error("load 應回快取")
	}
	if err := l.pushText("U1", "標題", "內容"); err != nil {
		t.Errorf("pushText mock 應為 nil: %v", err)
	}
	if err := l.pushText("U1", "", "只有內容"); err != nil {
		t.Errorf("pushText 無標題應為 nil: %v", err)
	}

	// Ready 模式：pushText 真送
	resetShared(t)
	var gotText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&v)
		msgs := v["messages"].([]interface{})
		gotText = msgs[0].(map[string]interface{})["text"].(string)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	setLineEnv(t, "id", "sec", "tok", srv.URL)
	l2 := NewLazyClient()
	if !l2.ready() {
		t.Fatal("有憑證 ready 應為 true")
	}
	if err := l2.pushText("U1", "標題", "內容"); err != nil {
		t.Fatal(err)
	}
	if gotText != "標題\n內容" {
		t.Errorf("gotText = %q", gotText)
	}
}
