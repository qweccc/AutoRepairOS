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

func setupCSRFTestSession(t *testing.T) (string, string) {
	t.Helper()
	d, err := openTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	db.DB = d
	if _, err := d.Exec(`CREATE TABLE sessions (
		id TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		csrf_token TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}

	sessionToken, err := session.Start(1)
	if err != nil {
		t.Fatal(err)
	}
	csrfToken, err := session.CSRFToken(sessionToken)
	if err != nil {
		t.Fatal(err)
	}
	return sessionToken, csrfToken
}

func TestCSRFProtection(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		token      string
		formToken  string
		wantStatus int
	}{
		{name: "missing token is rejected", method: http.MethodPost, wantStatus: http.StatusForbidden},
		{name: "invalid token is rejected", method: http.MethodPost, token: "wrong", wantStatus: http.StatusForbidden},
		{name: "valid header token is accepted", method: http.MethodPost, token: "valid", wantStatus: http.StatusNoContent},
		{name: "valid form token is accepted", method: http.MethodPost, formToken: "valid", wantStatus: http.StatusNoContent},
		{name: "valid DELETE header token is accepted", method: http.MethodDelete, token: "valid", wantStatus: http.StatusNoContent},
		{name: "GET does not require a token", method: http.MethodGet, wantStatus: http.StatusNoContent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessionToken, csrfToken := setupCSRFTestSession(t)
			if tt.token == "valid" {
				tt.token = csrfToken
			}
			var called bool
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if got := session.CSRFTokenFromContext(r.Context()); got != csrfToken {
					t.Errorf("context CSRF token = %q, want %q", got, csrfToken)
				}
				w.WriteHeader(http.StatusNoContent)
			})

			var req *http.Request
			if tt.formToken != "" {
				req = httptest.NewRequest(tt.method, "/resource",
					strings.NewReader(url.Values{"_csrf": {csrfToken}}.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			} else {
				req = httptest.NewRequest(tt.method, "/resource", nil)
			}
			req.AddCookie(&http.Cookie{Name: "session_token", Value: sessionToken})
			if tt.token != "" && tt.formToken == "" {
				req.Header.Set(csrfHeader, tt.token)
			}

			rec := httptest.NewRecorder()
			CSRFProtection(next).ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if called != (tt.wantStatus == http.StatusNoContent) {
				t.Errorf("next handler called = %t, want %t", called, tt.wantStatus == http.StatusNoContent)
			}
		})
	}
}

// TestCSRFExemptions 驗證免 CSRF 的路徑，並確認免責沒有外溢到其他路徑。
//
// 這支測試對應一個真實故障：車主端的 /approval/approve/{token} 是公開端點
// （核准頁不建立 session、不發 cookie），而 CSRF 檢查是綁 session 的——沒有
// session 就永遠拿不到可比對的 token，所以車主點「同意」一律 403。整個
// 客戶端授權流程等於完全不能用。
func TestCSRFExemptions(t *testing.T) {
	// 車主端：沒有 cookie、沒有 token，POST 仍必須通過
	for _, p := range []string{
		"/approval/approve/abc123",
		"/approval/reject/abc123",
		"/login",
		"/webhook/line",
	} {
		called := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusNoContent)
		})
		req := httptest.NewRequest(http.MethodPost, p, nil) // 刻意不帶 cookie 與 token
		rec := httptest.NewRecorder()
		CSRFProtection(next).ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent || !called {
			t.Errorf("%s：無 session 的 POST 應直接通過（修復前一律 403），得到 %d", p, rec.Code)
		}
	}

	// 免責不可外溢：這些路徑沒有 token 仍必須被擋
	for _, p := range []string{
		"/work-orders/1/items",
		"/customers/1/line-bind",
		"/approval/123", // 核准頁本身是 GET，但 POST 到它不該被放行
		"/approval",
		"/approvalX/approve/abc", // 前綴不可誤判
		"/approval/approve",      // 沒有 token 的裸前綴不該放行
		"/loginx",
	} {
		called := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		})
		req := httptest.NewRequest(http.MethodPost, p, nil)
		rec := httptest.NewRecorder()
		CSRFProtection(next).ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden || called {
			t.Errorf("%s：沒有 token 仍應被擋（403），得到 %d（called=%t）", p, rec.Code, called)
		}
	}
}

func TestCSRFPageHelpers(t *testing.T) {
	const token = "random-token"
	form := withCSRFFormFields(`<form method="POST" action="/change"><button>Save</button></form>`, token)
	if !strings.Contains(form, `<input type="hidden" name="_csrf" value="random-token">`) {
		t.Fatalf("CSRF input missing from form: %s", form)
	}

	link := withCSRFHTMXActions(`<a class="delete" href="/items/12/delete">Delete</a>`)
	if !strings.Contains(link, `hx-post="/items/12/delete"`) {
		t.Fatalf("state-changing link was not converted to HTMX POST: %s", link)
	}
}

func TestCsrfExempt(t *testing.T) {
	if !csrfExempt("/login") {
		t.Error("/login should be exempt")
	}
	if !csrfExempt("/webhook/line") {
		t.Error("/webhook/line should be exempt")
	}
	if !csrfExempt("/approval/approve/abc") {
		t.Error("/approval/approve/ should be exempt")
	}
	if !csrfExempt("/approval/reject/abc") {
		t.Error("/approval/reject/ should be exempt")
	}
	if csrfExempt("/dashboard") {
		t.Error("/dashboard should not be exempt")
	}
}
