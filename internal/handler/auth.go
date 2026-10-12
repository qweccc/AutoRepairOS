package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/internal/session"

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

// currentUser 由 session cookie 解析出使用者。
// token 必須存在 sessions 表且未過期；舊版的 username-id 格式一律視為未登入。
func currentUser(r *http.Request) (model.User, error) {
	var user model.User

	token := session.TokenFromRequest(r)
	if token == "" {
		return user, http.ErrNoCookie
	}

	userID, err := session.Lookup(token)
	if err != nil {
		return user, err
	}

	err = db.DB.QueryRow(
		"SELECT id, username, role, name FROM users WHERE id = ?", userID,
	).Scan(&user.ID, &user.Username, &user.Role, &user.Name)
	if err != nil {
		// 帳號已被刪除：順手清掉孤兒 session
		session.Delete(token)
		return user, err
	}

	return user, nil
}

const loginStyle = pageStyle + `
  .wrap { display: flex; align-items: center; justify-content: center; height: 100vh; }
  form.login { background: #1e293b; border: 1px solid #334155; border-radius: 12px;
               padding: 32px; width: 340px; }
  form.login input { width: 100%; padding: 10px; margin: 6px 0 16px; border-radius: 8px;
                     border: 1px solid #334155; background: #0f172a; color: #e2e8f0; }
  form.login button { width: 100%; padding: 11px; background: #38bdf8; color: #0f172a;
                      border: 0; border-radius: 8px; font-weight: 700; cursor: pointer; }
  .hint { color: #64748b; font-size: 12px; line-height: 1.7; margin-top: 20px; }
`

func LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<!DOCTYPE html>
<html lang="zh-Hant">
<head>
<meta charset="utf-8">
<title>登入 - ` + appName() + `</title>
<style>` + loginStyle + `</style>
</head>
<body>
  <div class="wrap">
    <form class="login" method="POST" action="/login">
      <h2>🔧 ` + appName() + `</h2>
      <label>帳號<input type="text" name="username" required autofocus></label>
      <label>密碼<input type="password" name="password" required></label>
      <button type="submit">登入</button>
      <input type="hidden" name="return_to" value="/">
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

	// 記錄「登入前所在頁面」，登出後回跳過去。
	returnTo := r.FormValue("return_to")
	if returnTo == "" || returnTo == "/login" {
		returnTo = "/"
	}

	token, err := session.Start(user.ID)
	if err != nil {
		http.Error(w, "建立登入階段失敗", http.StatusInternalServerError)
		return
	}
	session.SetCookie(w, token)

	http.Redirect(w, r, "/", http.StatusFound)
}

func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	// 撤銷 sessions 表的記錄，再清除 cookie，最後回跳到登入前的頁面。
	// return_to 從登入表單的隱藏欄位帶回（預設首頁 /，避免回跳 loop）。
	session.Delete(session.TokenFromRequest(r))
	session.ClearCookie(w)
	returnTo := r.URL.Query().Get("return_to")
	if returnTo == "" || returnTo == "/login" {
		returnTo = "/"
	}
	http.Redirect(w, r, returnTo, http.StatusFound)
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

