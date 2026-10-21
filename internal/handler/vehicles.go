package handler

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

const vehicleCols = `v.id, v.customer_id, COALESCE(c.name,''), v.plate_number, v.brand_model,
	COALESCE(v.vehicle_type,''), COALESCE(v.driver_name,''), COALESCE(v.driver_phone,''),
	COALESCE(v.contact_name,''), COALESCE(v.contact_phone,''), COALESCE(v.owner_name,''),
	COALESCE(v.owner_address,''), COALESCE(v.owner_id_number,''), COALESCE(v.license_expiry_date,''),
	COALESCE(v.color,''), COALESCE(v.door_count,0), COALESCE(v.manufacture_year,0),
	COALESCE(v.manufacture_date,''), COALESCE(v.license_issue_date,''),
	COALESCE(v.fuel_type,''), COALESCE(v.displacement,0), COALESCE(v.body_style,''),
	COALESCE(v.seating_capacity,0), COALESCE(v.curb_weight,0), COALESCE(v.gross_weight,0),
	COALESCE(v.tire_size,''), COALESCE(v.wheelbase,0), COALESCE(v.vin,''), COALESCE(v.engine_number,''),
	COALESCE(v.inspection_due_date,''), COALESCE(v.next_maintenance_date,''),
	COALESCE(v.next_maintenance_mileage,0),
	COALESCE(v.last_maintenance_date,''), COALESCE(v.last_maintenance_mileage,0),
	COALESCE(v.next_maintenance_interval_months,6), COALESCE(v.next_maintenance_interval_km,5000)`

func scanVehicle(rows interface{ Scan(...interface{}) error }) (model.Vehicle, error) {
	var v model.Vehicle
	err := rows.Scan(&v.ID, &v.CustomerID, &v.CustomerName, &v.PlateNumber, &v.BrandModel,
		&v.VehicleType, &v.DriverName, &v.DriverPhone, &v.ContactName, &v.ContactPhone, &v.OwnerName,
		&v.OwnerAddress, &v.OwnerIDNumber, &v.LicenseExpiryDate,
		&v.Color, &v.DoorCount, &v.ManufactureYear, &v.ManufactureDate, &v.LicenseIssueDate,
		&v.FuelType, &v.Displacement, &v.BodyStyle, &v.SeatingCapacity, &v.CurbWeight, &v.GrossWeight,
		&v.TireSize, &v.Wheelbase, &v.VIN, &v.EngineNumber, &v.InspectionDueDate,
		&v.NextMaintenanceDate, &v.NextMaintenanceMileage,
		&v.LastMaintenanceDate, &v.LastMaintenanceMileage,
		&v.MaintIntervalMonths, &v.MaintIntervalKm)
	if v.VehicleType != "" {
		v.VehicleTypeLabel = lookupLabel("vehicle_type", v.VehicleType)
	}
	if v.FuelType != "" {
		v.FuelTypeLabel = lookupLabel("fuel_type", v.FuelType)
	}
	if v.BodyStyle != "" {
		v.BodyStyleLabel = lookupLabel("body_style", v.BodyStyle)
	}
	return v, err
}

// normDate 把舊資料的 '2016/02/22' 轉成 HTML date input 要的 '2016-02-22'。
func normDate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 10 {
		s = s[:10]
	}
	return strings.ReplaceAll(s, "/", "-")
}

