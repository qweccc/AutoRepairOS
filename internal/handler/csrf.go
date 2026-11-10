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

		if isStateChangingMethod(r.Method) && !csrfExempt(r.URL.Path) {
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

// csrfExempt 回報這個路徑的狀態變更請求是否不需要 CSRF token。
//
// 判斷標準：呼叫者「根本沒有 session」的端點。CSRF 要防的是「已登入使用者的
// 瀏覽器被騙去執行操作」，而沒有 session 就沒有可被冒用的身分——session 綁定的
// token 檢查在這種端點上永遠不可能通過，硬擋等於讓功能完全無法使用。
//
// 這些端點的安全性由其他機制承擔，不是靠 CSRF：
//   - /login：登入本身就是要建立 session，此時還沒有 token 可比對。
//   - /webhook/line：由 LINE 平台伺服器呼叫，以簽章驗證來源（VerifyEventSignature）。
//   - /approval/approve/{token}、/approval/reject/{token}：車主在 LINE 上點連結
//     開啟，車主沒有登入、沒有 session。防護是「不可猜測的一次性 token」
//     （32 bytes 隨機，見 generateSecureToken）——capability URL 模式。
//     授權決定本身冪等（狀態只能 pending→approved/rejected），重放無效。
func csrfExempt(path string) bool {
	switch path {
	case "/login", "/webhook/line":
		return true
	case "/set-lang", "/set-theme":
		// 偏好 cookie 寫入：只改變「自己」的語系/主題偏好密文（lang/theme cookie），
		// 不觸發任何有身分的操作；登入者從頁面切換時 HTMX 不帶 _csrf 亦可運作。
		return true
	}
	// 車主端的授權決定端點（公開、無 session、以 token 為憑證）
	if strings.HasPrefix(path, "/approval/approve/") || strings.HasPrefix(path, "/approval/reject/") {
		return true
	}
	return false
}
