package handler

import (
	"database/sql"
	"errors"
	"html"
	"log"
	"net/http"
	"regexp"
	"strings"

	"autorepairos/internal/session"
)

const csrfHeader = "X-CSRF-Token"

var csrfFormPattern = regexp.MustCompile(`(?i)<form\b([^>]*)>`)
var csrfLinkPattern = regexp.MustCompile(`(?i)<a\b([^>]*?)\bhref="([^"]+/(?:delete|deactivate|reactivate|void|cancel|toggle|run|receive))"([^>]*)>`)

type csrfResponseWriter struct {
	http.ResponseWriter
	token string
}

func (w *csrfResponseWriter) csrfToken() string {
	return w.token
}

func csrfTokenForResponse(w http.ResponseWriter) string {
	if csrfWriter, ok := w.(*csrfResponseWriter); ok {
		return csrfWriter.csrfToken()
	}
	return ""
}

func withCSRFFormFields(body, token string) string {
	if token == "" {
		return body
	}
	field := `<input type="hidden" name="_csrf" value="` + token + `">`
	return csrfFormPattern.ReplaceAllString(body, `${0}`+field)
}

func withCSRFHTMXActions(body string) string {
	return csrfLinkPattern.ReplaceAllStringFunc(body, func(link string) string {
		parts := csrfLinkPattern.FindStringSubmatch(link)
		if len(parts) != 4 || strings.Contains(strings.ToLower(link), "hx-post=") {
			return link
		}
		href := html.EscapeString(parts[2])
		return `<a` + parts[1] + `href="` + href + `" hx-post="` + href + `"` + parts[3] + `>`
	})
}

// CSRFProtection validates every state-changing request and exposes the current
// session token to both Templ and renderPage responses.
func CSRFProtection(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionToken := session.TokenFromRequest(r)
		csrfToken := ""
		if sessionToken != "" {
			var err error
			csrfToken, err = session.CSRFToken(sessionToken)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				log.Printf("取得 CSRF token 失敗: %v", err)
				http.Error(w, "CSRF token 初始化失敗", http.StatusInternalServerError)
				return
			}
		}

		if isStateChangingMethod(r.Method) && r.URL.Path != "/login" && r.URL.Path != "/webhook/line" {
			provided := strings.TrimSpace(r.Header.Get(csrfHeader))
			if provided == "" {
				provided = strings.TrimSpace(r.FormValue("_csrf"))
			}
			if csrfToken == "" || provided == "" || !session.ConstantTimeEqual(provided, csrfToken) {
				http.Error(w, "CSRF token 無效或缺少", http.StatusForbidden)
				return
			}
		}

		if csrfToken != "" {
			r = r.WithContext(session.WithCSRFToken(r.Context(), csrfToken))
		}
		next.ServeHTTP(&csrfResponseWriter{ResponseWriter: w, token: csrfToken}, r)
	})
}

func isStateChangingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
