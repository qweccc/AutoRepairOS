package handler

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// 廠商管理。
//
// 舊系統把三種東西混在 Staff 表，匯入時已拆開：
//   - 材料商（CAR35 Firm 表，559 家）— 賣零件給我方，vendor_type='material'
//   - 協力廠商（CAR35 Staff Branch H，如輪胎行、鈑金廠）— 外包施工，'vendor'
// 兩者在採購與成本追蹤上的角色不同，因此分開篩選。
//
// 為什麼需要獨立頁面：技師指派的下拉選單只列員工，廠商不能出現在那裡
// （否則會把外包廠商當成自己的技師來派工），所以廠商要自己的入口。

const supplierCols = `s.id, s.name, COALESCE(s.phone,''), COALESCE(s.contact_person,''),
	COALESCE(s.address,''), COALESCE(s.notes,''), COALESCE(s.vendor_type,'material'),
	s.created_at`

func scanSupplier(rows interface{ Scan(...interface{}) error }) (model.Supplier, error) {
	var s model.Supplier
	var createdAt string
	err := rows.Scan(&s.ID, &s.Name, &s.Phone, &s.ContactPerson, &s.Address,
		&s.Notes, &s.VendorType, &createdAt)
	s.CreatedAt = parseFlexTime(createdAt)
	return s, err
}

var vendorTypeLabel = map[string]string{
	"material": "材料商",
	"vendor":   "協力廠商",
	"other":    "其他",
}

func ListSuppliers(w http.ResponseWriter, r *http.Request, user model.User) {
	qv := r.URL.Query()
	q := strings.TrimSpace(qv.Get("q"))
	vtype := strings.TrimSpace(qv.Get("type"))
	page := pageNo(r)
	size := pageSize(r)

	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (s.name LIKE ? OR COALESCE(s.phone,'') LIKE ? OR COALESCE(s.contact_person,'') LIKE ? OR COALESCE(s.notes,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like, like)
	}
	if vtype != "" {
		where += ` AND COALESCE(s.vendor_type,'material') = ?`
		args = append(args, vtype)
	}

	total := countRows(`SELECT COUNT(1) FROM suppliers s`+where, args...)

	query := `SELECT ` + supplierCols + ` FROM suppliers s` + where +
		` ORDER BY s.vendor_type, s.name LIMIT ? OFFSET ?`
	pageArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := db.DB.Query(query, pageArgs...)
	if err != nil {
		fail(w, "查詢廠商失敗")
		return
	}
	defer rows.Close()

	var list []model.Supplier
	for rows.Next() {
		s, err := scanSupplier(rows)
		if err != nil {
			continue
		}
		list = append(list, s)
	}

	// 各型態筆數（篩選籤用）
	matCount := countRows(`SELECT COUNT(1) FROM suppliers WHERE COALESCE(vendor_type,'material')='material'`)
	venCount := countRows(`SELECT COUNT(1) FROM suppliers WHERE vendor_type='vendor'`)

	var tb strings.Builder
	tb.WriteString(`<table><tr><th>名稱</th><th>型態</th><th>電話</th><th>聯絡人</th><th>地址</th><th>備註</th></tr>`)
	for _, s := range list {
		label := vendorTypeLabel[s.VendorType]
		cls := "background:#1e3a5f;color:#bae6fd"   // 材料商：藍
		if s.VendorType == "vendor" {
			cls = "background:#4a2f13;color:#fde68a" // 協力廠商：橙
		}
		note := s.Notes
		// 舊編號對技師查詢有用，但不用搶版面
		tb.WriteString(`<tr><td><b>` + htmlEsc(s.Name) + `</b></td><td><span class="pill" style="` + cls + `">` +
			label + `</span></td><td>` + orDash(htmlEsc(s.Phone)) + `</td><td>` + orDash(htmlEsc(s.ContactPerson)) +
			`</td><td style="font-size:12.5px">` + orDash(htmlEsc(s.Address)) +
			`</td><td style="font-size:12px;color:#94a3b8">` + orDash(htmlEsc(note)) + `</td></tr>`)
	}
	tb.WriteString(`</table>`)

	table := tb.String()
	if len(list) == 0 {
		table = `<div class="empty">沒有符合條件的廠商</div>`
	}

	tab := func(label, key string) string {
		cls := "btn-s"
		if vtype == key {
			cls = "btn-p"
		}
		u := url.Values{}
		if q != "" {
			u.Set("q", q)
		}
		if key != "" {
			u.Set("type", key)
		}
		href := "/suppliers"
		if s := u.Encode(); s != "" {
			href += "?" + s
		}
		return `<a class="btn ` + cls + `" href="` + href + `">` + label + `</a>`
	}

	body := flash(r) + `
    <div class="card">
      <h2>廠商（符合條件 ` + strconv.Itoa(total) + ` 家）</h2>
      <p style="color:#94a3b8;font-size:13.5px;margin:0 0 14px">
        舊系統把材料商與外包廠商混在人員表裡，匯入時已分開：<br>
        <b>材料商</b>是賣零件給我方的（採購成本歸這裡）；
        <b>協力廠商</b>是外包施工的（如輪胎行、鈑金廠、烤漆廠）。<br>
        技師指派只會列出真正的員工，廠商不會出現在技師選單中。
      </p>
      <div style="margin-bottom:12px">` +
		tab("全部 "+strconv.Itoa(matCount+venCount), "") + ` ` +
		tab("材料商 "+strconv.Itoa(matCount), "material") + ` ` +
		tab("協力廠商 "+strconv.Itoa(venCount), "vendor") + `
      </div>
      <form method="GET" action="/suppliers" style="margin-bottom:12px">
        <input name="q" value="` + htmlEsc(q) + `" placeholder="搜尋名稱 / 電話 / 聯絡人" style="padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;width:280px">
        ` + hiddenInputs(qv, "q", "page", "per") + `
        <button class="btn btn-p" type="submit">搜尋</button>
        <a class="btn btn-s" href="/suppliers">清除</a>
      </form>` + table + pager("/suppliers", qv, total, page, size) +
		`<div style="margin-top:6px">` + perPageSel("/suppliers", qv, size) + `</div>
    </div>`

	renderPage(w, "廠商管理", "/suppliers", user.Username, user.Role, body)
}
