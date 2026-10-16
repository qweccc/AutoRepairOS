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
