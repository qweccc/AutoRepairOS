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
	COALESCE(s.active,1), s.created_at`

func scanSupplier(rows interface{ Scan(...interface{}) error }) (model.Supplier, error) {
	var s model.Supplier
	var createdAt string
	err := rows.Scan(&s.ID, &s.Name, &s.Phone, &s.ContactPerson, &s.Address,
		&s.Notes, &s.VendorType, &s.Active, &createdAt)
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
	tb.WriteString(`<table><tr><th>名稱</th><th>狀態</th><th>型態</th><th>電話</th><th>聯絡人</th><th>地址</th><th>備註</th><th>操作</th></tr>`)
	for _, s := range list {
		label := vendorTypeLabel[s.VendorType]
		cls := "background:#1e3a5f;color:#bae6fd"   // 材料商：藍
		if s.VendorType == "vendor" {
			cls = "background:#4a2f13;color:#fde68a" // 協力廠商：橙
		}
		note := s.Notes
		statusCell := `<span class="pill" style="background:#14532d;color:#86efac">啟用</span>`
		editBtn := `<a class="btn btn-s" href="/suppliers/` + strconv.FormatInt(s.ID, 10) + `/edit">編輯</a>`
		if s.Active == 0 {
			statusCell = `<span class="pill" style="background:#7f1d1d;color:#fca5a5">停用</span>`
			editBtn = `<a class="btn btn-s" href="/suppliers/` + strconv.FormatInt(s.ID, 10) + `/reactivate">復用</a>`
		}
		tb.WriteString(`<tr><td><b>` + htmlEsc(s.Name) + `</b></td><td>` + statusCell + `</td><td><span class="pill" style="` + cls + `">` +
			label + `</span></td><td>` + orDash(htmlEsc(s.Phone)) + `</td><td>` + orDash(htmlEsc(s.ContactPerson)) +
			`</td><td style="font-size:12.5px">` + orDash(htmlEsc(s.Address)) +
			`</td><td style="font-size:12px;color:#94a3b8">` + orDash(htmlEsc(note)) + `</td><td>` + editBtn + `</td></tr>`)
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

	// 新增表單：只給櫃檯與管理員（技師只看得到列表）。
	addForm := ``
	if user.Role == "owner" || user.Role == "counter" {
		addForm = `
    <div class="card">
      <h2>新增廠商</h2>
      <form class="frm" method="POST" action="/suppliers">
        <label>名稱<input name="name" required placeholder="如：○○輪胎行"></label>
        <label>類型<select name="vendor_type">
          <option value="material">材料商</option>
          <option value="vendor">協力廠商</option>
        </select></label>
        <label>電話<input name="phone"></label>
        <label>聯絡人<input name="contact_person"></label>
        <label>地址<input name="address"></label>
        <label class="full">備註<textarea name="notes"></textarea></label>
        <div class="full"><button class="btn btn-p" type="submit">新增</button></div>
      </form>
    </div>`
	}

	body := flash(r) + addForm + `
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

// CreateSupplier 新增供應商。
func CreateSupplier(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		fail(w, "名稱為必填")
		return
	}
	vtype := strings.TrimSpace(r.FormValue("vendor_type"))
	if vtype != "material" && vtype != "vendor" {
		vtype = "material"
	}
	_, err := db.DB.Exec(`INSERT INTO suppliers (name, phone, contact_person, address, notes, vendor_type)
		VALUES (?,?,?,?,?,?)`,
		name, nullStr(r.FormValue("phone")), nullStr(r.FormValue("contact_person")),
		nullStr(r.FormValue("address")), nullStr(r.FormValue("notes")), vtype)
	if err != nil {
		fail(w, "新增供應商失敗")
		return
	}
	redirectMsg(w, r, "/suppliers", "供應商已新增")
}

// EditSupplierForm 編輯供應商表單。
func EditSupplierForm(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))
	var s model.Supplier
	err := db.DB.QueryRow(`SELECT id, name, COALESCE(phone,''), COALESCE(contact_person,''),
		COALESCE(address,''), COALESCE(notes,''), COALESCE(vendor_type,'material'), COALESCE(active,1)
		FROM suppliers WHERE id = ?`, id).Scan(
		&s.ID, &s.Name, &s.Phone, &s.ContactPerson, &s.Address, &s.Notes, &s.VendorType, &s.Active)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	body := `
    <div class="card">
      <h2>編輯供應商 ` + htmlEsc(s.Name) + `</h2>
      <form class="frm" method="POST" action="/suppliers/` + strconv.FormatInt(s.ID, 10) + `">
        <label>名稱<input name="name" value="` + htmlEsc(s.Name) + `" required></label>
        <label>電話<input name="phone" value="` + htmlEsc(s.Phone) + `"></label>
        <label>聯絡人<input name="contact_person" value="` + htmlEsc(s.ContactPerson) + `"></label>
        <label>地址<input name="address" value="` + htmlEsc(s.Address) + `"></label>
        <label>類型<select name="vendor_type">
          <option value="material"` + selIf(s.VendorType == "material") + `>材料商</option>
          <option value="vendor"` + selIf(s.VendorType == "vendor") + `>協力廠商</option>
        </select></label>
        <label class="full">備註<textarea name="notes">` + htmlEsc(s.Notes) + `</textarea></label>
        <div class="full">
          <button class="btn btn-p" type="submit">儲存</button>
          <a class="btn btn-s" href="/suppliers">取消</a>
        </div>
      </form>
    </div>`
	renderPage(w, "編輯供應商", "/suppliers", user.Username, user.Role, body)
}

// UpdateSupplier 更新供應商。
func UpdateSupplier(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		fail(w, "名稱為必填")
		return
	}
	vtype := strings.TrimSpace(r.FormValue("vendor_type"))
	if vtype != "material" && vtype != "vendor" {
		vtype = "material"
	}
	_, err := db.DB.Exec(`UPDATE suppliers SET name=?, phone=?, contact_person=?, address=?, notes=?, vendor_type=? WHERE id=?`,
		name, nullStr(r.FormValue("phone")), nullStr(r.FormValue("contact_person")),
		nullStr(r.FormValue("address")), nullStr(r.FormValue("notes")), vtype, id)
	if err != nil {
		fail(w, "更新失敗")
		return
	}
	redirectMsg(w, r, "/suppliers", "供應商已更新")
}

// DeactivateSupplier 停用供應商（軟刪除）。
func DeactivateSupplier(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	db.DB.Exec(`UPDATE suppliers SET active = 0 WHERE id = ?`, id)
	redirectMsg(w, r, "/suppliers", "供應商已停用")
}

// ReactivateSupplier 復用供應商。
func ReactivateSupplier(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	db.DB.Exec(`UPDATE suppliers SET active = 1 WHERE id = ?`, id)
	redirectMsg(w, r, "/suppliers", "供應商已復用")
}
