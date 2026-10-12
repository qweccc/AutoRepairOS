package handler

import (
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

func ListCustomers(w http.ResponseWriter, r *http.Request, user model.User) {
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

	total := countRows(`SELECT COUNT(1) FROM customers`+where, args...)

	query := `SELECT id, name, phone, COALESCE(line_user_id,''), COALESCE(telegram_user_id,''),
		COALESCE(address,''), COALESCE(notification_preference,'none'), COALESCE(notes,''),
		COALESCE(tags,''), COALESCE(campaign_code,''),
		COALESCE(total_spent,0), COALESCE(unpaid_amount,0), created_at
		FROM customers` + where + ` ORDER BY id DESC LIMIT ? OFFSET ?`
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
			&c.NotificationPref, &c.Notes, &c.Tags, &c.CampaignCode, &c.TotalSpent, &c.UnpaidAmount, &createdAt); err != nil {
			continue
		}
		c.CreatedAt = parseFlexTime(createdAt)
		list = append(list, c)
	}

	var tb strings.Builder
	tb.WriteString(`<table><tr><th>ID</th><th>姓名</th><th>電話</th><th>標籤</th><th>地址</th><th>通知</th><th>LINE ID</th><th>Telegram ID</th><th class="num">累計消費</th><th class="num">未付</th><th>操作</th></tr>`)
	for _, c := range list {
		notif := "LINE"
		switch c.NotificationPref {
		case "telegram":
			notif = "Telegram"
		case "sms":
			notif = "SMS"
		case "none":
			notif = "不通知"
		}
		tb.WriteString(`<tr><td>` + strconv.FormatInt(c.ID, 10) + `</td><td>` + htmlEsc(c.Name) + `</td><td>` +
			htmlEsc(c.Phone) + `</td><td style="font-size:12.5px">` + tagPills(c.Tags) + `</td><td>` + htmlEsc(c.Address) + `</td><td>` + notif +
			`</td><td>` + htmlEsc(orDash(c.LineUserID)) + `</td><td>` + htmlEsc(orDash(c.TelegramUserID)) +
			`</td><td class="num">` + money(c.TotalSpent) + `</td><td class="num">` + money(c.UnpaidAmount) +
			`</td><td><a class="btn btn-p" href="/work-orders/new?customer=` + strconv.FormatInt(c.ID, 10) + `">開工單</a> ` +
			`<a class="btn btn-s" href="/customers/` + strconv.FormatInt(c.ID, 10) + `/edit">編輯</a> ` +
			`<a class="btn btn-o" href="/vehicles?customer=` + strconv.FormatInt(c.ID, 10) + `">車輛</a> ` +
			`<a class="btn btn-s" href="/customers/` + strconv.FormatInt(c.ID, 10) + `/ar-statement">催款單</a> ` +
			`<a class="btn btn-d" href="/customers/` + strconv.FormatInt(c.ID, 10) + `/delete" onclick="return confirm('確定刪除這位客戶？')">刪除</a></td></tr>`)
	}
	tb.WriteString(`</table>`)

	table := tb.String()
	if len(list) == 0 {
		table = `<div class="empty">沒有符合條件的客戶</div>`
	}

	body := flash(r) + `
    <div class="card">
      <h2>新增客戶</h2>
      <form class="frm" method="POST" action="/customers">
        <label>姓名<input name="name" required></label>
        <label>電話<input name="phone" required></label>
        <label>通知偏好
          <select name="notification_preference">
            <option value="none" selected>不通知</option>
            <option value="line">LINE</option>
            <option value="telegram">Telegram</option>
            <option value="sms">SMS</option>
          </select>
        </label>
        <label class="full">地址<input name="address"></label>
        <label class="full">LINE User ID<input name="line_user_id" placeholder="例：U1234567890abcdef（LINE 官方帳號後台取得）"></label>
        <label class="full">Telegram User ID<input name="telegram_user_id" placeholder="例：123456789（Telegram Bot 對話後台取得）"></label>
        <label class="full">標籤<input name="tags" placeholder="以逗號分隔，例：VIP,奧客警示"></label>
        <label class="full">行銷活動代碼<input name="campaign_code" placeholder="例：2026Q4-煞車促銷（供方案成效分析）"></label>
        <label class="full">備註<textarea name="notes"></textarea></label>
        <div class="full"><button class="btn btn-p" type="submit">建立客戶</button></div>
      </form>
    </div>
    <div class="card">
      <h2>客戶列表（符合條件 ` + strconv.Itoa(total) + ` 筆）</h2>
      <form method="GET" action="/customers" style="margin-bottom:12px">
        <input name="q" value="` + htmlEsc(q) + `" placeholder="搜尋姓名 / 電話 / 地址" style="padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;width:280px">
        <button class="btn btn-p" type="submit">搜尋</button>
        <a class="btn btn-s" href="/customers">清除</a>
      </form>` + table + pager("/customers", qv, total, page, size) +
		`<div style="margin-top:6px">` + perPageSel("/customers", qv, size) + `</div>
    </div>`

	renderPage(w, "客戶管理", "/customers", user.Username, user.Role, body)
}

