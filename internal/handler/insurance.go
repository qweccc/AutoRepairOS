package handler

import (
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// 保險公司主檔 CRUD。
//
// 參考 CAR35 的 Company 表：
//   Carp78 — 廠商/公司主檔（Address / Comm / Fax / No / Short / Tax / Telephone）
//   Carp92 — 公司別帳務彙總
//   Carp32 — 完整估價單含保險公司
//   Carp77 — 保險公司車輛查詢
//
// 設計重點：
//   - code（簡稱）唯一，對應 CAR35 的 Company.Short；櫃檯開單時打簡稱最快
//   - claim_phone 獨立於 phone：保險理賠有專屬客服電話，車行打報案用
//   - 「刪除」是停用（active=0）而非真刪：歷史工單會引用保險公司，
//     真刪會讓舊工單的保險公司資訊消失

const insuranceCols = `ic.id, ic.code, ic.name, COALESCE(ic.tax_id,''),
	COALESCE(ic.claim_phone,''), COALESCE(ic.phone,''), COALESCE(ic.contact_person,''),
	COALESCE(ic.address,''), COALESCE(ic.postal_code,''), COALESCE(ic.notes,''), ic.active`

type insuranceCompany struct {
	ID            int64
	Code          string
	Name          string
	TaxID         string
	ClaimPhone    string
	Phone         string
	ContactPerson string
	Address       string
	PostalCode    string
	Notes         string
	Active        int
}

func scanInsuranceCompany(rows interface{ Scan(...interface{}) error }) (insuranceCompany, error) {
	var ic insuranceCompany
	err := rows.Scan(&ic.ID, &ic.Code, &ic.Name, &ic.TaxID, &ic.ClaimPhone, &ic.Phone,
		&ic.ContactPerson, &ic.Address, &ic.PostalCode, &ic.Notes, &ic.Active)
	return ic, err
}

// ListInsuranceCompanies 保險公司列表 + 新增表單。
func ListInsuranceCompanies(w http.ResponseWriter, r *http.Request, user model.User) {
	qv := r.URL.Query()
	q := strings.TrimSpace(qv.Get("q"))
	page := pageNo(r)
	size := pageSize(r)

	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (ic.code LIKE ? OR ic.name LIKE ? OR COALESCE(ic.tax_id,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}

	total := countRows(`SELECT COUNT(1) FROM insurance_companies ic`+where, args...)

	// 停用的排最後，不要消失在清單裡（要查歷史案件的保險公司時還找得到）
	query := `SELECT ` + insuranceCols + ` FROM insurance_companies ic` + where +
		` ORDER BY ic.active DESC, ic.code LIMIT ? OFFSET ?`
	pageArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := db.DB.Query(query, pageArgs...)
	if err != nil {
		fail(w, "查詢保險公司失敗")
		return
	}
	defer rows.Close()

	var list []insuranceCompany
	for rows.Next() {
		ic, err := scanInsuranceCompany(rows)
		if err != nil {
			continue
		}
		list = append(list, ic)
	}

	var tb strings.Builder
	tb.WriteString(`<table><tr><th>簡稱</th><th>公司名稱</th><th>統編</th><th>客服電話</th><th>聯絡人</th><th>狀態</th><th>操作</th></tr>`)
	for _, ic := range list {
		status := `<span class="pill p-completed">啟用</span>`
		ops := `<a class="btn btn-s" href="/insurance-companies/` +
			strconv.FormatInt(ic.ID, 10) + `/edit">編輯</a> ` +
			`<a class="btn btn-d" href="/insurance-companies/` +
			strconv.FormatInt(ic.ID, 10) + `/delete" onclick="return confirm('確定停用這家保險公司？停用後不會出現在工單選單中，但歷史工單仍會保留。')">停用</a>`
		if ic.Active == 0 {
			status = `<span class="pill p-cancelled">停用</span>`
			ops = `<a class="btn btn-s" href="/insurance-companies/` +
				strconv.FormatInt(ic.ID, 10) + `/edit">編輯</a>`
		}
		tb.WriteString(`<tr><td><b>` + htmlEsc(ic.Code) + `</b></td><td>` + htmlEsc(ic.Name) +
			`</td><td>` + htmlEsc(ic.TaxID) + `</td><td>` + htmlEsc(ic.ClaimPhone) +
			`</td><td>` + htmlEsc(ic.ContactPerson) + `</td><td>` + status +
			`</td><td>` + ops + `</td></tr>`)
	}
	tb.WriteString(`</table>`)

	table := tb.String()
	if len(list) == 0 {
		table = `<div class="empty">沒有保險公司資料</div>`
	}

	body := flash(r) + `
    <div class="card">
      <h2>新增保險公司</h2>
      <form class="frm" method="POST" action="/insurance-companies">
        <label>簡稱<input name="code" placeholder="如：國泰" required></label>
        <label>公司名稱<input name="name" required></label>
        <label>統一編號<input name="tax_id"></label>
        <label>客服電話<input name="claim_phone" placeholder="打報案用"></label>
        <label>聯絡人<input name="contact_person"></label>
        <label>地址<input name="address"></label>
        <label>郵遞區號<input name="postal_code"></label>
        <label>備註<textarea name="notes"></textarea></label>
        <div class="full"><button class="btn btn-p" type="submit">新增</button></div>
      </form>
    </div>
    <div class="card">
      <h2>保險公司（符合條件 ` + strconv.Itoa(total) + ` 家）</h2>
      <p style="color:#64748b;font-size:12.5px;margin:0 0 14px">
        保險理賠案件需在此維護對應保險公司，工單開立時才能選擇。
      </p>
      <form method="GET" action="/insurance-companies" style="margin-bottom:12px">
        <input name="q" value="` + htmlEsc(q) + `" placeholder="搜尋簡稱 / 公司名稱 / 統編"
               style="padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;width:300px">
        <button class="btn btn-p" type="submit">搜尋</button>
        <a class="btn btn-s" href="/insurance-companies">清除</a>
      </form>` + table + pager("/insurance-companies", qv, total, page, size) +
		`<div style="margin-top:6px">` + perPageSel("/insurance-companies", qv, size) + `</div>
    </div>`

	renderPage(w, "保險公司", "/insurance-companies", user.Username, user.Role, body)
}

// CreateInsuranceCompany 新增保險公司。
func CreateInsuranceCompany(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.FormValue("code"))
	name := strings.TrimSpace(r.FormValue("name"))

	if code == "" || name == "" {
		fail(w, "簡稱與公司名稱為必填")
		return
	}

	_, err := db.DB.Exec(`INSERT INTO insurance_companies
		(code, name, tax_id, claim_phone, phone, contact_person, address, postal_code, notes)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		code, name,
		nullStr(r.FormValue("tax_id")), nullStr(r.FormValue("claim_phone")),
		nullStr(r.FormValue("phone")), nullStr(r.FormValue("contact_person")),
		nullStr(r.FormValue("address")), nullStr(r.FormValue("postal_code")),
		nullStr(r.FormValue("notes")))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			fail(w, "簡稱「"+code+"」已存在，請用其他簡稱")
			return
		}
		fail(w, "新增保險公司失敗")
		return
	}

	redirectMsg(w, r, "/insurance-companies", "保險公司已新增: "+code)
}

// EditInsuranceCompanyForm 編輯頁。
func EditInsuranceCompanyForm(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))

	ic, err := scanInsuranceCompany(db.DB.QueryRow(
		`SELECT `+insuranceCols+` FROM insurance_companies ic WHERE ic.id = ?`, id))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	body := `
    <div class="card">
      <h2>編輯保險公司 ` + htmlEsc(ic.Code) + `</h2>
      <form class="frm" method="POST" action="/insurance-companies/` + strconv.FormatInt(ic.ID, 10) + `">
        <label>簡稱<input name="code" value="` + htmlEsc(ic.Code) + `" required></label>
        <label>公司名稱<input name="name" value="` + htmlEsc(ic.Name) + `" required></label>
        <label>統一編號<input name="tax_id" value="` + htmlEsc(ic.TaxID) + `"></label>
        <label>客服電話<input name="claim_phone" value="` + htmlEsc(ic.ClaimPhone) + `"></label>
        <label>一般電話<input name="phone" value="` + htmlEsc(ic.Phone) + `"></label>
        <label>聯絡人<input name="contact_person" value="` + htmlEsc(ic.ContactPerson) + `"></label>
        <label>郵遞區號<input name="postal_code" value="` + htmlEsc(ic.PostalCode) + `"></label>
        <label>地址<input name="address" value="` + htmlEsc(ic.Address) + `"></label>
        <label class="full">備註<textarea name="notes">` + htmlEsc(ic.Notes) + `</textarea></label>
        <div class="full">
          <button class="btn btn-p" type="submit">儲存</button>
          <a class="btn btn-s" href="/insurance-companies">取消</a>
        </div>
      </form>
    </div>`

	renderPage(w, "編輯保險公司", "/insurance-companies", user.Username, user.Role, body)
}

// UpdateInsuranceCompany 更新保險公司。
func UpdateInsuranceCompany(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	code := strings.TrimSpace(r.FormValue("code"))
	name := strings.TrimSpace(r.FormValue("name"))
	if code == "" || name == "" {
		fail(w, "簡稱與公司名稱為必填")
		return
	}

	res, err := db.DB.Exec(`UPDATE insurance_companies SET
		code = ?, name = ?, tax_id = ?, claim_phone = ?, phone = ?,
		contact_person = ?, address = ?, postal_code = ?, notes = ?
		WHERE id = ?`,
		code, name,
		nullStr(r.FormValue("tax_id")), nullStr(r.FormValue("claim_phone")),
		nullStr(r.FormValue("phone")), nullStr(r.FormValue("contact_person")),
		nullStr(r.FormValue("address")), nullStr(r.FormValue("postal_code")),
		nullStr(r.FormValue("notes")), id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			fail(w, "簡稱「"+code+"」已被其他保險公司使用")
			return
		}
		fail(w, "更新失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		fail(w, "找不到這家保險公司")
		return
	}

	redirectMsg(w, r, "/insurance-companies", "保險公司已更新")
}

// DeactivateInsuranceCompany 停用保險公司（軟刪除）。
//
// 不真刪：歷史工單透過 insurance_company_id 引用本表，
// 真刪會讓舊工單的保險公司資訊變成空白，報表對帳會出錯。
func DeactivateInsuranceCompany(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	// 先檢查是否仍有進行中的工單引用
	var activeWO int
	db.DB.QueryRow(`SELECT COUNT(1) FROM work_orders
		WHERE insurance_company_id = ? AND status NOT IN ('paid')`, id).Scan(&activeWO)
	if activeWO > 0 {
		fail(w, "仍有 "+strconv.Itoa(activeWO)+" 張未結帳工單使用這家保險公司，無法停用")
		return
	}

	res, err := db.DB.Exec(`UPDATE insurance_companies SET active = 0 WHERE id = ?`, id)
	if err != nil {
		fail(w, "停用失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		fail(w, "找不到這家保險公司")
		return
	}

	redirectMsg(w, r, "/insurance-companies", "保險公司已停用")
}

// insuranceOptionsSel 保險公司下拉選單，只列啟用的。
// selected > 0 時預選該公司（工單帶入保險時用）。
func insuranceOptionsSel(selected int64) string {
	rows, err := db.DB.Query(`SELECT id, code, name FROM insurance_companies
		WHERE active = 1 ORDER BY code`)
	if err != nil {
		return `<option value="">— 無法載入保險公司 —</option>`
	}
	defer rows.Close()

	var sb strings.Builder
	sb.WriteString(`<option value="">— 不指定 —</option>`)
	for rows.Next() {
		var id int64
		var code, name string
		if rows.Scan(&id, &code, &name) != nil {
			continue
		}
		sb.WriteString(`<option value="` + strconv.FormatInt(id, 10) + `"` +
			selIf(id == selected) + `>` + htmlEsc(code) + " / " + htmlEsc(name) + `</option>`)
	}
	return sb.String()
}