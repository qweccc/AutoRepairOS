package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/internal/session"
	"autorepairos/web/pages"

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
		"SELECT id, username, role, name, active FROM users WHERE id = ?", userID,
	).Scan(&user.ID, &user.Username, &user.Role, &user.Name, &user.Active)
	if err != nil {
		// 帳號已被刪除：順手清掉孤兒 session
		session.Delete(token)
		return user, err
	}
	if user.Active == 0 {
		// 帳號已停用：清掉 session，讓前端導回登入頁
		session.Delete(token)
		return user, http.ErrNoCookie
	}

	return user, nil
}

// loginFail 登入失敗：回傳 Templ 登入頁並帶錯誤訊息（保留輸入的 return_to）。
func loginFail(w http.ResponseWriter, r *http.Request, errMsg string) {
	returnTo := r.FormValue("return_to")
	if returnTo == "" || returnTo == "/login" {
		returnTo = "/"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	pages.LoginPage(appName(), errMsg, returnTo).Render(r.Context(), w)
}

func LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		returnTo := r.URL.Query().Get("return_to")
		if returnTo == "" {
			returnTo = "/"
		}
		errMsg := r.URL.Query().Get("error")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		pages.LoginPage(appName(), errMsg, returnTo).Render(r.Context(), w)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "不正確的方法", http.StatusMethodNotAllowed)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		loginFail(w, r, "請輸入帳號與密碼")
		return
	}

	var user model.User
	err := db.DB.QueryRow(
		"SELECT id, username, password, role, name, active FROM users WHERE username = ?", username,
	).Scan(&user.ID, &user.Username, &user.Password, &user.Role, &user.Name, &user.Active)
	if err != nil {
		loginFail(w, r, "帳號或密碼錯誤")
		return
	}

	if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)) != nil {
		loginFail(w, r, "帳號或密碼錯誤")
		return
	}

	if user.Active == 0 {
		loginFail(w, r, "帳號已停用，請聯絡管理員")
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
	session.SetCookie(w, r, token)

	http.Redirect(w, r, "/", http.StatusFound)
}

