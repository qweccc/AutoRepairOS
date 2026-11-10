package handler

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

func ListCustomers(w http.ResponseWriter, r *http.Request, user model.User) {
	renderCustomerList(w, r, user, pages.CustomerCreateForm{})
}

// renderCustomerList queries and renders the customer list page. Shared by
// ListCustomers and CreateCustomer's error path so a failed POST re-renders
// the same templ page with the error and the user's input preserved.
func renderCustomerList(w http.ResponseWriter, r *http.Request, user model.User, form pages.CustomerCreateForm) {
	qv := r.URL.Query()
	q := strings.TrimSpace(qv.Get("q"))
	page := pageNo(r)
	size := pageSize(r)

	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (name LIKE ? OR phone LIKE ? OR COALESCE(address,'') LIKE ? OR COALESCE(notes,'') LIKE ?
			OR COALESCE(tags,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like, like, like)
	}

	total := countRows(`SELECT COUNT(1) FROM active_customers`+where, args...)

	query := `SELECT c.id, c.name, c.phone, COALESCE(c.line_user_id,''), COALESCE(c.telegram_user_id,''),
		COALESCE(c.address,''), COALESCE(c.notification_preference,'none'), COALESCE(c.notes,''),
		COALESCE(c.tags,''), COALESCE(c.campaign_code,''),
		COALESCE((SELECT SUM(r.pay_total) FROM receivables r WHERE r.customer_id = c.id AND r.voided = 0), 0),
		COALESCE((SELECT SUM(CASE WHEN wo.total_amount - COALESCE(p.paid,0) > 0 THEN wo.total_amount - COALESCE(p.paid,0) ELSE 0 END)
			FROM active_work_orders wo
			LEFT JOIN (SELECT work_order_id, SUM(pay_total) paid FROM receivables WHERE voided = 0 GROUP BY work_order_id) p ON p.work_order_id = wo.id
			WHERE wo.customer_id = c.id AND wo.status = 'completed'), 0),
		c.created_at,
		COALESCE(c.birthday,'')
		FROM active_customers c` + where + ` ORDER BY c.id DESC LIMIT ? OFFSET ?`
	pageArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := db.DB.Query(query, pageArgs...)
	if err != nil {
		fail(w, "查詢客戶失敗")
		return
	}
	defer rows.Close()

	var list []model.Customer
	for rows.Next() {
		var c model.Customer
		var createdAt string
		if err := rows.Scan(&c.ID, &c.Name, &c.Phone, &c.LineUserID, &c.TelegramUserID, &c.Address,
			&c.NotificationPref, &c.Notes, &c.Tags, &c.CampaignCode, &c.TotalSpent, &c.UnpaidAmount, &createdAt,
			&c.Birthday); err != nil {
			continue
		}
		c.CreatedAt = parseFlexTime(createdAt)
		list = append(list, c)
	}

	var customerRows []pages.CustomerRow
	for _, c := range list {
		notif := "LINE"
		switch c.NotificationPref {
		case "telegram":
			notif = "Telegram"
		case "sms":
			notif = "SMS（需更新）"
		case "none":
			notif = "不通知"
		}
		customerRows = append(customerRows, pages.CustomerRow{
			ID:             c.ID,
			Name:           c.Name,
			Phone:          c.Phone,
			Tags:           c.Tags,
			Address:        c.Address,
			Notification:   notif,
			LineUserID:     c.LineUserID,
			TelegramUserID: c.TelegramUserID,
			TotalSpent:     c.TotalSpent,
			UnpaidAmount:   c.UnpaidAmount,
		})
	}

	pages.ListCustomersPage(customerRows, total, page, size, q, form, user).Render(r.Context(), w)
}

