package handler

import (
	"bytes"
	"database/sql"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"autorepairos/internal/db"
	"autorepairos/internal/session"
)

var csrfInputRe = regexp.MustCompile(`name="_csrf"\s+value="([^"]+)"`)

// newMigratedTestDB 使用真實 migration 建立暫存 SQLite DB，並替換全域 db.DB。
// 測試結束會自動還原與關閉。
func newMigratedTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := openMigratedTestDB(t)
	if err != nil {
		t.Fatalf("newMigratedTestDB: %v", err)
	}
	return d
}

// testHTTPClient 帶 cookie jar 的 http.Client，模擬瀏覽器行為。
func newTestHTTPClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Jar:     jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// extractCSRF 從 HTML 取出 _csrf token。
func extractCSRF(html string) string {
	m := csrfInputRe.FindStringSubmatch(html)
	if len(m) != 2 {
		return ""
	}
	return m[1]
}

// postForm 帶 CSRF 的 POST 表單 helper。
func postFormWithCSRF(t *testing.T, client *http.Client, srv *httptest.Server, path string, values url.Values, csrf string) *http.Response {
	t.Helper()
	if csrf != "" {
		values.Set("_csrf", csrf)
	}
	req, err := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(values.Encode()))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("post form: %v", err)
	}
	return resp
}

// getWithClient 用既有 client（帶 cookie）發 GET。
func getWithClient(t *testing.T, client *http.Client, srv *httptest.Server, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	return resp
}

// loginWithClient 以指定帳密登入，回傳帶 session cookie 的 client 與 CSRF token。
func loginWithClient(t *testing.T, srv *httptest.Server, username, password string) (*http.Client, string) {
	t.Helper()
	client := newTestHTTPClient()
	resp := getWithClient(t, client, srv, "/login")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get /login: %d", resp.StatusCode)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read login body: %v", err)
	}
	resp.Body.Close()
	csrf := extractCSRF(buf.String())

	form := url.Values{}
	form.Set("username", username)
	form.Set("password", password)
	resp2 := postFormWithCSRF(t, client, srv, "/login", form, csrf)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusFound && resp2.StatusCode != http.StatusSeeOther {
		t.Fatalf("post /login: %d", resp2.StatusCode)
	}
	// 取得首頁或設定頁的 CSRF 供後續 POST 用
	resp3 := getWithClient(t, client, srv, "/")
	var buf3 bytes.Buffer
	if _, err := buf3.ReadFrom(resp3.Body); err == nil {
		if c := extractCSRF(buf3.String()); c != "" {
			csrf = c
		}
	}
	resp3.Body.Close()
	return client, csrf
}

// startTestSession 直接用 session.Start 建立測試 session（不走登入頁）。
func startTestSession(t *testing.T, userID int64) (string, string) {
	t.Helper()
	token, err := session.Start(userID)
	if err != nil {
		t.Fatalf("session.Start: %v", err)
	}
	csrf, err := session.CSRFToken(token)
	if err != nil {
		t.Fatalf("session.CSRFToken: %v", err)
	}
	return token, csrf
}

// sqlDB 為了與現有 openTestDB 相容的型別別名（實際用 *sql.DB）。
type sqlDB = *sql.DB

// openMigratedTestDB 用 migration 建立獨立測試 DB。
func openMigratedTestDB(t *testing.T) (*sql.DB, error) {
	t.Helper()
	// 直接重用 testhelper 的模式：記憶體 + 單一連線 + migration
	orig := db.DB
	t.Cleanup(func() { db.DB = orig })

	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = d.Close() })
	db.DB = d
	if err := db.Migrate(); err != nil {
		return nil, err
	}
	return d, nil
}

// 確保 import 存在
var (
	_ = bytes.Buffer{}
	_ = errors.New
	_ = url.Values{}
	_ = strings.TrimSpace
)
