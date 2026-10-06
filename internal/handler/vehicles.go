package handler

import (
	"net/http"
	"net/url"
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
	qv := r.URL.Query()
	q := strings.TrimSpace(qv.Get("q"))
	filterCustomer := mustInt(qv.Get("customer"))
	page := pageNo(r)
	size := pageSize(r)

	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (v.plate_number LIKE ? OR v.brand_model LIKE ? OR c.name LIKE ? OR COALESCE(v.vin,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like, like)
	}
	if filterCustomer > 0 {
		where += ` AND v.customer_id = ?`
		args = append(args, filterCustomer)
	}

	total := countRows(`SELECT COUNT(1) FROM vehicles v
		JOIN customers c ON c.id = v.customer_id`+where, args...)

	query := `SELECT ` + vehicleCols + ` FROM vehicles v
		JOIN customers c ON c.id = v.customer_id` + where +
		` ORDER BY v.id DESC LIMIT ? OFFSET ?`
	pageArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := db.DB.Query(query, pageArgs...)
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
			`<a class="btn btn-o" href="/vehicles/` + strconv.FormatInt(v.ID, 10) + `">履歷</a> ` +
			`<a class="btn btn-s" href="/vehicles/` + strconv.FormatInt(v.ID, 10) + `/edit">編輯</a> ` +
			`<a class="btn btn-d" href="/vehicles/` + strconv.FormatInt(v.ID, 10) + `/delete" onclick="return confirm('確定刪除這台車？')">刪除</a></td></tr>`)
	}
	tb.WriteString(`</table>`)

	table := tb.String()
	if len(list) == 0 {
		table = `<div class="empty">沒有符合條件的車輛</div>`
	}

	filterNote := ""
	if filterCustomer > 0 {
		filterNote = ` <a href="/vehicles" style="font-size:13px">（清除客戶篩選）</a>`
	}

	body := flash(r) + `
    <div class="card">
      <h2>新增車輛</h2>
      <form class="frm" method="POST" action="/vehicles">
        <label>所屬客戶（編號 / 姓名 / 電話）<input name="customer_query" required placeholder="例：王小明 或 0912345678 或 1234"></label>
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
      <h2>車輛列表（符合條件 ` + strconv.Itoa(total) + ` 筆）` + filterNote + `</h2>
      <form method="GET" action="/vehicles" style="margin-bottom:12px">
        <input name="q" value="` + htmlEsc(q) + `" placeholder="搜尋車牌 / 車型 / VIN / 業主" style="padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;width:320px">
        ` + hiddenInputs(qv, "q", "page", "per") + `
        <button class="btn btn-p" type="submit">搜尋</button>
        <a class="btn btn-s" href="/vehicles">清除</a>
      </form>` + table + pager("/vehicles", qv, total, page, size) +
		`<div style="margin-top:6px">` + perPageSel("/vehicles", qv, size) + `</div>
    </div>`

	renderPage(w, "車輛管理", "/vehicles", user.Username, user.Role, body)
}

