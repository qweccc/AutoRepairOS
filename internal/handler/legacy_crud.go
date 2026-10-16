// Package handler 提供 AutoRepairOS 的 HTTP 端點與頁面渲染。
package handler

import (
	"bytes"
	"net/http"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

// ============================================================
// 舊系統補齊表的 CRUD（投保紀錄 / 定檢排程 / 保養套餐）
//
// 這三張表原本只有唯讀列表（遷移進來的歷史資料只能看），導致：
//   - 車主換新保單，櫃檯無法在系統登錄，只能翻舊紙本
//   - 排定新的驗車日無處可記
//   - 想新增一個自訂保養套餐（本店標準工項）也做不到
// 這裡補上新增 / 編輯 / 刪除，讓它們成為可維護的主資料而非死資料。
//
// 與主流程表（客戶/工單/庫存）的差異：
//   - 投保與定檢屬「附掛在車輛上」的紀錄，允許刪除（誤植可直接移除），
//     但仍保留確認對話框——舊資料有 2,667 / 21,176 筆，誤刪代價高。
//   - 保養套餐的 code 是舊系統編號且 UNIQUE，編輯時沿用原 code，
//     不可改（改了會與舊工單的參照斷鏈）。
// ============================================================

// ============ 車輛投保紀錄（vehicle_insurances）============

const vehInsuranceCols = `vi.id, vi.vehicle_id, COALESCE(vi.insurance_company_id,0),
	COALESCE(ic.name, vi.company_no, ''), COALESCE(vi.company_no,''),
	COALESCE(vi.insurance_class,''), COALESCE(vi.insurance_class_name,''),
	COALESCE(vi.license_date,''), COALESCE(vi.card_no,''), COALESCE(vi.amount_raw,''),
	COALESCE(vi.notes,''), COALESCE(vi.policy_number,''), COALESCE(vi.expiry_date,'')`

const vehInsuranceFrom = ` FROM active_vehicle_insurances vi
	JOIN active_vehicles v ON v.id = vi.vehicle_id
	JOIN active_customers c ON c.id = v.customer_id
	LEFT JOIN insurance_companies ic ON ic.id = vi.insurance_company_id `

type insuranceRow struct {
	ID           int64
	VehicleID    int64
	CompanyID    int64
	CompanyName  string
	CompanyNo    string
	Class        string
	ClassName    string
	LicenseDate  string
	CardNo       string
	AmountRaw    string
	Notes        string
	PolicyNumber string
	ExpiryDate   string
}

func scanInsuranceRow(rows interface{ Scan(...interface{}) error }) (insuranceRow, error) {
	var r insuranceRow
	err := rows.Scan(&r.ID, &r.VehicleID, &r.CompanyID, &r.CompanyName, &r.CompanyNo,
		&r.Class, &r.ClassName, &r.LicenseDate, &r.CardNo, &r.AmountRaw, &r.Notes,
		&r.PolicyNumber, &r.ExpiryDate)
	return r, err
}

// NewVehicleInsuranceForm 新增投保紀錄表單。
func NewVehicleInsuranceForm(w http.ResponseWriter, r *http.Request, user model.User) {
	// 可從車輛頁帶入 ?vehicle=<id>，省去重選車輛。
	vehID := mustInt(r.URL.Query().Get("vehicle"))
	data := pages.VehicleInsuranceFormData{
		Title: "新增車輛投保紀錄", Action: "/insurances", VehicleID: vehID,
		VehicleOptions: vehicleOptionsFor(vehID), CompanyOptions: insuranceCompanyOptions(0),
	}
	renderVehicleInsuranceForm(w, r, user, data)
}

// CreateVehicleInsurance 建立投保紀錄。
func CreateVehicleInsurance(w http.ResponseWriter, r *http.Request) {
	vehID := mustInt(r.FormValue("vehicle_id"))
	if vehID == 0 || countRows("SELECT COUNT(1) FROM active_vehicles WHERE id = ?", vehID) == 0 {
		fail(w, "請選擇有效的車輛")
		return
	}
	compID := mustInt(r.FormValue("insurance_company_id"))
	if compID > 0 && countRows("SELECT COUNT(1) FROM insurance_companies WHERE id = ?", compID) == 0 {
		compID = 0
	}
	expiryDate, err := insuranceExpiryDate(r.FormValue("expiry_date"))
	if err != nil {
		fail(w, "保單有效期限格式錯誤")
		return
	}

	_, err = db.DB.Exec(`INSERT INTO vehicle_insurances
		(vehicle_id, insurance_company_id, company_no, insurance_class, insurance_class_name,
		 license_date, card_no, amount_raw, notes, policy_number, expiry_date)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		vehID, zeroToNil(compID), nullStr(r.FormValue("company_no")),
		nullStr(r.FormValue("insurance_class")), nullStr(r.FormValue("insurance_class_name")),
		normDateOrEmpty(r.FormValue("license_date")), nullStr(r.FormValue("card_no")),
		nullStr(r.FormValue("amount_raw")), nullStr(r.FormValue("notes")),
		strings.TrimSpace(r.FormValue("policy_number")), expiryDate)
	if err != nil {
		fail(w, "建立投保紀錄失敗")
		return
	}
	redirectMsg(w, r, "/insurances", "投保紀錄已建立")
}

// EditVehicleInsuranceForm 編輯投保紀錄。
func EditVehicleInsuranceForm(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))
	row := db.DB.QueryRow(`SELECT `+vehInsuranceCols+vehInsuranceFrom+` WHERE vi.id = ?`, id)
	ir, err := scanInsuranceRow(row)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	data := pages.VehicleInsuranceFormData{
		ID: ir.ID, Title: "編輯投保紀錄 #" + strconv.FormatInt(ir.ID, 10),
		Action:    "/insurances/" + strconv.FormatInt(ir.ID, 10),
		CompanyID: ir.CompanyID, CompanyOptions: insuranceCompanyOptions(ir.CompanyID),
		CompanyNo: ir.CompanyNo, Class: ir.Class, ClassName: ir.ClassName,
		LicenseDate: normDate(ir.LicenseDate), PolicyNumber: ir.PolicyNumber,
		ExpiryDate: normDate(ir.ExpiryDate), CardNo: ir.CardNo, AmountRaw: ir.AmountRaw, Notes: ir.Notes,
	}
	renderVehicleInsuranceForm(w, r, user, data)
}

func renderVehicleInsuranceForm(w http.ResponseWriter, r *http.Request, user model.User, data pages.VehicleInsuranceFormData) {
	var body bytes.Buffer
	if err := pages.VehicleInsuranceForm(data).Render(r.Context(), &body); err != nil {
		http.Error(w, "投保表單產生失敗", http.StatusInternalServerError)
		return
	}
	renderPage(w, data.Title, "/insurances", user.Username, user.Role, body.String())
}

// UpdateVehicleInsurance 更新投保紀錄。
// 車輛不可改：投保紀錄綁定在特定車輛上，換車應另建一筆（保留舊車的投保歷史）。
func UpdateVehicleInsurance(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	compID := mustInt(r.FormValue("insurance_company_id"))
	if compID > 0 && countRows("SELECT COUNT(1) FROM insurance_companies WHERE id = ?", compID) == 0 {
		compID = 0
	}
	expiryDate, err := insuranceExpiryDate(r.FormValue("expiry_date"))
	if err != nil {
		fail(w, "保單有效期限格式錯誤")
		return
	}

	res, err := db.DB.Exec(`UPDATE vehicle_insurances SET insurance_company_id=?, company_no=?,
		insurance_class=?, insurance_class_name=?, license_date=?, card_no=?, amount_raw=?, notes=?,
		policy_number=?, expiry_date=?
		WHERE id=?`,
		zeroToNil(compID), nullStr(r.FormValue("company_no")),
		nullStr(r.FormValue("insurance_class")), nullStr(r.FormValue("insurance_class_name")),
		normDateOrEmpty(r.FormValue("license_date")), nullStr(r.FormValue("card_no")),
		nullStr(r.FormValue("amount_raw")), nullStr(r.FormValue("notes")),
		strings.TrimSpace(r.FormValue("policy_number")), expiryDate, id)
	if err != nil {
		fail(w, "更新失敗")
		return
	}

	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	redirectMsg(w, r, "/insurances", "投保紀錄已更新")
}

func insuranceExpiryDate(value string) (string, error) {
	date := normDateOrEmpty(value)
	if date == "" {
		return "", nil
	}
	_, err := time.Parse("2006-01-02", date)
	return date, err
}

// DeleteVehicleInsurance 刪除投保紀錄。
func DeleteVehicleInsurance(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	res, err := db.DB.Exec("UPDATE vehicle_insurances SET deleted_at = datetime('now') WHERE id = ? AND deleted_at IS NULL", id)
	if err != nil {
		fail(w, "刪除失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	redirectMsg(w, r, "/insurances", "投保紀錄已刪除")
}

// ============ 車輛定檢排程（vehicle_inspections）============

const vehInspectionCols = `vi.id, vi.vehicle_id, COALESCE(vi.order_date,''),
	COALESCE(vi.check_date,''), COALESCE(vi.firm_no,''), COALESCE(vi.notes,'')`

const vehInspectionFrom = ` FROM active_vehicle_inspections vi
	JOIN active_vehicles v ON v.id = vi.vehicle_id `

type inspectionRow struct {
	ID        int64
	VehicleID int64
	OrderDate string
	CheckDate string
	FirmNo    string
	Notes     string
}

func scanInspectionRow(rows interface{ Scan(...interface{}) error }) (inspectionRow, error) {
	var r inspectionRow
	err := rows.Scan(&r.ID, &r.VehicleID, &r.OrderDate, &r.CheckDate, &r.FirmNo, &r.Notes)
	return r, err
}

// NewVehicleInspectionForm 新增定檢排程。
func NewVehicleInspectionForm(w http.ResponseWriter, r *http.Request, user model.User) {
	vehID := mustInt(r.URL.Query().Get("vehicle"))
	body := `
    <div class="card">
      <h2>新增車輛定檢排程</h2>
      <form class="frm" method="POST" action="/inspections">
        <label>車輛<select name="vehicle_id" required>` + vehicleOptionsFor(vehID) + `</select></label>
        <label>排定檢驗日<input type="date" name="order_date" required></label>
        <label>實際完成日<input type="date" name="check_date"></label>
        <label>代檢廠編號<input name="firm_no"></label>
        <label class="full">備註<textarea name="notes"></textarea></label>
        <div class="full">
          <button class="btn btn-p" type="submit">建立</button>
          <a class="btn btn-s" href="/inspections">取消</a>
        </div>
      </form>
      <p style="color:#64748b;font-size:12.5px;margin-top:10px">
        完成日留空代表「已排定但未驗」；填上完成日即視為已結案。
      </p>
    </div>`

	renderPage(w, "新增定檢排程", "/inspections", user.Username, user.Role, body)
}

// CreateVehicleInspection 建立定檢排程。
func CreateVehicleInspection(w http.ResponseWriter, r *http.Request) {
	vehID := mustInt(r.FormValue("vehicle_id"))
	if vehID == 0 || countRows("SELECT COUNT(1) FROM active_vehicles WHERE id = ?", vehID) == 0 {
		fail(w, "請選擇有效的車輛")
		return
	}
	orderDate := normDateOrEmpty(r.FormValue("order_date"))
	if orderDate == "" {
		fail(w, "排定檢驗日為必填")
		return
	}

	_, err := db.DB.Exec(`INSERT INTO vehicle_inspections
		(vehicle_id, order_date, check_date, firm_no, notes) VALUES (?,?,?,?,?)`,
		vehID, orderDate, nullStr(normDateOrEmpty(r.FormValue("check_date"))),
		nullStr(r.FormValue("firm_no")), nullStr(r.FormValue("notes")))
	if err != nil {
		fail(w, "建立定檢排程失敗")
		return
	}
	redirectMsg(w, r, "/inspections", "定檢排程已建立")
}

// EditVehicleInspectionForm 編輯定檢排程。
func EditVehicleInspectionForm(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))
	row := db.DB.QueryRow(`SELECT `+vehInspectionCols+vehInspectionFrom+` WHERE vi.id = ?`, id)
	ir, err := scanInspectionRow(row)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	body := `
    <div class="card">
      <h2>編輯定檢排程 #` + strconv.FormatInt(ir.ID, 10) + `</h2>
      <form class="frm" method="POST" action="/inspections/` + strconv.FormatInt(ir.ID, 10) + `">
        <label>排定檢驗日<input type="date" name="order_date" value="` + htmlEsc(normDate(ir.OrderDate)) + `" required></label>
        <label>實際完成日<input type="date" name="check_date" value="` + htmlEsc(normDate(ir.CheckDate)) + `"></label>
        <label>代檢廠編號<input name="firm_no" value="` + htmlEsc(ir.FirmNo) + `"></label>
        <label class="full">備註<textarea name="notes">` + htmlEsc(ir.Notes) + `</textarea></label>
        <div class="full">
          <button class="btn btn-p" type="submit">儲存</button>
          <a class="btn btn-s" href="/inspections">取消</a>
        </div>
      </form>
    </div>`

	renderPage(w, "編輯定檢排程", "/inspections", user.Username, user.Role, body)
}

// UpdateVehicleInspection 更新定檢排程。
func UpdateVehicleInspection(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	orderDate := normDateOrEmpty(r.FormValue("order_date"))
	if orderDate == "" {
		fail(w, "排定檢驗日為必填")
		return
	}

	res, err := db.DB.Exec(`UPDATE vehicle_inspections SET order_date=?, check_date=?, firm_no=?, notes=?
		WHERE id=?`,
		orderDate, nullStr(normDateOrEmpty(r.FormValue("check_date"))),
		nullStr(r.FormValue("firm_no")), nullStr(r.FormValue("notes")), id)
	if err != nil {
		fail(w, "更新失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	redirectMsg(w, r, "/inspections", "定檢排程已更新")
}

// DeleteVehicleInspection 刪除定檢排程。
func DeleteVehicleInspection(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	res, err := db.DB.Exec("UPDATE vehicle_inspections SET deleted_at = datetime('now') WHERE id = ? AND deleted_at IS NULL", id)
	if err != nil {
		fail(w, "刪除失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	redirectMsg(w, r, "/inspections", "定檢排程已刪除")
}

// ============ 共用小工具 ============

// vehicleOptionsFor 車輛下拉，選到的項目帶 selected。
// 與 vehicleOptions() 的差別只在是否要預選——避免改動既有的函式。
func vehicleOptionsFor(selected int64) string {
	rows, err := db.DB.Query(`SELECT v.id, COALESCE(v.plate_number,''), COALESCE(v.brand_model,''),
		COALESCE(c.name,'')
		FROM active_vehicles v JOIN active_customers c ON c.id = v.customer_id
		ORDER BY v.plate_number`)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var id int64
		var plate, model, cust string
		if rows.Scan(&id, &plate, &model, &cust) != nil {
			continue
		}
		label := plate + "（" + cust
		if model != "" {
			label += " / " + model
		}
		label += "）"
		b.WriteString(`<option value="` + strconv.FormatInt(id, 10) + `"` +
			selIf(id == selected) + `>` + htmlEsc(label) + `</option>`)
	}
	return b.String()
}

// insuranceCompanyOptions 保險公司下拉（只列啟用中的）。
func insuranceCompanyOptions(selected int64) string {
	rows, err := db.DB.Query(`SELECT id, code, name FROM insurance_companies
		WHERE COALESCE(active,1) = 1 ORDER BY code`)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var id int64
		var code, name string
		if rows.Scan(&id, &code, &name) != nil {
			continue
		}
		b.WriteString(`<option value="` + strconv.FormatInt(id, 10) + `"` +
			selIf(id == selected) + `>` + htmlEsc(code+" "+name) + `</option>`)
	}
	return b.String()
}

// zeroToNil 把 0 轉成 nil，讓 NOT NULL 外鍵欄位在「未指定」時寫入 NULL。
func zeroToNil(n int64) interface{} {
	if n == 0 {
		return nil
	}
	return n
}

// normDateOrEmpty 正規化日期輸入；空值回空字串（不合法的值也回空，由 DB 存空）。
func normDateOrEmpty(s string) string {
	return normDate(s)
}

var _ = model.Vehicle{}