// intOrBlank 0 顯示為空字串（表單留白比填 0 清楚）。
func intOrBlank(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// lastMileageOf 該車工單紀錄中的最大里程（用來算「距下次保養還差多少」）。
func lastMileageOf(vehicleID int64) int {
	var km int
	db.DB.QueryRow(`SELECT COALESCE(MAX(current_mileage),0) FROM active_work_orders
		WHERE vehicle_id = ?`, vehicleID).Scan(&km)
	return km
}

// maintDueText 保養到期顯示：日期與里程分開兩欄，各自標示是否已到。
//
// 為什麼要分開：保養是兩條獨立觸發條件 —— 里程到（車跑得多）或日期到
// （車少跑）。舊版併成一個字串「2026-04-01 / 5500km」，看不出哪個先到，
// 也無法判斷是否已逾期。
func maintDueText(v model.Vehicle, currentMileage int) (string, string) {
	dateCell := `<span style="color:#64748b">-</span>`
	if v.NextMaintenanceDate != "" {
		d := v.NextMaintenanceDate
		if len(d) > 10 {
			d = d[:10]
		}
		if t, err := time.Parse("2006-01-02", d); err == nil {
			days := int(time.Until(t).Hours() / 24)
			switch {
			case days < 0:
				dateCell = `<b style="color:#f87171">` + d + ` 逾期` + strconv.Itoa(-days) + `天</b>`
			case days <= 14:
				dateCell = `<b style="color:#fde68a">` + d + ` 剩` + strconv.Itoa(days) + `天</b>`
			default:
				dateCell = d
			}
		} else {
			dateCell = d
		}
	}

	kmCell := `<span style="color:#64748b">-</span>`
	if v.NextMaintenanceMileage > 0 {
		if currentMileage > 0 {
			remain := v.NextMaintenanceMileage - currentMileage
			switch {
			case remain < 0:
				kmCell = `<b style="color:#f87171">` + strconv.Itoa(v.NextMaintenanceMileage) + `km 已超過 ` + strconv.Itoa(-remain) + `</b>`
			case remain <= 1000:
				kmCell = `<b style="color:#fde68a">` + strconv.Itoa(v.NextMaintenanceMileage) + `km 剩 ` + strconv.Itoa(remain) + `</b>`
			default:
				kmCell = strconv.Itoa(v.NextMaintenanceMileage) + "km"
			}
		} else {
			kmCell = strconv.Itoa(v.NextMaintenanceMileage) + "km"
		}
	}
	return dateCell, kmCell
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

	total := countRows(`SELECT COUNT(1) FROM active_vehicles v
		JOIN active_customers c ON c.id = v.customer_id`+where, args...)

	query := `SELECT ` + vehicleCols + ` FROM active_vehicles v
		JOIN active_customers c ON c.id = v.customer_id` + where +
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

	var vehicleRows []pages.VehicleRow
	for _, v := range list {
		curKm := lastMileageOf(v.ID)
		dateCell, kmCell := maintDueText(v, curKm)
		vehicleRows = append(vehicleRows, pages.VehicleRow{
			ID:               v.ID,
			PlateNumber:      v.PlateNumber,
			BrandModel:       v.BrandModel,
			VehicleTypeLabel: v.VehicleTypeLabel,
			ManufactureYear:  v.ManufactureYear,
			CustomerName:     v.CustomerName,
			DateCell:         dateCell,
			KmCell:           kmCell,
			CanEdit:          true,
		})
	}

	pages.ListVehiclesPage(vehicleRows, total, page, size, q, filterCustomer,
		vehicleModelOptions(""),
		lookupOptions("vehicle_type", ""),
		lookupOptions("fuel_type", ""),
		lookupOptions("body_style", ""),
		user).Render(r.Context(), w)
}

// SearchVehiclesAPI 車輛即時搜尋 API（HTMX 用），回傳純 HTML 列。
func SearchVehiclesAPI(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		w.Write([]byte(`<tr><td colspan="9" style="color:#64748b;text-align:center;padding:20px">查無車輛</td></tr>`))
		return
	}
	like := "%" + q + "%"
	rows, err := db.DB.Query(`SELECT `+vehicleCols+` FROM active_vehicles v
		JOIN active_customers c ON c.id = v.customer_id
		WHERE v.plate_number LIKE ? OR v.brand_model LIKE ? OR c.name LIKE ? OR COALESCE(v.vin,'') LIKE ?
		ORDER BY v.id DESC LIMIT 50`, like, like, like, like)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var b strings.Builder
	for rows.Next() {
		v, err := scanVehicle(rows)
		if err != nil {
			continue
		}
		typeCell := `<span style="color:#64748b">-</span>`
		if v.VehicleTypeLabel != "" {
			typeCell = `<span class="pill" style="background:#1e3a5f;color:#bae6fd">` + v.VehicleTypeLabel + `</span>`
		}
		b.WriteString(`<tr><td>` + strconv.FormatInt(v.ID, 10) + `</td><td><b>` + v.PlateNumber + `</b></td><td>` +
			v.BrandModel + `</td><td>` + typeCell + `</td><td>` + intOrBlank(v.ManufactureYear) + `</td><td>` +
			v.CustomerName + `</td><td></td><td></td><td>` +
			`<a class="btn btn-p" href="/work-orders/new?vehicle=` + strconv.FormatInt(v.ID, 10) + `">開工單</a> ` +
			`<a class="btn btn-o" href="/vehicles/` + strconv.FormatInt(v.ID, 10) + `">履歷</a> ` +
			`<a class="btn btn-s" href="/vehicles/` + strconv.FormatInt(v.ID, 10) + `/edit">編輯</a></td></tr>`)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(b.String()))
}