// SearchCustomersAPI 客戶即時搜尋 API（HTMX 用），回傳純 HTML 列。
func SearchCustomersAPI(w http.ResponseWriter, r *http.Request) {
	user, _ := currentUser(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		pages.CustomerSearchRows(nil, user).Render(r.Context(), w)
		return
	}

	like := "%" + q + "%"
	rows, err := db.DB.Query(`SELECT c.id, c.name, c.phone, COALESCE(c.line_user_id,''), COALESCE(c.telegram_user_id,''),
		COALESCE(c.address,''), COALESCE(c.notification_preference,'none'),
		COALESCE((SELECT SUM(r.pay_total) FROM receivables r WHERE r.customer_id = c.id AND r.voided = 0), 0),
		COALESCE((SELECT SUM(CASE WHEN wo.total_amount - COALESCE(p.paid,0) > 0 THEN wo.total_amount - COALESCE(p.paid,0) ELSE 0 END)
			FROM active_work_orders wo
			LEFT JOIN (SELECT work_order_id, SUM(pay_total) paid FROM receivables WHERE voided = 0 GROUP BY work_order_id) p ON p.work_order_id = wo.id
			WHERE wo.customer_id = c.id AND wo.status = 'completed'), 0)
		FROM active_customers c
		WHERE c.name LIKE ? OR c.phone LIKE ? OR COALESCE(c.address,'') LIKE ? OR COALESCE(c.tags,'') LIKE ?
		ORDER BY c.id DESC LIMIT 50`, like, like, like, like)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var result []pages.CustomerRow
	for rows.Next() {
		var c pages.CustomerRow
		var notifPref string
		if err := rows.Scan(&c.ID, &c.Name, &c.Phone, &c.LineUserID, &c.TelegramUserID,
			&c.Address, &notifPref, &c.TotalSpent, &c.UnpaidAmount); err != nil {
			continue
		}
		switch notifPref {
		case "telegram":
			c.Notification = "Telegram"
		case "sms":
			c.Notification = "SMS（需更新）"
		case "none":
			c.Notification = "不通知"
		default:
			c.Notification = "LINE"
		}
		result = append(result, c)
	}
	pages.CustomerSearchRows(result, user).Render(r.Context(), w)
}

