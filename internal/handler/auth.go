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

const pageStyle = `
  * { box-sizing: border-box; }
  body { margin: 0; font-family: "Segoe UI", "Microsoft JhengHei", sans-serif;
         background: #0f172a; color: #e2e8f0; }
  header { background: #1e293b; padding: 16px 28px; display: flex;
           align-items: center; justify-content: space-between;
           border-bottom: 2px solid #334155; }
  header h1 { margin: 0; font-size: 22px; }
  .user { font-size: 14px; }
  .badge { background: #38bdf8; color: #0f172a; padding: 2px 10px;
           border-radius: 999px; font-weight: 700; font-size: 12px; }
  .logout { margin-left: 16px; padding: 8px 18px; background: #ef4444;
            color: #fff; border-radius: 8px; text-decoration: none; font-weight: 700; }
  .logout:hover { background: #dc2626; }
  main { padding: 28px; }
  .card { background: #1e293b; border: 1px solid #334155; border-radius: 12px;
          padding: 20px; margin-bottom: 20px; }
  .card h2 { margin-top: 0; color: #38bdf8; }
  table { width: 100%; border-collapse: collapse; }
  th, td { text-align: left; padding: 10px; border-bottom: 1px solid #334155; font-size: 14px; }
  th { color: #94a3b8; }
  .stat { display: inline-block; margin-right: 28px; }
  .stat b { font-size: 28px; color: #f8fafc; display: block; }
  .stat span { color: #94a3b8; font-size: 13px; }
`

// currentUser 從 session cookie 解析出使用者，未登入或 cookie 無效時回傳 error。
func currentUser(r *http.Request) (model.User, error) {
	var user model.User

	cookie, err := r.Cookie("session_token")
	if err != nil || cookie.Value == "" {
		return user, http.ErrNoCookie
	}

	username := strings.Split(cookie.Value, "-")[0]

	err = db.DB.QueryRow(
		"SELECT id, username, role, name FROM users WHERE username = ?", username,
	).Scan(&user.ID, &user.Username, &user.Role, &user.Name)
	if err != nil {
		return user, err
	}

	return user, nil
}

func renderPage(w http.ResponseWriter, title, username, role, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<!DOCTYPE html>
<html lang="zh-Hant">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + title + ` - AutoRepair OS</title>
<style>` + pageStyle + `</style>
</head>
<body>
  <header>
    <h1>🔧 AutoRepair OS</h1>
    <div class="user">登入中：<code>` + username + `</code> <span class="badge">` + role + `</span>
    <a href="/logout" class="logout">登出</a>
    </div>
  </header>
  <main>` + body + `
  </main>
</body>
</html>`))
}

func LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<!DOCTYPE html>
<html lang="zh-Hant">
<head>
<meta charset="utf-8">
<title>登入 - AutoRepair OS</title>
<style>` + pageStyle + `
  .wrap { display: flex; align-items: center; justify-content: center; height: 100vh; }
  form { background: #1e293b; border: 1px solid #334155; border-radius: 12px;
         padding: 32px; width: 340px; }
  input { width: 100%; padding: 10px; margin: 6px 0 16px; border-radius: 8px;
          border: 1px solid #334155; background: #0f172a; color: #e2e8f0; }
  button { width: 100%; padding: 11px; background: #38bdf8; color: #0f172a;
           border: 0; border-radius: 8px; font-weight: 700; cursor: pointer; }
  .hint { color: #64748b; font-size: 12px; line-height: 1.7; margin-top: 20px; }
</style>
</head>
<body>
  <div class="wrap">
    <form method="POST" action="/login">
      <h2>🔧 AutoRepair OS</h2>
      <label>帳號<input type="text" name="username" required autofocus></label>
      <label>密碼<input type="password" name="password" required></label>
      <button type="submit">登入</button>
      <div class="hint">
        預設帳號：owner / Owner001!<br>
        counter / Counter001!<br>
        tech / Tech001!
      </div>
    </form>
  </div>
</body>
</html>`))
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
	err := db.DB.QueryRow(
		"SELECT id, username, password, role, name FROM users WHERE username = ?", username,
	).Scan(&user.ID, &user.Username, &user.Password, &user.Role, &user.Name)
	if err != nil {
		http.Error(w, "帳號或密碼錯誤", http.StatusUnauthorized)
		return
	}

	if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)) != nil {
		http.Error(w, "帳號或密碼錯誤", http.StatusUnauthorized)
		return
	}

	// TODO: 換成隨機 session token 存進 sessions 表
	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    strings.ReplaceAll(username+"-"+strconv.FormatInt(user.ID, 10), " ", "_"),
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
		HttpOnly: true,
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/login", http.StatusFound)
}

func MeHandler(w http.ResponseWriter, r *http.Request) {
	user, err := currentUser(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, model.SimpleResponse{Error: "未登入"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":       user.ID,
		"username": user.Username,
		"role":     user.Role,
		"name":     user.Name,
	})
}

func IndexHandler(w http.ResponseWriter, r *http.Request) {
	user, err := currentUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	stats := []struct {
		label string
		query string
	}{
		{"待處理工單", "SELECT COUNT(1) FROM work_orders WHERE status IN ('pending','in_progress','waiting_parts')"},
		{"進行中", "SELECT COUNT(1) FROM work_orders WHERE status = 'in_progress'"},
		{"待結帳", "SELECT COUNT(1) FROM work_orders WHERE status = 'completed'"},
		{"客戶數", "SELECT COUNT(1) FROM customers"},
		{"車輛數", "SELECT COUNT(1) FROM vehicles"},
		{"低庫存件", "SELECT COUNT(1) FROM inventory_items WHERE stock_quantity < min_safety_stock"},
	}

	var statHTML strings.Builder
	for _, s := range stats {
		var n int
		if err := db.DB.QueryRow(s.query).Scan(&n); err != nil {
			n = 0
		}
		statHTML.WriteString(`<div class="stat"><b>` + strconv.Itoa(n) + `</b><span>` + s.label + `</span></div>`)
	}

	body := `
    <div class="card">
      <h2>總覽</h2>
      <div>` + statHTML.String() + `</div>
    </div>
    <div class="card">
      <h2>功能模組</h2>
      <p>尚未實作。目前僅有登入與總覽統計。</p>
    </div>`

	renderPage(w, "首頁", user.Username, user.Role, body)
}