// ShowVehicle 車輛履歷：基本資料 + 歷年工單 + 消費統計。
//
// 車行最常被問的三件事：「這台車以前修過什麼？」「換過哪些零件？」
// 「總共花了多少錢？」——這一頁一次回答完。
// 對應 CAR35 的 Carp74（車輛查詢）。
func ShowVehicle(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))

	row := db.DB.QueryRow(`SELECT `+vehicleCols+` FROM active_vehicles v
		JOIN active_customers c ON c.id = v.customer_id WHERE v.id = ?`, id)
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
		FROM active_work_orders WHERE vehicle_id = ?`, id).
		Scan(&orderCount, &totalSpent, &unpaid)

	var lastMileage int
	db.DB.QueryRow(`SELECT COALESCE(MAX(current_mileage),0) FROM active_work_orders WHERE vehicle_id = ?`, id).
		Scan(&lastMileage)

	// 歷年工單
	rows, err := db.DB.Query(`SELECT wo.id, wo.status, wo.current_mileage,
		COALESCE(wo.total_amount,0), wo.created_at, COALESCE(wo.notes,''),
		COALESCE(u.name,''), COALESCE(ic.name,''), COALESCE(wo.claim_no,'')
		FROM active_work_orders wo
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
		JOIN active_work_orders wo ON wo.id = woi.work_order_id
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

	// 保養狀態：日期與里程分開兩列，各自標示是否已到
	curKm := lastMileageOf(v.ID)
	dateCell, kmCell := maintDueText(v, curKm)

	pages.ShowVehiclePage(v,
		orderCount, totalSpent, unpaid, curKm,
		dateCell, kmCell,
		mileageHistory(v.ID, 50), ordersTable, partsTable,
		user).Render(r.Context(), w)
}

