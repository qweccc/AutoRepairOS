package session

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSessionCookiesUseSecureAndLaxAttributes(t *testing.T) {
	tests := []struct {
		name       string
		request    *http.Request
		wantSecure bool
	}{
		{
			name:    "plain HTTP",
			request: httptest.NewRequest(http.MethodGet, "http://example.test/", nil),
		},
		{
			name: "TLS",
			request: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "https://example.test/", nil)
				r.TLS = &tls.ConnectionState{}
				return r
			}(),
			wantSecure: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			SetCookie(rec, tt.request, "session-token")

			cookies := rec.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("cookie count = %d, want 1", len(cookies))
			}
			cookie := cookies[0]
			if cookie.SameSite != http.SameSiteLaxMode {
				t.Errorf("SameSite = %v, want Lax", cookie.SameSite)
			}
			if cookie.Secure != tt.wantSecure {
				t.Errorf("Secure = %t, want %t", cookie.Secure, tt.wantSecure)
			}
			if !cookie.HttpOnly {
				t.Error("session cookie must be HttpOnly")
			}

			clearRec := httptest.NewRecorder()
			ClearCookie(clearRec, tt.request)
			clearCookies := clearRec.Result().Cookies()
			if len(clearCookies) != 1 || clearCookies[0].SameSite != http.SameSiteLaxMode {
				t.Errorf("clear cookie must use SameSite=Lax; got %+v", clearCookies)
			}
		})
	}
}
