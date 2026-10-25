package handler

import (
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
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

	var insRows []pages.InsuranceCompanyRow
	for _, ic := range list {
		insRows = append(insRows, pages.InsuranceCompanyRow{
			ID:            ic.ID,
			Code:          ic.Code,
			Name:          ic.Name,
			TaxID:         ic.TaxID,
			ClaimPhone:    ic.ClaimPhone,
			ContactPerson: ic.ContactPerson,
			Active:        ic.Active,
		})
	}

	pages.ListInsurancePage(insRows, total, page, size, q, user).Render(r.Context(), w)
}

// SearchInsuranceAPI 保險公司即時搜尋 API（HTMX 用）。
func SearchInsuranceAPI(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (ic.code LIKE ? OR ic.name LIKE ? OR COALESCE(ic.tax_id,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	rows, err := db.DB.Query(`SELECT `+insuranceCols+` FROM insurance_companies ic`+where+
		` ORDER BY ic.active DESC, ic.code LIMIT 50`, args...)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var result []pages.InsuranceCompanyRow
	for rows.Next() {
		ic, err := scanInsuranceCompany(rows)
		if err != nil {
			continue
		}
		result = append(result, pages.InsuranceCompanyRow{
			ID:            ic.ID,
			Code:          ic.Code,
			Name:          ic.Name,
			TaxID:         ic.TaxID,
			ClaimPhone:    ic.ClaimPhone,
			ContactPerson: ic.ContactPerson,
			Active:        ic.Active,
		})
	}
	pages.InsuranceSearchRows(result).Render(r.Context(), w)
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

	pages.EditInsuranceCompanyPage(pages.InsuranceCompanyEditData{
		ID: ic.ID, Code: ic.Code, Name: ic.Name, TaxID: ic.TaxID,
		ClaimPhone: ic.ClaimPhone, Phone: ic.Phone, ContactPerson: ic.ContactPerson,
		Address: ic.Address, PostalCode: ic.PostalCode, Notes: ic.Notes, User: user,
	}).Render(r.Context(), w)
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