func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	// 撤銷 sessions 表的記錄，再清除 cookie，最後回跳到登入前的頁面。
	// return_to 從登入表單的隱藏欄位帶回（預設首頁 /，避免回跳 loop）。
	session.Delete(session.TokenFromRequest(r))
	session.ClearCookie(w, r)
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
			{"待收款", "SELECT COUNT(1) FROM active_work_orders wo WHERE wo.status = 'completed' AND COALESCE((SELECT SUM(r.pay_total) FROM receivables r WHERE r.work_order_id = wo.id AND r.voided = 0), 0) < wo.total_amount", "/receivables?unpaid=1"},
			{"待結帳", "SELECT COUNT(1) FROM active_work_orders WHERE status = 'completed'", "/work-orders?status=completed"},
			{"進行中", "SELECT COUNT(1) FROM active_work_orders WHERE status = 'in_progress'", "/work-orders?status=in_progress"},
			{"低庫存件", "SELECT COUNT(1) FROM active_inventory_items WHERE stock_quantity < min_safety_stock", "/inventory?low=1"},
			{"客戶數", "SELECT COUNT(1) FROM active_customers", "/customers"},
			{"車輛數", "SELECT COUNT(1) FROM active_vehicles", "/vehicles"},
		}
	case "counter":
		stats = []struct {
			label string
			query string
			href  string
		}{
			{"待處理工單", "SELECT COUNT(1) FROM active_work_orders WHERE status IN ('pending','in_progress','waiting_parts')", "/work-orders?status=pending"},
			{"待結帳", "SELECT COUNT(1) FROM active_work_orders WHERE status = 'completed'", "/work-orders?status=completed"},
			{"待收款", "SELECT COUNT(1) FROM active_work_orders wo WHERE wo.status = 'completed' AND COALESCE((SELECT SUM(r.pay_total) FROM receivables r WHERE r.work_order_id = wo.id AND r.voided = 0), 0) < wo.total_amount", "/receivables?unpaid=1"},
			{"低庫存件", "SELECT COUNT(1) FROM active_inventory_items WHERE stock_quantity < min_safety_stock", "/inventory?low=1"},
		}
	default: // technician
		stats = []struct {
			label string
			query string
			href  string
		}{
			{"我的工單", "SELECT COUNT(1) FROM active_work_orders WHERE technician_id = ? AND status IN ('pending','in_progress','waiting_parts')", "/work-orders?mine=1"},
			{"進行中", "SELECT COUNT(1) FROM active_work_orders WHERE technician_id = ? AND status = 'in_progress'", "/work-orders?mine=1&status=in_progress"},
			{"待結帳", "SELECT COUNT(1) FROM active_work_orders WHERE technician_id = ? AND status = 'completed'", "/work-orders?mine=1&status=completed"},
		}
	}

	statsOut := make([]pages.DashboardStat, 0, len(stats))
	for _, s := range stats {
		n := 0
		if strings.Contains(s.query, "?") {
			if err := db.DB.QueryRow(s.query, user.ID).Scan(&n); err != nil {
				log.Printf("讀取首頁統計「%s」失敗: %v", s.label, err)
				fail(w, "讀取首頁統計失敗，請稍後再試")
				return
			}
		} else {
			if err := db.DB.QueryRow(s.query).Scan(&n); err != nil {
				log.Printf("讀取首頁統計「%s」失敗: %v", s.label, err)
				fail(w, "讀取首頁統計失敗，請稍後再試")
				return
			}
		}
		statsOut = append(statsOut, pages.DashboardStat{Label: s.label, Href: s.href, Count: n})
	}

	issues := []pages.DashboardIssue{
		{
			Label:  "客戶缺少電話",
			Detail: "電話是客戶搜尋與通知的重要資料",
			Href:   "/customers",
			Query:  `SELECT COUNT(1) FROM active_customers WHERE TRIM(COALESCE(phone, '')) = ''`,
		},
		{
			Label:  "庫存為負數",
			Detail: "庫存數量小於 0 的零件需要盤點",
			Href:   "/inventory",
			Query:  `SELECT COUNT(1) FROM inventory_items WHERE stock_quantity < 0`,
		},
		{
			Label:  "車輛資料不完整",
			Detail: "缺少車牌、車型或車主的車輛需要補正",
			Href:   "/vehicles",
			Query: `SELECT COUNT(1) FROM active_vehicles v
				LEFT JOIN active_customers c ON c.id = v.customer_id
				WHERE TRIM(COALESCE(v.plate_number, '')) = ''
				   OR TRIM(COALESCE(v.brand_model, '')) = ''
				   OR c.id IS NULL`,
		},
	}
	issuesOut := make([]pages.DashboardIssue, 0, len(issues))
	for _, issue := range issues {
		var count int
		if err := db.DB.QueryRow(issue.Query).Scan(&count); err != nil {
			log.Printf("讀取首頁異常資料「%s」失敗: %v", issue.Label, err)
			fail(w, "讀取首頁異常資料失敗，請稍後再試")
			return
		}
		if count > 0 {
			issue.Count = count
			issuesOut = append(issuesOut, issue)
		}
	}

	var recentOut []pages.DashboardRecentWorkOrder
	// 排除種子/測試資料（車牌 TEST-* 或客戶名含「測試」），否則正式工單會被擠掉。
	rows, err := db.DB.Query(`SELECT wo.id, v.plate_number, v.brand_model, c.name, wo.status
		FROM active_work_orders wo
		JOIN active_vehicles v ON v.id = wo.vehicle_id
		JOIN active_customers c ON c.id = wo.customer_id
		WHERE v.plate_number NOT LIKE 'TEST-%' AND c.name NOT LIKE '%測試%'
		ORDER BY wo.created_at DESC LIMIT 8`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id int64
			var plate, model_, cname, status string
			if err := rows.Scan(&id, &plate, &model_, &cname, &status); err != nil {
				log.Printf("讀取首頁最近工單失敗: %v", err)
				fail(w, "讀取最近工單失敗，請稍後再試")
				return
			}
			recentOut = append(recentOut, pages.DashboardRecentWorkOrder{
				ID: id, PlateNumber: plate, BrandModel: model_, CustomerName: cname,
				StatusLabel: workOrderStatusLabel[status], StatusPill: statusPill[status],
			})
		}
		if err := rows.Err(); err != nil {
			log.Printf("列舉首頁最近工單失敗: %v", err)
			fail(w, "讀取最近工單失敗，請稍後再試")
			return
		}
	} else {
		log.Printf("查詢首頁最近工單失敗: %v", err)
		fail(w, "讀取最近工單失敗，請稍後再試")
	}

	pages.DashboardPage(statsOut, issuesOut, recentOut, user).Render(r.Context(), w)
}
