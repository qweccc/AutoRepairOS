package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"

	"golang.org/x/crypto/bcrypt"
)

func writeHTML(w http.ResponseWriter, r *http.Request, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	// TODO: Templ 整合後改用 templ.Render
	w.Write([]byte("<html><body>" + name + "</body></html>"))
}

func writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	b, err := json.Marshal(data)
	if err != nil {
		http.Error(w, "序列化失敗", http.StatusInternalServerError)
		return
	}
	w.Write(b)
}

func requireLogin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := r.Context().Value("role")
		if role == nil {
			http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
			return
		}
		next(w, r)
	}
}

func requireRole(roles ...string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ctxRole := r.Context().Value("role")
			if ctxRole == nil {
				http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
				return
			}

			roleStr, ok := ctxRole.(string)
			if !ok {
				http.Error(w, "權限錯誤", http.StatusForbidden)
				return
			}

			allowed := false
			for _, r := range roles {
				if roleStr == r {
					allowed = true
					break
				}
			}

			if !allowed {
				http.Error(w, "權限不足", http.StatusForbidden)
				return
			}

			next(w, r)
		}
	}
}

func LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`
			<html>
			<head><title>登入 - AutoRepair OS</title></head>
			<body>
				<h1>登入 - AutoRepair OS</h1>
				<form method="POST" action="/login">
					<label>帳號: <input type="text" name="username" required></label><br><br>
					<label>密碼: <input type="password" name="password" required></label><br><br>
					<button type="submit">登入</button>
				</form>
				<hr>
				<p>預設測試帳號:</p>
				<ul>
					<li>Owner: <code>owner</code> / <code>Owner001!</code></li>
					<li>Counter: <code>counter</code> / <code>Counter001!</code></li>
					<li>Technician: <code>tech</code> / <code>Tech001!</code></li>
				</ul>
			</body>
			</html>
		`))
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "不正確的方法", http.StatusMethodNotAllowed)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		http.Error(w, "請輸入帳號與密碼", http.StatusBadRequest)
		return
	}

	var user model.User
	err := db.DB.QueryRow("SELECT id, username, password, role, name, created_at FROM users WHERE username = ?", username).
		Scan(&user.ID, &user.Username, &user.Password, &user.Role, &user.Name, &user.CreatedAt)
	if err != nil {
		http.Error(w, "帳號或密碼錯誤", http.StatusUnauthorized)
		return
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password))
	if err != nil {
		http.Error(w, "帳號或密碼錯誤", http.StatusUnauthorized)
		return
	}

	// 建立 session cookie (簡易版)
	sessionToken := strings.ReplaceAll(username+"-"+strconv.FormatInt(user.ID, 10), " ", "_")
	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		MaxAge:   86400 * 7,
	})

	http.Redirect(w, r, "/", http.StatusFound)
}

func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/login", http.StatusFound)
}

func MeHandler(w http.ResponseWriter, r *http.Request) {
	sessionCookie, err := r.Cookie("session_token")
	if err != nil || sessionCookie.Value == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登入"})
		return
	}

	username := strings.Split(sessionCookie.Value, "-")[0]

	var user model.User
	err = db.DB.QueryRow("SELECT id, username, password, role, name, created_at FROM users WHERE username = ?", username).
		Scan(&user.ID, &user.Username, &user.Password, &user.Role, &user.Name, &user.CreatedAt)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "會話無效"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":        user.ID,
		"username":  user.Username,
		"role":      user.Role,
		"name":      user.Name,
	})
}

func IndexHandler(w http.ResponseWriter, r *http.Request) {
	sessionCookie, err := r.Cookie("session_token")
	if err != nil || sessionCookie.Value == "" {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	username := strings.Split(sessionCookie.Value, "-")[0]

	var user model.User
	err = db.DB.QueryRow("SELECT id, username, password, role, name, created_at FROM users WHERE username = ?", username).
		Scan(&user.ID, &user.Username, &user.Password, &user.Role, &user.Name, &user.CreatedAt)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`
		<html>
		<head><title>AutoRepair OS</title></head>
		<body>
			<h1>AutoRepair OS 首頁</h1>
			<p>你好，<code>` + user.Username + `</code>（角色: ` + user.Role + `）</p>
			<p><a href="/logout">登出</a></p>
		</body>
		</html>
	`))
}