func CreateCustomer(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	phone := r.FormValue("phone")

	if strings.TrimSpace(name) == "" || strings.TrimSpace(phone) == "" {
		fail(w, "姓名與電話為必填")
		return
	}

	_, err := db.DB.Exec(`INSERT INTO customers
		(name, phone, line_user_id, telegram_user_id, address, notification_preference, notes, tags, campaign_code)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		strings.TrimSpace(name), strings.TrimSpace(phone),
		nullStr(r.FormValue("line_user_id")), nullStr(r.FormValue("telegram_user_id")), nullStr(r.FormValue("address")),
		notifPref(r.FormValue("notification_preference")), nullStr(r.FormValue("notes")),
		normTags(r.FormValue("tags")), nullStr(r.FormValue("campaign_code")),
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			fail(w, "電話號碼已存在")
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
		COALESCE(total_spent,0), COALESCE(unpaid_amount,0)
		FROM customers WHERE id = ?`, id).
		Scan(&c.ID, &c.Name, &c.Phone, &c.LineUserID, &c.TelegramUserID, &c.Address,
			&c.NotificationPref, &c.Notes, &c.Tags, &c.CampaignCode, &c.TotalSpent, &c.UnpaidAmount)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	body := `
    <div class="card">
      <h2>編輯客戶 #` + strconv.FormatInt(c.ID, 10) + `</h2>
      <form class="frm" method="POST" action="/customers/` + strconv.FormatInt(c.ID, 10) + `">
        <label>姓名<input name="name" value="` + htmlEsc(c.Name) + `" required></label>
        <label>電話<input name="phone" value="` + htmlEsc(c.Phone) + `" required></label>
        <label>通知偏好
          <select name="notification_preference">
            <option value="none"` + selIf(c.NotificationPref == "none") + `>不通知</option>
            <option value="line"` + selIf(c.NotificationPref == "line") + `>LINE</option>
            <option value="telegram"` + selIf(c.NotificationPref == "telegram") + `>Telegram</option>
            <option value="sms"` + selIf(c.NotificationPref == "sms") + `>SMS</option>
          </select>
        </label>
        <label class="full">地址<input name="address" value="` + htmlEsc(c.Address) + `"></label>
        <label class="full">LINE User ID<input name="line_user_id" value="` + htmlEsc(c.LineUserID) + `" placeholder="例：U1234567890abcdef"></label>
        <label class="full">Telegram User ID<input name="telegram_user_id" value="` + htmlEsc(c.TelegramUserID) + `" placeholder="例：123456789"></label>
        <label class="full">標籤<input name="tags" value="` + htmlEsc(c.Tags) + `" placeholder="以逗號分隔，例：VIP,奧客警示"></label>
        <label class="full">行銷活動代碼<input name="campaign_code" value="` + htmlEsc(c.CampaignCode) + `" placeholder="例：2026Q4-煞車促銷"></label>
        <label class="full">備註<textarea name="notes">` + htmlEsc(c.Notes) + `</textarea></label>
        <div class="full"><button class="btn btn-p" type="submit">儲存</button> <a class="btn btn-s" href="/customers">取消</a></div>
      </form>
    </div>`

	renderPage(w, "編輯客戶", "/customers", user.Username, user.Role, body)
}

func UpdateCustomer(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	name := strings.TrimSpace(r.FormValue("name"))
	phone := strings.TrimSpace(r.FormValue("phone"))

	if name == "" || phone == "" {
		fail(w, "姓名與電話為必填")
		return
	}

	res, err := db.DB.Exec(`UPDATE customers SET name=?, phone=?, line_user_id=?, telegram_user_id=?,
		address=?, notification_preference=?, notes=?, tags=?, campaign_code=? WHERE id=?`,
		name, phone, nullStr(r.FormValue("line_user_id")), nullStr(r.FormValue("telegram_user_id")), nullStr(r.FormValue("address")),
		notifPref(r.FormValue("notification_preference")), nullStr(r.FormValue("notes")),
		normTags(r.FormValue("tags")), nullStr(r.FormValue("campaign_code")), id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			fail(w, "電話號碼已存在")
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

	hasVehicles := countRows("SELECT COUNT(1) FROM vehicles WHERE customer_id = ?", id)
	if hasVehicles > 0 {
		fail(w, "此客戶仍有車輛，請先移轉或刪除車輛")
		return
	}

	hasOrders := countRows("SELECT COUNT(1) FROM work_orders WHERE customer_id = ?", id)
	if hasOrders > 0 {
		fail(w, "此客戶仍有工單紀錄，無法刪除")
		return
	}

	res, err := db.DB.Exec("DELETE FROM customers WHERE id = ?", id)
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
	case "line", "sms", "telegram", "none":
		return v
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
