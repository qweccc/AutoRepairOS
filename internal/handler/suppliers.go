package handler

import (
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
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
	COALESCE(s.payment_terms,''),
	COALESCE(s.active,1), s.created_at`

func scanSupplier(rows interface{ Scan(...interface{}) error }) (model.Supplier, error) {
	var s model.Supplier
	var createdAt string
	err := rows.Scan(&s.ID, &s.Name, &s.Phone, &s.ContactPerson, &s.Address,
		&s.Notes, &s.VendorType, &s.PaymentTerms, &s.Active, &createdAt)
	s.CreatedAt = parseFlexTime(createdAt)
	return s, err
}

var vendorTypeLabel = map[string]string{
	"material": "材料商",
	"vendor":   "協力廠商",
	"other":    "其他",
}

// 付款條件（FR-SUP-02）：現結 / 月結 / 期票，供 AP 模組引用。
// 存值是小寫代碼，畫面顯示中文，報表依代碼分組才不會因打字差異分家。
var paymentTermsList = []struct{ code, label string }{
	{"cash", "現結"},
	{"monthly", "月結"},
	{"check30", "30 天期票"},
	{"check60", "60 天期票"},
}

func normPaymentTerms(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	for _, p := range paymentTermsList {
		if p.code == v {
			return p.code
		}
	}
	return ""
}

func paymentTermsLabel(v string) string {
	for _, p := range paymentTermsList {
		if p.code == v {
			return p.label
		}
	}
	return ""
}

func paymentTermsOptions(selected string) string {
	var b strings.Builder
	b.WriteString(`<option value="">未設定</option>`)
	for _, p := range paymentTermsList {
		sel := ""
		if p.code == selected {
			sel = " selected"
		}
		b.WriteString(`<option value="` + p.code + `"` + sel + `>` + p.label + `</option>`)
	}
	return b.String()
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

	var supRows []pages.SupplierRow
	for _, s := range list {
		supRows = append(supRows, pages.SupplierRow{
			ID:            s.ID,
			Name:          s.Name,
			VendorType:    s.VendorType,
			VendorLabel:   vendorTypeLabel[s.VendorType],
			PaymentTerms:  paymentTermsLabel(s.PaymentTerms),
			Phone:         s.Phone,
			ContactPerson: s.ContactPerson,
			Address:       s.Address,
			Notes:         s.Notes,
			Active:        s.Active,
		})
	}

	matCount := countRows(`SELECT COUNT(1) FROM suppliers WHERE COALESCE(vendor_type,'material')='material'`)
	venCount := countRows(`SELECT COUNT(1) FROM suppliers WHERE vendor_type='vendor'`)

	pages.ListSuppliersPage(supRows, total, page, size, q, vtype, matCount, venCount,
		user.Role == "owner" || user.Role == "counter").Render(r.Context(), w)
}

// SearchSuppliersAPI 廠商即時搜尋 API（HTMX 用）。
func SearchSuppliersAPI(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	vtype := strings.TrimSpace(r.URL.Query().Get("type"))
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
	rows, err := db.DB.Query(`SELECT `+supplierCols+` FROM suppliers s`+where+
		` ORDER BY s.vendor_type, s.name LIMIT 50`, args...)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var result []pages.SupplierRow
	for rows.Next() {
		s, err := scanSupplier(rows)
		if err != nil {
			continue
		}
		result = append(result, pages.SupplierRow{
			ID:            s.ID,
			Name:          s.Name,
			VendorType:    s.VendorType,
			VendorLabel:   vendorTypeLabel[s.VendorType],
			PaymentTerms:  paymentTermsLabel(s.PaymentTerms),
			Phone:         s.Phone,
			ContactPerson: s.ContactPerson,
			Address:       s.Address,
			Notes:         s.Notes,
			Active:        s.Active,
		})
	}
	pages.SupplierSearchRows(result).Render(r.Context(), w)
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
	_, err := db.DB.Exec(`INSERT INTO suppliers (name, phone, contact_person, address, notes, vendor_type, payment_terms)
		VALUES (?,?,?,?,?,?,?)`,
		name, nullStr(r.FormValue("phone")), nullStr(r.FormValue("contact_person")),
		nullStr(r.FormValue("address")), nullStr(r.FormValue("notes")), vtype,
		normPaymentTerms(r.FormValue("payment_terms")))
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
		COALESCE(address,''), COALESCE(notes,''), COALESCE(vendor_type,'material'),
		COALESCE(payment_terms,''), COALESCE(active,1)
		FROM suppliers WHERE id = ?`, id).Scan(
		&s.ID, &s.Name, &s.Phone, &s.ContactPerson, &s.Address, &s.Notes, &s.VendorType,
		&s.PaymentTerms, &s.Active)
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
        <label>付款條件<select name="payment_terms">` + paymentTermsOptions(s.PaymentTerms) + `</select></label>
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
	_, err := db.DB.Exec(`UPDATE suppliers SET name=?, phone=?, contact_person=?, address=?, notes=?, vendor_type=?, payment_terms=? WHERE id=?`,
		name, nullStr(r.FormValue("phone")), nullStr(r.FormValue("contact_person")),
		nullStr(r.FormValue("address")), nullStr(r.FormValue("notes")), vtype,
		normPaymentTerms(r.FormValue("payment_terms")), id)
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