func CreateVehicle(w http.ResponseWriter, r *http.Request) {
	customerID, errMsg := resolveCustomerID(r)
	plate := strings.TrimSpace(r.FormValue("plate_number"))
	model_ := normVehicleModel(r.FormValue("brand_model"))
	vehicleType := strings.TrimSpace(r.FormValue("vehicle_type"))

	if errMsg != "" {
		fail(w, errMsg)
		return
	}
	if plate == "" || model_ == "" {
		fail(w, "車牌、車型為必填（若選了「其他」請輸入車型）")
		return
	}

	_, err := db.DB.Exec(`INSERT INTO vehicles
		(customer_id, plate_number, brand_model, vehicle_type, driver_name, driver_phone,
		 contact_name, contact_phone, owner_name, owner_address, owner_id_number, license_expiry_date,
		 color, door_count, manufacture_year, manufacture_date, license_issue_date,
		 fuel_type, displacement, body_style, seating_capacity, curb_weight, gross_weight,
		 tire_size, wheelbase, vin, engine_number,
		 inspection_due_date, next_maintenance_date, next_maintenance_mileage,
		 last_maintenance_date, last_maintenance_mileage,
		 next_maintenance_interval_months, next_maintenance_interval_km)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		customerID, plate, model_, vehicleType,
		nullStr(r.FormValue("driver_name")), nullStr(r.FormValue("driver_phone")),
		nullStr(r.FormValue("contact_name")), nullStr(r.FormValue("contact_phone")),
		nullStr(r.FormValue("owner_name")), nullStr(r.FormValue("owner_address")),
		nullStr(r.FormValue("owner_id_number")), nullStr(r.FormValue("license_expiry_date")),
		nullStr(r.FormValue("color")), atoiDefault(r.FormValue("door_count"), 0),
		atoiDefault(r.FormValue("manufacture_year"), 0),
		nullStr(r.FormValue("manufacture_date")), nullStr(r.FormValue("license_issue_date")),
		nullStr(r.FormValue("fuel_type")), atoiDefault(r.FormValue("displacement"), 0),
		nullStr(r.FormValue("body_style")), atoiDefault(r.FormValue("seating_capacity"), 0),
		atoiDefault(r.FormValue("curb_weight"), 0), atoiDefault(r.FormValue("gross_weight"), 0),
		nullStr(r.FormValue("tire_size")), atoiDefault(r.FormValue("wheelbase"), 0),
		nullStr(r.FormValue("vin")), nullStr(r.FormValue("engine_number")),
		nullDate(r.FormValue("inspection_due_date")),
		nil, nil,
		nullDate(r.FormValue("last_maintenance_date")),
		atoiOrNil(r.FormValue("last_maintenance_mileage")),
		atoiDefault(r.FormValue("maint_interval_months"), 6),
		atoiDefault(r.FormValue("maint_interval_km"), 5000),
	)
	if err != nil {
		log.Printf("建立車輛失敗（plate=%s）: %v", plate, err)
		if strings.Contains(err.Error(), "UNIQUE") {
			fail(w, "車牌號碼已存在")
			return
		}
		fail(w, "建立車輛失敗")
		return
	}

	if vid := lastInsertID("vehicles"); vid > 0 {
		recalcNextMaintenance(vid)
	}

	redirectMsg(w, r, "/vehicles", "車輛已建立")
}

// atoiOrNil 空字串回 nil（讓 DB 存 NULL 而非 0）。
func atoiOrNil(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return nil
	}
	return n
}

func lastInsertID(table string) int64 {
	var id int64
	db.DB.QueryRow("SELECT COALESCE(MAX(id),0) FROM " + table).Scan(&id)
	return id
}

// recalcNextMaintenance 由「最後保養基準 + 間隔」推算下次保養日期與里程。
//
// 保養是兩條獨立條件（日期到 / 里程到），兩者都要算，先到者為準。
// 車行慣例：每 6 個月或每 5000 公里保養一次（可用 vehicles 的間隔欄位調整）。
func recalcNextMaintenance(vehicleID int64) {
	var lastDate string
	var lastKm, months, km int
	err := db.DB.QueryRow(`SELECT COALESCE(last_maintenance_date,''),
		COALESCE(last_maintenance_mileage,0),
		COALESCE(next_maintenance_interval_months,6),
		COALESCE(next_maintenance_interval_km,5000)
		FROM active_vehicles WHERE id = ?`, vehicleID).Scan(&lastDate, &lastKm, &months, &km)
	if err != nil {
		return
	}
	if months <= 0 {
		months = 6
	}
	if km <= 0 {
		km = 5000
	}

	var nextDate interface{}
	if d := normDate(lastDate); d != "" {
		if t, perr := time.Parse("2006-01-02", d); perr == nil {
			nextDate = t.AddDate(0, months, 0).Format("2006-01-02")
		}
	}
	var nextKm interface{}
	if lastKm > 0 {
		nextKm = lastKm + km
	}

	db.DB.Exec(`UPDATE vehicles SET next_maintenance_date=?, next_maintenance_mileage=? WHERE id=?`,
		nextDate, nextKm, vehicleID)
}

func EditVehicleForm(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))

	row := db.DB.QueryRow(`SELECT `+vehicleCols+` FROM active_vehicles v
		JOIN active_customers c ON c.id = v.customer_id WHERE v.id = ?`, id)
	v, err := scanVehicle(row)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	pages.EditVehiclePage(v,
		vehicleModelOptions(v.BrandModel),
		lookupOptions("vehicle_type", v.VehicleType),
		lookupOptions("fuel_type", v.FuelType),
		lookupOptions("body_style", v.BodyStyle),
		user).Render(r.Context(), w)
}

func UpdateVehicle(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	customerID, errMsg := resolveCustomerID(r)
	plate := strings.TrimSpace(r.FormValue("plate_number"))
	model_ := normVehicleModel(r.FormValue("brand_model"))

	if errMsg != "" {
		fail(w, errMsg)
		return
	}
	if plate == "" || model_ == "" {
		fail(w, "車牌、車型為必填（若選了「其他」請輸入車型）")
		return
	}

	res, err := db.DB.Exec(`UPDATE vehicles SET customer_id=?, plate_number=?, brand_model=?,
		vehicle_type=?, driver_name=?, driver_phone=?, contact_name=?, contact_phone=?, owner_name=?,
		owner_address=?, owner_id_number=?, license_expiry_date=?,
		color=?, door_count=?, manufacture_year=?, manufacture_date=?, license_issue_date=?,
		fuel_type=?, displacement=?, body_style=?, seating_capacity=?, curb_weight=?, gross_weight=?,
		tire_size=?, wheelbase=?, vin=?, engine_number=?, inspection_due_date=?,
		last_maintenance_date=?, last_maintenance_mileage=?,
		next_maintenance_interval_months=?, next_maintenance_interval_km=? WHERE id=?`,
		customerID, plate, model_, strings.TrimSpace(r.FormValue("vehicle_type")),
		nullStr(r.FormValue("driver_name")), nullStr(r.FormValue("driver_phone")),
		nullStr(r.FormValue("contact_name")), nullStr(r.FormValue("contact_phone")),
		nullStr(r.FormValue("owner_name")), nullStr(r.FormValue("owner_address")),
		nullStr(r.FormValue("owner_id_number")), nullStr(r.FormValue("license_expiry_date")),
		nullStr(r.FormValue("color")), atoiDefault(r.FormValue("door_count"), 0),
		atoiDefault(r.FormValue("manufacture_year"), 0),
		nullStr(r.FormValue("manufacture_date")), nullStr(r.FormValue("license_issue_date")),
		nullStr(r.FormValue("fuel_type")), atoiDefault(r.FormValue("displacement"), 0),
		nullStr(r.FormValue("body_style")), atoiDefault(r.FormValue("seating_capacity"), 0),
		atoiDefault(r.FormValue("curb_weight"), 0), atoiDefault(r.FormValue("gross_weight"), 0),
		nullStr(r.FormValue("tire_size")), atoiDefault(r.FormValue("wheelbase"), 0),
		nullStr(r.FormValue("vin")), nullStr(r.FormValue("engine_number")),
		nullDate(r.FormValue("inspection_due_date")),
		nullDate(r.FormValue("last_maintenance_date")),
		atoiOrNil(r.FormValue("last_maintenance_mileage")),
		atoiDefault(r.FormValue("maint_interval_months"), 6),
		atoiDefault(r.FormValue("maint_interval_km"), 5000), id)
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

	// 基準或間隔改了 → 重算下次保養
	recalcNextMaintenance(id)

	redirectMsg(w, r, "/vehicles", "車輛已更新")
}

func DeleteVehicle(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	if countRows("SELECT COUNT(1) FROM active_work_orders WHERE vehicle_id = ?", id) > 0 {
		fail(w, "此車輛已有工單紀錄，無法刪除")
		return
	}

	res, err := db.DB.Exec("UPDATE vehicles SET deleted_at = datetime('now') WHERE id = ? AND deleted_at IS NULL", id)
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
		if countRows("SELECT COUNT(1) FROM active_customers WHERE id = ?", id) > 0 {
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
		if db.DB.QueryRow("SELECT id FROM active_customers WHERE id = ?", n).Scan(&id) == nil {
			return id, ""
		}
		if db.DB.QueryRow("SELECT id FROM active_customers WHERE phone = ?", raw).Scan(&id) == nil {
			return id, ""
		}
	}

	// 姓名或電話精確比對
	var id int64
	if db.DB.QueryRow("SELECT id FROM active_customers WHERE name = ? OR phone = ? LIMIT 1", raw, raw).
		Scan(&id) == nil {
		return id, ""
	}
	// 模糊比對（取第一筆，並要求唯一以免誤選）
	like := "%" + raw + "%"
	rows, err := db.DB.Query(`SELECT id FROM active_customers
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
	rows, err := db.DB.Query("SELECT id, name, phone FROM active_customers ORDER BY name LIMIT 300")
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
		FROM active_vehicles v JOIN active_customers c ON c.id = v.customer_id ORDER BY v.plate_number LIMIT 300`)
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

// vehicleModelOther 是下拉的「其他」哨兵值。
//
// 用 __OTHER__ 而非空字串或 "其他"：空字串會被 required 擋下，
// "其他" 則是 migration 裡真實存在的選項（label「其他（手動輸入）」），
// 兩者都會與真正的自由輸入混淆。前後加底劃線的常值不會是合法車型。
const vehicleModelOther = "__OTHER__"

// vehicleModelOptions 車輛品牌車型下拉（lookup category='vehicle_model'）。
//
// selected 為該車現有的 brand_model：
//   - 在清單內 → 標示 selected，使用者看到目前值
//   - 不在清單內（舊資料或自由輸入過）→ 額外附一個「（現有值）」選項並
//     設為 selected，避免一進編輯頁就被靜默改寫成清單第一項
//   - 最後固定附「其他（手動輸入）」
func vehicleModelOptions(selected string) string {
	selected = strings.TrimSpace(selected)
	rows, err := db.DB.Query(`SELECT code, label FROM lookup_items
		WHERE category = 'vehicle_model' AND active = 1
		ORDER BY sort_order, code`)
	if err != nil {
		return ""
	}
	defer rows.Close()

	var b strings.Builder
	known := false
	for rows.Next() {
		var code, label string
		if rows.Scan(&code, &label) != nil {
			continue
		}
		sel := ""
		if code == selected {
			sel = " selected"
			known = true
		}
		b.WriteString(`<option value="` + htmlEsc(code) + `"` + sel + `>` + htmlEsc(label) + `</option>`)
	}
	// 現有值不在清單內：保留它，讓使用者自己決定是否正規化。
	if selected != "" && !known {
		b.WriteString(`<option value="` + htmlEsc(selected) + `" selected>` +
			htmlEsc(selected) + `（現有值）</option>`)
	}
	b.WriteString(`<option value="` + vehicleModelOther + `">其他（手動輸入）</option>`)
	return b.String()
}

// normVehicleModel 正規化車型輸入。
//
// 擋下 __OTHER__ 哨兵值：前端在選「其他」時會把 select 的 name 移除、
// 只送出文字欄位；但若使用者停用 JavaScript，哨兵值會直接送出而被存進
// brand_model，之後所有分析都會看到一台名為「__OTHER__」的車。
// 後端一律當成未填，讓必填檢查擋下。
func normVehicleModel(v string) string {
	v = strings.TrimSpace(v)
	if v == vehicleModelOther {
		return ""
	}
	return v
}
