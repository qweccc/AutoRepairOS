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
const vehicleListCols = vehicleCols + `,
	COALESCE(vt.label,''), COALESCE(ft.label,''), COALESCE(bs.label,''),
	COALESCE(lm.current_mileage,0)`
const vehicleListJoins = `
	LEFT JOIN lookup_items vt ON vt.category = 'vehicle_type' AND vt.code = v.vehicle_type AND vt.active = 1
	LEFT JOIN lookup_items ft ON ft.category = 'fuel_type' AND ft.code = v.fuel_type AND ft.active = 1
	LEFT JOIN lookup_items bs ON bs.category = 'body_style' AND bs.code = v.body_style AND bs.active = 1
	LEFT JOIN (
		SELECT vehicle_id, MAX(current_mileage) AS current_mileage
		FROM active_work_orders GROUP BY vehicle_id
	) lm ON lm.vehicle_id = v.id`

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
	v.LicenseExpiryDate = normDate(v.LicenseExpiryDate)
	v.ManufactureDate = normDate(v.ManufactureDate)
	v.LicenseIssueDate = normDate(v.LicenseIssueDate)
	v.InspectionDueDate = normDate(v.InspectionDueDate)
	v.NextMaintenanceDate = normDate(v.NextMaintenanceDate)
	v.LastMaintenanceDate = normDate(v.LastMaintenanceDate)
	return v, err
}
func scanVehicleListRow(rows interface{ Scan(...interface{}) error }) (model.Vehicle, int, error) {
	var v model.Vehicle
	var currentMileage int
	var vehicleTypeLabel, fuelTypeLabel, bodyStyleLabel string
	err := rows.Scan(&v.ID, &v.CustomerID, &v.CustomerName, &v.PlateNumber, &v.BrandModel,
		&v.VehicleType, &v.DriverName, &v.DriverPhone, &v.ContactName, &v.ContactPhone, &v.OwnerName,
		&v.OwnerAddress, &v.OwnerIDNumber, &v.LicenseExpiryDate,
		&v.Color, &v.DoorCount, &v.ManufactureYear, &v.ManufactureDate, &v.LicenseIssueDate,
		&v.FuelType, &v.Displacement, &v.BodyStyle, &v.SeatingCapacity, &v.CurbWeight, &v.GrossWeight,
		&v.TireSize, &v.Wheelbase, &v.VIN, &v.EngineNumber, &v.InspectionDueDate,
		&v.NextMaintenanceDate, &v.NextMaintenanceMileage,
		&v.LastMaintenanceDate, &v.LastMaintenanceMileage,
		&v.MaintIntervalMonths, &v.MaintIntervalKm,
		&vehicleTypeLabel, &fuelTypeLabel, &bodyStyleLabel, &currentMileage)
	v.VehicleTypeLabel = vehicleTypeLabel
	v.FuelTypeLabel = fuelTypeLabel
	v.BodyStyleLabel = bodyStyleLabel
	return v, currentMileage, err
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

	query = `SELECT ` + vehicleListCols + ` FROM active_vehicles v
		JOIN active_customers c ON c.id = v.customer_id` + vehicleListJoins + where +
		` ORDER BY v.id DESC LIMIT ? OFFSET ?`
	rows, err := db.DB.Query(query, pageArgs...)
	if err != nil {
		fail(w, "查詢車輛失敗")
		return
	}
	defer rows.Close()

	var list []model.Vehicle
	mileage := make(map[int64]int)
	for rows.Next() {
		v, km, err := scanVehicleListRow(rows)
		if err != nil {
			log.Printf("讀取車輛列表失敗: %v", err)
			fail(w, "讀取車輛列表失敗")
			return
		}
		list = append(list, v)
		mileage[v.ID] = km
	}
	if err := rows.Err(); err != nil {
		log.Printf("列舉車輛列表失敗: %v", err)
		fail(w, "讀取車輛列表失敗")
		return
	}

	var vehicleRows []pages.VehicleRow
	for _, v := range list {
		curKm := mileage[v.ID]
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
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := pages.VehicleSearchRows(nil).Render(r.Context(), w); err != nil {
			log.Printf("產生空車輛搜尋結果失敗: %v", err)
		}
		return
	}
	like := "%" + q + "%"
	rows, err := db.DB.Query(`SELECT `+vehicleListCols+` FROM active_vehicles v
	JOIN active_customers c ON c.id = v.customer_id
	`+vehicleListJoins+`
	WHERE v.plate_number LIKE ? OR v.brand_model LIKE ? OR c.name LIKE ? OR COALESCE(v.vin,'') LIKE ?
	ORDER BY v.id DESC LIMIT 50`, like, like, like, like)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var result []pages.VehicleRow
	for rows.Next() {
		v, km, err := scanVehicleListRow(rows)
		if err != nil {
			log.Printf("讀取車輛搜尋結果失敗: %v", err)
			http.Error(w, "讀取車輛搜尋結果失敗", http.StatusInternalServerError)
			return
		}
		dateCell, kmCell := maintDueText(v, km)
		result = append(result, pages.VehicleRow{
			ID: v.ID, PlateNumber: v.PlateNumber, BrandModel: v.BrandModel,
			VehicleTypeLabel: v.VehicleTypeLabel, ManufactureYear: v.ManufactureYear,
			CustomerName: v.CustomerName, DateCell: dateCell, KmCell: kmCell, CanEdit: true,
		})
	}
	if err := rows.Err(); err != nil {
		log.Printf("列舉車輛搜尋結果失敗: %v", err)
		http.Error(w, "讀取車輛搜尋結果失敗", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pages.VehicleSearchRows(result).Render(r.Context(), w); err != nil {
		log.Printf("產生車輛搜尋結果失敗: %v", err)
	}
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
			`</td><td><span class="pill ` + statusPill[status] + `">` + workOrderStatusLabel(r.Context(), status) +
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