// CustomerPickerAPI 回傳客戶選擇器使用的 JSON 選項。
func CustomerPickerAPI(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) < 1 {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}
	like := "%" + q + "%"
	rows, err := db.DB.Query(`SELECT id, name, COALESCE(phone,'')
		FROM active_customers
		WHERE id = ? OR name LIKE ? OR COALESCE(phone,'') LIKE ? OR COALESCE(address,'') LIKE ?
		ORDER BY CASE WHEN name = ? THEN 0 WHEN name LIKE ? THEN 1 ELSE 2 END, name
		LIMIT 60`, mustInt(q), like, like, like, q, q+"%")
	if err != nil {
		http.Error(w, "搜尋客戶失敗", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	type customerOption struct {
		V     string `json:"v"`
		Label string `json:"label"`
	}
	result := make([]customerOption, 0)
	for rows.Next() {
		var id int64
		var name, phone string
		if err := rows.Scan(&id, &name, &phone); err != nil {
			http.Error(w, "讀取客戶搜尋結果失敗", http.StatusInternalServerError)
			return
		}
		result = append(result, customerOption{
			V:     strconv.FormatInt(id, 10),
			Label: strings.TrimSpace(name) + " / " + strings.TrimSpace(phone),
		})
	}
	writeJSON(w, http.StatusOK, result)
}

func CreateCustomer(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	phone := r.FormValue("phone")

	// customerFormFromRequest refills the create form after a failed POST.
	customerFormFromRequest := func(errMsg string) pages.CustomerCreateForm {
		return pages.CustomerCreateForm{
			ErrMsg:   errMsg,
			Name:     strings.TrimSpace(name),
			Phone:    strings.TrimSpace(phone),
			Address:  strings.TrimSpace(r.FormValue("address")),
			Tags:     strings.TrimSpace(r.FormValue("tags")),
			Campaign: strings.TrimSpace(r.FormValue("campaign_code")),
			Notes:    strings.TrimSpace(r.FormValue("notes")),
			Pref:     notifPref(r.FormValue("notification_preference")),
		}
	}
	rejectCustomer := func(errMsg string) {
		user, _ := currentUser(r)
		w.WriteHeader(http.StatusBadRequest)
		renderCustomerList(w, r, user, customerFormFromRequest(errMsg))
	}

	if strings.TrimSpace(name) == "" || strings.TrimSpace(phone) == "" {
		rejectCustomer("姓名與電話為必填")
		return
	}

	// LINE 綁定一律走 /customers/{id}/line-bind（見 customer_binding.go）：
	// 新增客戶時還不知道他的 LINE 帳號，留著這個欄位只會讓人以為可以隨手填。
	// Telegram 可由表單填，但要驗格式與唯一性。
	telegramID := strings.TrimSpace(r.FormValue("telegram_user_id"))
	if err := validateTelegramUserID(telegramID); err != nil {
		rejectCustomer(err.Error())
		return
	}
	if telegramID != "" {
		if otherID, otherName := telegramBindingTakenByOther(telegramID, 0); otherID != 0 {
			fail(w, fmt.Sprintf("這個 Telegram ID 已綁定客戶 #%d（%s）", otherID, otherName))
			return
		}
	}

	// FIX: Use POST /customers/new for customer creation via a specific route.
	// If this handler is reached via /customers/new, it will still work because
	// r.FormValue doesn't care about the path. However, we need to ensure
	// the router is configured correctly.
	_, err := db.DB.Exec(`INSERT INTO customers
		(name, phone, telegram_user_id, address, notification_preference, notes, tags, campaign_code, birthday)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		strings.TrimSpace(name), strings.TrimSpace(phone),
		nullStr(telegramID), nullStr(r.FormValue("address")),
		notifPref(r.FormValue("notification_preference")), nullStr(r.FormValue("notes")),
		normTags(r.FormValue("tags")), nullStr(r.FormValue("campaign_code")),
		nullStr(r.FormValue("birthday")),
	)
	if err != nil {
		log.Printf("建立客戶失敗（phone=%s）: %v", strings.TrimSpace(phone), err)
		if strings.Contains(err.Error(), "UNIQUE") {
			rejectCustomer("電話號碼或 Telegram ID 已存在")
			return
		}
		fail(w, "建立客戶失敗")
		return
	}

	redirectMsg(w, r, "/customers", "客戶已建立")
}

func EditCustomerForm(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))

	var c model.Customer
	err := db.DB.QueryRow(`SELECT id, name, phone, COALESCE(line_user_id,''), COALESCE(telegram_user_id,''),
		COALESCE(address,''), COALESCE(notification_preference,'none'), COALESCE(notes,''),
		COALESCE(tags,''), COALESCE(campaign_code,''),
		COALESCE(total_spent,0), COALESCE(unpaid_amount,0), COALESCE(birthday,'')
		FROM active_customers WHERE id = ?`, id).
		Scan(&c.ID, &c.Name, &c.Phone, &c.LineUserID, &c.TelegramUserID, &c.Address,
			&c.NotificationPref, &c.Notes, &c.Tags, &c.CampaignCode, &c.TotalSpent, &c.UnpaidAmount, &c.Birthday)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	c.Birthday = normDate(c.Birthday)
	pages.EditCustomerPage(pages.CustomerEditData{
		Customer: c, SMSWarning: c.NotificationPref == "sms",
		Flash: r.URL.Query().Get("msg"), FlashErr: r.URL.Query().Get("err"), User: user,
	}).Render(r.Context(), w)
}

// UpdateCustomer 更新客戶基本資料。
//
// 刻意**不接受** line_user_id：綁定改由 /customers/{id}/line-bind 與
// /line-unbind 兩個專用端點處理。原本表單直接 UPDATE 會繞過 bindLineUser 的
// 唯一性檢查，讓同一個 LINE 帳號綁到兩個客戶，而履歷查詢用
// `SELECT id FROM customers WHERE line_user_id = ?` 任取一筆，
// 於是對方會看到不屬於他的維修紀錄與金額（詳見 customer_binding.go）。
// 移除這個欄位後，所有寫入 line_user_id 的路徑都必須經過綁定端點。
func UpdateCustomer(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	name := strings.TrimSpace(r.FormValue("name"))
	phone := strings.TrimSpace(r.FormValue("phone"))

	editPath := "/customers/" + strconv.FormatInt(id, 10) + "/edit"
	if name == "" || phone == "" {
		formError(w, r, "編輯客戶", editPath, "姓名與電話為必填")
		return
	}

	// Telegram 仍可由表單編輯（客戶查得到自己的 chat id），但要驗格式與唯一性。
	// 這裡不驗就會讓同一個 chat id 綁兩個客戶 → 通知送錯人。
	telegramID := strings.TrimSpace(r.FormValue("telegram_user_id"))
	if err := validateTelegramUserID(telegramID); err != nil {
		formError(w, r, "編輯客戶", editPath, err.Error())
		return
	}
	if telegramID != "" {
		if otherID, otherName := telegramBindingTakenByOther(telegramID, id); otherID != 0 {
			fail(w, fmt.Sprintf("這個 Telegram ID 已綁定客戶 #%d（%s），同一個帳號不能綁兩個客戶",
				otherID, otherName))
			return
		}
	}

	res, err := db.DB.Exec(`UPDATE customers SET name=?, phone=?, telegram_user_id=?,
		address=?, notification_preference=?, notes=?, tags=?, campaign_code=?, birthday=? WHERE id=?`,
		name, phone, nullStr(telegramID), nullStr(r.FormValue("address")),
		notifPref(r.FormValue("notification_preference")), nullStr(r.FormValue("notes")),
		normTags(r.FormValue("tags")), nullStr(r.FormValue("campaign_code")),
		nullStr(r.FormValue("birthday")), id)
	if err != nil {
		log.Printf("更新客戶失敗（客戶 #%d）: %v", id, err)
		if strings.Contains(err.Error(), "UNIQUE") {
			fail(w, "電話號碼或 Telegram ID 已被其他客戶使用")
			return
		}
		fail(w, "更新客戶失敗")
		return
	}

	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}

	redirectMsg(w, r, "/customers", "客戶已更新")
}

func DeleteCustomer(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	hasVehicles := countRows("SELECT COUNT(1) FROM active_vehicles WHERE customer_id = ?", id)
	if hasVehicles > 0 {
		fail(w, "此客戶仍有車輛，請先移轉或刪除車輛")
		return
	}

	hasOrders := countRows("SELECT COUNT(1) FROM active_work_orders WHERE customer_id = ?", id)
	if hasOrders > 0 {
		fail(w, "此客戶仍有工單紀錄，無法刪除")
		return
	}

	res, err := db.DB.Exec("UPDATE customers SET deleted_at = datetime('now') WHERE id = ? AND deleted_at IS NULL", id)
	if err != nil {
		fail(w, "刪除客戶失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}

	redirectMsg(w, r, "/customers", "客戶已刪除")
}

func notifPref(v string) string {
	switch v {
	case "line", "telegram", "none":
		return v
	case "sms":
		// 保留 SMS 值但不當作有效通道；通知發送時會標記為失敗
		return "sms"
	default:
		return "none"
	}
}

// tagPills 把逗號分隔的標籤渲染成小圓標；VIP 用醒目色，其餘用中性色。
// 奧客警示這類負面標記用紅，讓櫃檯在列表就能一眼看到。
func tagPills(tags string) string {
	if strings.TrimSpace(tags) == "" {
		return `<span style="color:#64748b">—</span>`
	}
	var b strings.Builder
	for _, t := range strings.Split(tags, ",") {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		style := "background:#1e3a5f;color:#bae6fd"
		up := strings.ToUpper(t)
		if up == "VIP" {
			style = "background:#3b2f13;color:#fde68a"
		} else if strings.Contains(up, "奧客") || strings.Contains(up, "警示") {
			style = "background:#7f1d1d;color:#fca5a5"
		}
		b.WriteString(`<span class="pill" style="` + style + `;font-size:11.5px;margin-right:3px">` +
			htmlEsc(t) + `</span>`)
	}
	return b.String()
}

// normTags 正規化客戶標籤（FR-CRM-01）：以逗號分隔多值。
// 去掉空白、丟掉空項、去重（保留順序），避免「VIP, VIP」與「vip」分裂成
// 不同標籤而讓篩選失效。
func normTags(v string) string {
	seen := map[string]bool{}
	var out []string
	for _, t := range strings.Split(v, ",") {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		key := strings.ToUpper(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	return strings.Join(out, ",")
}