// ShowVehicle 車輛履歷：基本資料 + 歷年工單 + 消費統計。
//
// 車行最常被問的三件事：「這台車以前修過什麼？」「換過哪些零件？」
// 「總共花了多少錢？」——這一頁一次回答完。
// 對應 CAR35 的 Carp74（車輛查詢）。
func ShowVehicle(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))

	row := db.DB.QueryRow(`SELECT `+vehicleCols+` FROM vehicles v
		JOIN customers c ON c.id = v.customer_id WHERE v.id = ?`, id)
	v, err := scanVehicle(row)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// 消費統計
	var orderCount int
	var totalSpent, unpaid float64
	db.DB.QueryRow(`SELECT COUNT(1), COALESCE(SUM(total_amount),0),
		COALESCE(SUM(CASE WHEN status <> 'paid' THEN total_amount ELSE 0 END),0)
		FROM work_orders WHERE vehicle_id = ?`, id).
		Scan(&orderCount, &totalSpent, &unpaid)

	var lastMileage int
	db.DB.QueryRow(`SELECT COALESCE(MAX(current_mileage),0) FROM work_orders WHERE vehicle_id = ?`, id).
		Scan(&lastMileage)

	// 歷年工單
	rows, err := db.DB.Query(`SELECT wo.id, wo.status, wo.current_mileage,
		COALESCE(wo.total_amount,0), wo.created_at, COALESCE(wo.notes,''),
		COALESCE(u.name,''), COALESCE(ic.name,''), COALESCE(wo.claim_no,'')
		FROM work_orders wo
		LEFT JOIN users u ON u.id = wo.technician_id
		LEFT JOIN insurance_companies ic ON ic.id = wo.insurance_company_id
		WHERE wo.vehicle_id = ?
		ORDER BY wo.created_at DESC, wo.id DESC LIMIT 200`, id)
	if err != nil {
		fail(w, "查詢工單失敗")
		return
	}
	defer rows.Close()

	var tb strings.Builder
	tb.WriteString(`<table><tr><th>#</th><th>日期</th><th>狀態</th><th class="num">里程</th><th class="num">金額</th><th>技師</th><th>保險</th><th>備註</th></tr>`)
	n := 0
	for rows.Next() {
		var woID int64
		var status, notes, tech, insName, claimNo, createdAt string
		var mileage int
		var amount float64
		if err := rows.Scan(&woID, &status, &mileage, &amount, &createdAt,
			&notes, &tech, &insName, &claimNo); err != nil {
			continue
		}
		n++
		insCell := "-"
		if insName != "" {
			insCell = htmlEsc(insName)
			if claimNo != "" {
				insCell += `<br><span style="color:#64748b;font-size:11.5px">` + htmlEsc(claimNo) + `</span>`
			}
		}
		tb.WriteString(`<tr><td><a href="/work-orders/` + strconv.FormatInt(woID, 10) + `">` +
			strconv.FormatInt(woID, 10) + `</a></td><td>` + woCreatedText(parseFlexTime(createdAt)) +
			`</td><td><span class="pill ` + statusPill[status] + `">` + workOrderStatusLabel[status] +
			`</span></td><td class="num">` + strconv.Itoa(mileage) + `</td><td class="num">` + money(amount) +
			`</td><td>` + orDash(htmlEsc(tech)) + `</td><td>` + insCell + `</td><td style="font-size:12px;color:#94a3b8">` +
			htmlEsc(notes) + `</td></tr>`)
	}
	tb.WriteString(`</table>`)

	ordersTable := tb.String()
	if n == 0 {
		ordersTable = `<div class="empty">這台車還沒有維修紀錄</div>`
	}

	// 換過的零件（彙總）
	var partsTable string
	prows, perr := db.DB.Query(`SELECT woi.description, SUM(woi.quantity), MAX(woi.unit_price),
		COUNT(DISTINCT woi.work_order_id)
		FROM work_order_items woi
		JOIN work_orders wo ON wo.id = woi.work_order_id
		WHERE wo.vehicle_id = ?
		GROUP BY woi.description
		ORDER BY COUNT(DISTINCT woi.work_order_id) DESC, SUM(woi.quantity) DESC
		LIMIT 50`, id)
	if perr == nil {
		defer prows.Close()
		var pb strings.Builder
		pb.WriteString(`<table><tr><th>項目</th><th class="num">累計數量</th><th class="num">最近單價</th><th class="num">出現工單數</th></tr>`)
		pn := 0
		for prows.Next() {
			var desc string
			var qty float64
			var price float64
			var orderN int
			if err := prows.Scan(&desc, &qty, &price, &orderN); err != nil {
				continue
			}
			pn++
			pb.WriteString(`<tr><td>` + htmlEsc(desc) + `</td><td class="num">` + money(qty) +
				`</td><td class="num">` + money(price) + `</td><td class="num">` + strconv.Itoa(orderN) + `</td></tr>`)
		}
		pb.WriteString(`</table>`)
		if pn > 0 {
			partsTable = `<div class="card"><h2>歷年更換項目（前 50 項）</h2>` + pb.String() + `</div>`
		}
	}

	body := `
    <div class="card">
      <h2>` + htmlEsc(v.PlateNumber) + ` — ` + htmlEsc(v.BrandModel) + `</h2>
      <div style="margin-bottom:14px">
        <div class="stat"><b>` + strconv.Itoa(orderCount) + `</b><span>維修次數</span></div>
        <div class="stat"><b>` + money(totalSpent) + `</b><span>累計消費</span></div>
        <div class="stat"><b>` + money(unpaid) + `</b><span>未結金額</span></div>
        <div class="stat"><b>` + strconv.Itoa(lastMileage) + `</b><span>最近里程</span></div>
      </div>
      <table>
        <tr><th>業主</th><td>` + htmlEsc(v.CustomerName) + ` <a href="/customers/` +
		strconv.FormatInt(v.CustomerID, 10) + `/edit" style="font-size:12.5px">（編輯客戶）</a></td></tr>
        <tr><th>出廠年份</th><td>` + yearText(v.ManufactureYear) + `</td></tr>
        <tr><th>VIN</th><td>` + orDash(htmlEsc(v.VIN)) + `</td></tr>
        <tr><th>引擎號碼</th><td>` + orDash(htmlEsc(v.EngineNumber)) + `</td></tr>
        <tr><th>檢驗到期日</th><td>` + dateText(v.InspectionDueDate) + `</td></tr>
        <tr><th>下次保養</th><td>` + dateText(v.NextMaintenanceDate) +
		func() string {
			if v.NextMaintenanceMileage > 0 {
				return " / " + strconv.Itoa(v.NextMaintenanceMileage) + "km"
			}
			return ""
		}() + `</td></tr>
      </table>
      <div style="margin-top:16px">
        <a class="btn btn-p" href="/work-orders/new?vehicle=` + strconv.FormatInt(v.ID, 10) + `">＋ 開立工單</a>
        <a class="btn btn-s" href="/vehicles/` + strconv.FormatInt(v.ID, 10) + `/edit">編輯車輛</a>
        <a class="btn btn-s" href="/work-orders?q=` + url.QueryEscape(v.PlateNumber) + `">在工單列表查詢</a>
        <a class="btn btn-s" href="/vehicles">回車輛列表</a>
      </div>
    </div>
    <div class="card"><h2>維修紀錄（最近 200 筆）</h2>` + ordersTable + `</div>` + partsTable

	renderPage(w, "車輛履歷 "+v.PlateNumber, "/vehicles", user.Username, user.Role, body)
}

