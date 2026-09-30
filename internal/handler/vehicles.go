package handler

import (
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

const vehicleCols = `v.id, v.customer_id, COALESCE(c.name,''), v.plate_number, v.brand_model,
	COALESCE(v.manufacture_year,0), COALESCE(v.vin,''), COALESCE(v.engine_number,''),
	COALESCE(v.inspection_due_date,''), COALESCE(v.next_maintenance_date,''),
	COALESCE(v.next_maintenance_mileage,0)`

func scanVehicle(rows interface{ Scan(...interface{}) error }) (model.Vehicle, error) {
	var v model.Vehicle
	err := rows.Scan(&v.ID, &v.CustomerID, &v.CustomerName, &v.PlateNumber, &v.BrandModel,
		&v.ManufactureYear, &v.VIN, &v.EngineNumber, &v.InspectionDueDate,
		&v.NextMaintenanceDate, &v.NextMaintenanceMileage)
	return v, err
}

func ListVehicles(w http.ResponseWriter, r *http.Request, user model.User) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	filterCustomer := mustInt(r.URL.Query().Get("customer"))

	query := `SELECT ` + vehicleCols + ` FROM vehicles v
		JOIN customers c ON c.id = v.customer_id WHERE 1=1`
	var args []interface{}
	if q != "" {
		query += ` AND (v.plate_number LIKE ? OR v.brand_model LIKE ? OR c.name LIKE ? OR COALESCE(v.vin,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like, like)
	}
	if filterCustomer > 0 {
		query += ` AND v.customer_id = ?`
		args = append(args, filterCustomer)
	}
	query += ` ORDER BY v.id DESC`

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		fail(w, "查詢車輛失敗")
		return
	}
	defer rows.Close()

	var list []model.Vehicle
	for rows.Next() {
		v, err := scanVehicle(rows)
		if err != nil {
			continue
		}
		list = append(list, v)
	}

	var tb strings.Builder
	tb.WriteString(`<table><tr><th>ID</th><th>車牌</th><th>車型</th><th>年份</th><th>VIN</th><th>業主</th><th>下次保養</th><th>操作</th></tr>`)
	for _, v := range list {
		nextMaint := dateText(v.NextMaintenanceDate)
		if v.NextMaintenanceMileage > 0 {
			nextMaint += " / " + strconv.Itoa(v.NextMaintenanceMileage) + "km"
		}
		tb.WriteString(`<tr><td>` + strconv.FormatInt(v.ID, 10) + `</td><td><b>` + htmlEsc(v.PlateNumber) + `</b></td><td>` +
			htmlEsc(v.BrandModel) + `</td><td>` + yearText(v.ManufactureYear) + `</td><td>` + htmlEsc(v.VIN) + `</td><td>` +
			htmlEsc(v.CustomerName) + `</td><td>` + htmlEsc(nextMaint) + `</td><td>` +
			`<a class="btn btn-p" href="/work-orders/new?vehicle=` + strconv.FormatInt(v.ID, 10) + `">開工單</a> ` +
			`<a class="btn btn-s" href="/vehicles/` + strconv.FormatInt(v.ID, 10) + `/edit">編輯</a> ` +
			`<a class="btn btn-d" href="/vehicles/` + strconv.FormatInt(v.ID, 10) + `/delete" onclick="return confirm('確定刪除這台車？')">刪除</a></td></tr>`)
	}
	tb.WriteString(`</table>`)

	table := tb.String()
	if len(list) == 0 {
		table = `<div class="empty">沒有車輛資料</div>`
	}

	filterNote := ""
	if filterCustomer > 0 {
		filterNote = ` <a href="/vehicles" style="font-size:13px">（清除客戶篩選）</a>`
	}

	body := flash(r) + `
    <div class="card">
      <h2>新增車輛</h2>
      <form class="frm" method="POST" action="/vehicles">
        <label>所屬客戶<select name="customer_id" required>` + customerOptions() + `</select></label>
        <label>車牌號碼<input name="plate_number" required></label>
        <label>品牌車型<input name="brand_model" required></label>
        <label>出廠年份<input name="manufacture_year" type="number" min="1900" max="2100"></label>
        <label>VIN<input name="vin"></label>
        <label>引擎號碼<input name="engine_number"></label>
        <label>檢驗到期日<input name="inspection_due_date" type="date"></label>
        <label>下次保養日期<input name="next_maintenance_date" type="date"></label>
        <label>下次保養里程<input name="next_maintenance_mileage" type="number" min="0"></label>
        <div class="full"><button class="btn btn-p" type="submit">建立車輛</button></div>
      </form>
    </div>
    <div class="card">
      <h2>車輛列表（` + strconv.Itoa(len(list)) + ` 筆）` + filterNote + `</h2>
      <form method="GET" action="/vehicles" style="margin-bottom:12px">
        <input name="q" value="` + htmlEsc(q) + `" placeholder="搜尋車牌 / 車型 / VIN / 業主" style="padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;width:320px">
        <button class="btn btn-p" type="submit">搜尋</button>
      </form>` + table + `
    </div>`

	renderPage(w, "車輛管理", "/vehicles", user.Username, user.Role, body)
}

func CreateVehicle(w http.ResponseWriter, r *http.Request) {
	customerID := mustInt(r.FormValue("customer_id"))
	plate := strings.TrimSpace(r.FormValue("plate_number"))
	model_ := strings.TrimSpace(r.FormValue("brand_model"))

	if customerID == 0 || plate == "" || model_ == "" {
		fail(w, "客戶、車牌、車型為必填")
		return
	}
	if countRows("SELECT COUNT(1) FROM customers WHERE id = ?", customerID) == 0 {
		fail(w, "找不到該客戶")
		return
	}

	_, err := db.DB.Exec(`INSERT INTO vehicles
		(customer_id, plate_number, brand_model, manufacture_year, vin, engine_number,
		 inspection_due_date, next_maintenance_date, next_maintenance_mileage)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		customerID, plate, model_,
		atoiDefault(r.FormValue("manufacture_year"), 0), nullStr(r.FormValue("vin")),
		nullStr(r.FormValue("engine_number")),
		nullDate(r.FormValue("inspection_due_date")),
		nullDate(r.FormValue("next_maintenance_date")),
		atoiDefault(r.FormValue("next_maintenance_mileage"), 0),
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			fail(w, "車牌號碼已存在")
			return
		}
		fail(w, "建立車輛失敗")
		return
	}

	redirectMsg(w, r, "/vehicles", "車輛已建立")
}

