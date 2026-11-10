package handler

import (
	"fmt"
	"html"
	"net/http"
	"os"
	"strings"

	"autorepairos/internal/model"
	navpkg "autorepairos/internal/nav"
)

// RequireAuth 擋未登入者，並把使用者注入給頁面 handler。
func RequireAuth(fn func(http.ResponseWriter, *http.Request, model.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := currentUser(r)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		fn(w, r, user)
	}
}

// RequireLogin 擋未登入者，給不需要使用者資訊的 handler 使用。
func RequireLogin(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := currentUser(r); err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		fn(w, r)
	}
}

// appName 取得軟體名稱。handler 套件不依賴 config（避免循環），
// 直接讀環境變數；未設定時沿用預設值。
func appName() string {
	if v := strings.TrimSpace(os.Getenv("APP_NAME")); v != "" {
		return v
	}
	return "AutoRepair OS"
}

func renderPage(w http.ResponseWriter, title, active, username, role, body string) {
	app := appName()
	csrfToken := csrfTokenForResponse(w)
	body = withCSRFFormFields(body, csrfToken)
	body = withCSRFHTMXActions(body)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<!DOCTYPE html>
<html lang="zh-Hant">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + html.EscapeString(title) + ` - ` + html.EscapeString(app) + `</title>
<link rel="stylesheet" href="/static/app.css"/>
<script src="/static/htmx.min.js"></script>
<style>` + pageStyle + `</style>
</head>
<body hx-headers='{"X-CSRF-Token":"` + csrfToken + `"}'>
  <header>
    <h1>🔧 ` + html.EscapeString(app) + `</h1>
    <div class="user">登入中：<code>` + html.EscapeString(username) + `</code> <span class="badge">` + html.EscapeString(role) + `</span>
    <form method="POST" action="/logout" style="display:inline"><button class="logout" type="submit">登出</button></form>
    </div>
  </header>
  <div class="shell">
  <main>` + body + `</main>
  <nav>` + navpkg.Render(nil, active, role) + `</nav>
  </div>
  <script src="/offline.js"></script>
</body>
</html>`))
}

func flash(r *http.Request) string {
	msg := r.URL.Query().Get("msg")
	if msg == "" {
		return ""
	}
	return `<div class="msg msg-ok">` + html.EscapeString(msg) + `</div>`
}

func flashText(r *http.Request) string {
	return r.URL.Query().Get("msg")
}

// redirectErrMsg 與 redirectMsg 相同，但走另一個參數（err）並以紅色樣式呈現。
//
// 為什麼不共用 msg：flash() 一律輸出綠色 msg-ok。把失敗訊息也塞進 msg 會讓
// 「已綁定成功」與「綁定失敗」長得一模一樣，使用者只看顏色就誤判成功。
func redirectErrMsg(w http.ResponseWriter, r *http.Request, path, msg string) {
	http.Redirect(w, r, path+"?err="+urlQueryEscape(msg), http.StatusFound)
}

// flashErr 讀取 redirectErrMsg 留下的錯誤訊息。
func flashErr(r *http.Request) string {
	msg := r.URL.Query().Get("err")
	if msg == "" {
		return ""
	}
	return `<div class="msg msg-err">` + html.EscapeString(msg) + `</div>`
}

func fail(w http.ResponseWriter, msg string) {
	http.Error(w, msg, http.StatusBadRequest)
}

// formError renders a POST validation error as a page: the message appears
// in a msg-err block below the form area (with a link back to the form),
// instead of a bare 400 text response that loses the user's context.
// Status stays 400. Use for form-validation failures (bad / missing input);
// keep fail() for non-form errors (DB failures, permission, etc.).
func formError(w http.ResponseWriter, r *http.Request, title, formPath, msg string) {
	errDiv := `<div class="msg msg-err">` + html.EscapeString(msg) + `</div>`
	// HTMX requests (inline form submission) get only the fragment so it
	// swaps into the form area on the same screen; full-page requests get
	// the page shell with a link back to the form.
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(errDiv))
		return
	}
	user, _ := currentUser(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	renderPage(w, title, "", user.Username, user.Role,
		errDiv+`<p style="margin-top:1em"><a href="`+html.EscapeString(formPath)+`">返回表單重填</a></p>`)
}

func redirectMsg(w http.ResponseWriter, r *http.Request, path, msg string) {
	http.Redirect(w, r, path+"?msg="+urlQueryEscape(msg), http.StatusFound)
}

func urlQueryEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