func CreateVehicle(w http.ResponseWriter, r *http.Request) {
	customerID, errMsg := resolveCustomerID(r)
	plate := strings.TrimSpace(r.FormValue("plate_number"))
	model_ := strings.TrimSpace(r.FormValue("brand_model"))

	if errMsg != "" {
		fail(w, errMsg)
		return
	}
	if plate == "" || model_ == "" {
		fail(w, "車牌、車型為必填")
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
        <label>所屬客戶（編號 / 姓名 / 電話）<input name="customer_query" value="` + htmlEsc(v.CustomerName) + `" required></label>
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
	customerID, errMsg := resolveCustomerID(r)
	plate := strings.TrimSpace(r.FormValue("plate_number"))
	model_ := strings.TrimSpace(r.FormValue("brand_model"))

	if errMsg != "" {
		fail(w, errMsg)
		return
	}
	if plate == "" || model_ == "" {
		fail(w, "車牌、車型為必填")
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

// resolveCustomerID 從表單解析客戶。
//
// 為什麼不用下拉選單：客戶有 6 千多筆（舊系統遷移進來後），
// 全部塞進 <select> 會讓頁面變成 400KB 以上，且實務上沒人捲得動。
// 改成輸入「客戶編號 / 姓名 / 電話」由伺服器解析，符合車行打單習慣。
//
// 回傳 (id, errorMsg)。errorMsg 非空代表解析失敗，呼叫端應直接回錯。
func resolveCustomerID(r *http.Request) (int64, string) {
	// 表單直接帶 id（例如從客戶列表點「開工單」帶過來）
	if id := mustInt(r.FormValue("customer_id")); id > 0 {
		if countRows("SELECT COUNT(1) FROM customers WHERE id = ?", id) > 0 {
			return id, ""
		}
		return 0, "找不到該客戶"
	}

	raw := strings.TrimSpace(r.FormValue("customer_query"))
	if raw == "" {
		return 0, "請輸入客戶（編號 / 姓名 / 電話）"
	}

	// 純數字 → 先當 id，再當電話
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		var id int64
		if db.DB.QueryRow("SELECT id FROM customers WHERE id = ?", n).Scan(&id) == nil {
			return id, ""
		}
		if db.DB.QueryRow("SELECT id FROM customers WHERE phone = ?", raw).Scan(&id) == nil {
			return id, ""
		}
	}

	// 姓名或電話精確比對
	var id int64
	if db.DB.QueryRow("SELECT id FROM customers WHERE name = ? OR phone = ? LIMIT 1", raw, raw).
		Scan(&id) == nil {
		return id, ""
	}
	// 模糊比對（取第一筆，並要求唯一以免誤選）
	like := "%" + raw + "%"
	rows, err := db.DB.Query(`SELECT id FROM customers
		WHERE name LIKE ? OR phone LIKE ? OR COALESCE(notes,'') LIKE ? LIMIT 5`, like, like, like)
	if err != nil {
		return 0, "查詢客戶失敗"
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var v int64
		if rows.Scan(&v) == nil {
			ids = append(ids, v)
		}
	}
	switch len(ids) {
	case 0:
		return 0, "找不到符合「" + raw + "」的客戶，請先到客戶頁建立"
	case 1:
		return ids[0], ""
	default:
		return 0, "「" + raw + "」符合多筆客戶，請輸入更完整的姓名或直接填客戶編號"
	}
}

// customerOptions 客戶下拉選單（僅用於小資料集，如工單篩選）。
// 大資料集請改用 resolveCustomerID 搭配文字輸入。
func customerOptions() string {
	rows, err := db.DB.Query("SELECT id, name, phone FROM customers ORDER BY name LIMIT 300")
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