func EditVehicleForm(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))

	row := db.DB.QueryRow(`SELECT `+vehicleCols+` FROM vehicles v
		JOIN customers c ON c.id = v.customer_id WHERE v.id = ?`, id)
	v, err := scanVehicle(row)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	body := `
    <div class="card">
      <h2>編輯車輛 ` + htmlEsc(v.PlateNumber) + `</h2>
      <form class="frm" method="POST" action="/vehicles/` + strconv.FormatInt(v.ID, 10) + `">
        <label>所屬客戶<select name="customer_id" required>` + customerOptions() + `</select></label>
        <label>車牌號碼<input name="plate_number" value="` + htmlEsc(v.PlateNumber) + `" required></label>
        <label>品牌車型<input name="brand_model" value="` + htmlEsc(v.BrandModel) + `" required></label>
        <label>出廠年份<input name="manufacture_year" type="number" value="` + strconv.Itoa(v.ManufactureYear) + `" min="1900" max="2100"></label>
        <label>VIN<input name="vin" value="` + htmlEsc(v.VIN) + `"></label>
        <label>引擎號碼<input name="engine_number" value="` + htmlEsc(v.EngineNumber) + `"></label>
        <label>檢驗到期日<input name="inspection_due_date" type="date" value="` + htmlEsc(v.InspectionDueDate) + `"></label>
        <label>下次保養日期<input name="next_maintenance_date" type="date" value="` + htmlEsc(v.NextMaintenanceDate) + `"></label>
        <label>下次保養里程<input name="next_maintenance_mileage" type="number" value="` + strconv.Itoa(v.NextMaintenanceMileage) + `" min="0"></label>
        <div class="full"><button class="btn btn-p" type="submit">儲存</button> <a class="btn btn-s" href="/vehicles">取消</a></div>
      </form>
    </div>`

	renderPage(w, "編輯車輛", "/vehicles", user.Username, user.Role, body)
}

func UpdateVehicle(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	customerID := mustInt(r.FormValue("customer_id"))
	plate := strings.TrimSpace(r.FormValue("plate_number"))
	model_ := strings.TrimSpace(r.FormValue("brand_model"))

	if customerID == 0 || plate == "" || model_ == "" {
		fail(w, "客戶、車牌、車型為必填")
		return
	}

	res, err := db.DB.Exec(`UPDATE vehicles SET customer_id=?, plate_number=?, brand_model=?,
		manufacture_year=?, vin=?, engine_number=?, inspection_due_date=?,
		next_maintenance_date=?, next_maintenance_mileage=? WHERE id=?`,
		customerID, plate, model_,
		atoiDefault(r.FormValue("manufacture_year"), 0), nullStr(r.FormValue("vin")),
		nullStr(r.FormValue("engine_number")),
		nullDate(r.FormValue("inspection_due_date")),
		nullDate(r.FormValue("next_maintenance_date")),
		atoiDefault(r.FormValue("next_maintenance_mileage"), 0), id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			fail(w, "車牌號碼已存在")
			return
		}
		fail(w, "更新車輛失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}

	redirectMsg(w, r, "/vehicles", "車輛已更新")
}

func DeleteVehicle(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	if countRows("SELECT COUNT(1) FROM work_orders WHERE vehicle_id = ?", id) > 0 {
		fail(w, "此車輛已有工單紀錄，無法刪除")
		return
	}

	res, err := db.DB.Exec("DELETE FROM vehicles WHERE id = ?", id)
	if err != nil {
		fail(w, "刪除車輛失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}

	redirectMsg(w, r, "/vehicles", "車輛已刪除")
}

func customerOptions() string {
	rows, err := db.DB.Query("SELECT id, name, phone FROM customers ORDER BY name")
	if err != nil {
		return ""
	}
	defer rows.Close()

	var b strings.Builder
	for rows.Next() {
		var id int64
		var name, phone string
		if err := rows.Scan(&id, &name, &phone); err != nil {
			continue
		}
		b.WriteString(`<option value="` + strconv.FormatInt(id, 10) + `">` + htmlEsc(name) + ` (` + htmlEsc(phone) + `)</option>`)
	}
	return b.String()
}

func vehicleOptions() string {
	rows, err := db.DB.Query(`SELECT v.id, v.plate_number, v.brand_model, COALESCE(c.name,'')
		FROM vehicles v JOIN customers c ON c.id = v.customer_id ORDER BY v.plate_number`)
	if err != nil {
		return ""
	}
	defer rows.Close()

	var b strings.Builder
	for rows.Next() {
		var id int64
		var plate, model_, cname string
		if err := rows.Scan(&id, &plate, &model_, &cname); err != nil {
			continue
		}
		b.WriteString(`<option value="` + strconv.FormatInt(id, 10) + `">` + htmlEsc(plate) + ` / ` +
			htmlEsc(model_) + ` / ` + htmlEsc(cname) + `</option>`)
	}
	return b.String()
}

func yearText(y int) string {
	if y == 0 {
		return "-"
	}
	return strconv.Itoa(y)
}