// IndexHandler 渲染首頁總覽（統計卡片 + 最近工單）。
func IndexHandler(w http.ResponseWriter, r *http.Request, user model.User) {
	// href：點卡片就能跳到對應的列表（帶好篩選條件），不必自己找路徑。
	var stats []struct {
		label string
		query string
		href  string
	}
	switch user.Role {
	case "owner":
		stats = []struct {
			label string
			query string
			href  string
		}{
			{"待收款", "SELECT COUNT(1) FROM work_orders wo WHERE wo.status = 'completed' AND COALESCE((SELECT SUM(r.pay_total) FROM receivables r WHERE r.work_order_id = wo.id AND r.voided = 0), 0) < wo.total_amount", "/receivables?unpaid=1"},
			{"待結帳", "SELECT COUNT(1) FROM work_orders WHERE status = 'completed'", "/work-orders?status=completed"},
			{"進行中", "SELECT COUNT(1) FROM work_orders WHERE status = 'in_progress'", "/work-orders?status=in_progress"},
			{"低庫存件", "SELECT COUNT(1) FROM inventory_items WHERE stock_quantity < min_safety_stock", "/inventory?low=1"},
			{"客戶數", "SELECT COUNT(1) FROM customers", "/customers"},
			{"車輛數", "SELECT COUNT(1) FROM vehicles", "/vehicles"},
		}
	case "counter":
		stats = []struct {
			label string
			query string
			href  string
		}{
			{"待處理工單", "SELECT COUNT(1) FROM work_orders WHERE status IN ('pending','in_progress','waiting_parts')", "/work-orders?status=pending"},
			{"待結帳", "SELECT COUNT(1) FROM work_orders WHERE status = 'completed'", "/work-orders?status=completed"},
			{"待收款", "SELECT COUNT(1) FROM work_orders wo WHERE wo.status = 'completed' AND COALESCE((SELECT SUM(r.pay_total) FROM receivables r WHERE r.work_order_id = wo.id AND r.voided = 0), 0) < wo.total_amount", "/receivables?unpaid=1"},
			{"低庫存件", "SELECT COUNT(1) FROM inventory_items WHERE stock_quantity < min_safety_stock", "/inventory?low=1"},
		}
	default: // technician
		stats = []struct {
			label string
			query string
			href  string
		}{
			{"我的工單", "SELECT COUNT(1) FROM work_orders WHERE technician_id = ? AND status IN ('pending','in_progress','waiting_parts')", "/work-orders?mine=1"},
			{"進行中", "SELECT COUNT(1) FROM work_orders WHERE technician_id = ? AND status = 'in_progress'", "/work-orders?mine=1&status=in_progress"},
			{"待結帳", "SELECT COUNT(1) FROM work_orders WHERE technician_id = ? AND status = 'completed'", "/work-orders?mine=1&status=completed"},
		}
	}

	var sh strings.Builder
	for _, s := range stats {
		n := 0
		if strings.Contains(s.query, "?") {
			db.DB.QueryRow(s.query, user.ID).Scan(&n)
		} else {
			n = countRows(s.query)
		}
		sh.WriteString(`<a class="stat" href="` + s.href + `"><b>` +
			strconv.Itoa(n) + `</b><span>` + s.label + `</span></a>`)
	}

	var recent string
	// 排除種子/測試資料（車牌 TEST-* 或客戶名含「測試」），否則正式工單會被擠掉。
	rows, err := db.DB.Query(`SELECT wo.id, v.plate_number, v.brand_model, c.name, wo.status
		FROM work_orders wo
		JOIN vehicles v ON v.id = wo.vehicle_id
		JOIN customers c ON c.id = wo.customer_id
		WHERE v.plate_number NOT LIKE 'TEST-%' AND c.name NOT LIKE '%測試%'
		ORDER BY wo.created_at DESC LIMIT 8`)
	if err == nil {
		defer rows.Close()
		var tb strings.Builder
		tb.WriteString(`<table><tr><th>#</th><th>車牌</th><th>車型</th><th>客戶</th><th>狀態</th></tr>`)
		found := false
		for rows.Next() {
			var id int64
			var plate, model_, cname, status string
			if err := rows.Scan(&id, &plate, &model_, &cname, &status); err != nil {
				continue
			}
			found = true
			tb.WriteString(`<tr><td><a href="/work-orders/` + strconv.FormatInt(id, 10) + `">` + strconv.FormatInt(id, 10) + `</a></td><td>` + plate +
				`</td><td>` + model_ + `</td><td>` + cname + `</td><td><span class="pill ` + statusPill[status] + `">` + workOrderStatusLabel[status] + `</span></td></tr>`)
		}
		tb.WriteString(`</table>`)
		if found {
			recent = `<div class="card"><h2>最近工單</h2>` + tb.String() + `</div>`
		} else {
			recent = `<div class="card"><h2>最近工單</h2><div class="empty">尚無工單</div></div>`
		}
	}

	body := `
    <div class="card">
      <h2>總覽</h2>
      <div>` + sh.String() + `</div>
    </div>` + recent + `
    <div class="card"><h2>功能模組</h2><p style="color:#94a3b8;font-size:13.5px">客戶、車輛、工單、庫存皆可正常使用。</p></div>`

	renderPage(w, "總覽", "/", user.Username, user.Role, body)
}
