package handler

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

// 車輛投保紀錄與定檢排程。
//
// 這兩塊是車行的固定業務，舊系統各有一張表帶著歷史資料：
//   Car Insurance List  2,667 筆  投保紀錄（險種、發照日、卡號）
//   Car Check List     21,176 筆  定檢排程（排定日、完成日）
// 先前遷移只做營運主流程，這兩張漏掉，因此車主問「我上次保到什麼時候」
// 或「下次驗車何時」時查不到。

const insuranceClassLabel = "保險險種"

// ListVehicleInsurances 投保紀錄列表。
func ListVehicleInsurances(w http.ResponseWriter, r *http.Request, user model.User) {
	qv := r.URL.Query()
	q := strings.TrimSpace(qv.Get("q"))
	company := strings.TrimSpace(qv.Get("company"))
	page := pageNo(r)
	size := pageSize(r)

	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (v.plate_number LIKE ? OR c.name LIKE ? OR COALESCE(vi.card_no,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	if company != "" {
		where += ` AND vi.company_no = ?`
		args = append(args, company)
	}

	from := ` FROM active_vehicle_insurances vi
		JOIN active_vehicles v ON v.id = vi.vehicle_id
		JOIN active_customers c ON c.id = v.customer_id
		LEFT JOIN insurance_companies ic ON ic.id = vi.insurance_company_id`

	total := countRows(`SELECT COUNT(1)`+from+where, args...)

	query := `SELECT vi.id, vi.vehicle_id, v.plate_number, COALESCE(vi.insurance_company_id,0),
		COALESCE(ic.name, vi.company_no, ''), COALESCE(vi.company_no,''),
		COALESCE(vi.insurance_class,''), COALESCE(vi.insurance_class_name,''),
		COALESCE(vi.license_date,''), COALESCE(vi.card_no,''), COALESCE(vi.amount_raw,''),
		COALESCE(vi.notes,''), COALESCE(c.name,'')
		` + from + where + ` ORDER BY vi.license_date DESC, vi.id DESC LIMIT ? OFFSET ?`
	pageArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := db.DB.Query(query, pageArgs...)
	if err != nil {
		fail(w, "查詢投保紀錄失敗")
		return
	}
	defer rows.Close()

	var insRows []pages.VehicleInsuranceRow
	for rows.Next() {
		var id, vid, compID int64
		var plate, compName, compNo, cls, clsName, date, card, amount, notes, custName string
		if err := rows.Scan(&id, &vid, &plate, &compID, &compName, &compNo,
			&cls, &clsName, &date, &card, &amount, &notes, &custName); err != nil {
			continue
		}
		insRows = append(insRows, pages.VehicleInsuranceRow{
			ID:        id,
			VehicleID: vid,
			Plate:     plate,
			Customer:  custName,
			Company:   compName,
			ClassName: clsName,
			Class:     cls,
			Date:      normDate(date),
			CardNo:    card,
			Amount:    amount,
		})
	}

	// 保險公司篩選籤
	var compTabs strings.Builder
	compTabs.WriteString(`<a class="btn ` + btnIf(company == "", "btn-p", "btn-s") + `" href="/insurances">全部</a> `)
	crows, cerr := db.DB.Query(`SELECT COALESCE(ic.name, vi.company_no, '(未知)') nm, COUNT(1) n
		FROM active_vehicle_insurances vi
		LEFT JOIN insurance_companies ic ON ic.id = vi.insurance_company_id
		GROUP BY nm ORDER BY n DESC LIMIT 12`)
	if cerr == nil {
		defer crows.Close()
		for crows.Next() {
			var nm string
			var cnt int
			if crows.Scan(&nm, &cnt) != nil {
				continue
			}
			u := url.Values{}
			u.Set("company", nm)
			compTabs.WriteString(`<a class="btn ` + btnIf(company == nm, "btn-p", "btn-s") + `" href="/insurances?` +
				u.Encode() + `">` + htmlEsc(nm) + ` ` + strconv.Itoa(cnt) + `</a> `)
		}
	}

	pages.ListVehicleInsurancesPage(insRows, total, page, size, q, company,
		compTabs.String()).Render(r.Context(), w)
}

// SearchInsurancesAPI 車輛投保即時搜尋 API（HTMX 用）。
func SearchInsurancesAPI(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	from := ` FROM active_vehicle_insurances vi
		JOIN active_vehicles v ON v.id = vi.vehicle_id
		JOIN active_customers c ON c.id = v.customer_id
		LEFT JOIN insurance_companies ic ON ic.id = vi.insurance_company_id`
	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (v.plate_number LIKE ? OR c.name LIKE ? OR COALESCE(vi.card_no,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	rows, err := db.DB.Query(`SELECT vi.id, vi.vehicle_id, v.plate_number, COALESCE(vi.insurance_company_id,0),
		COALESCE(ic.name, vi.company_no, ''), COALESCE(vi.company_no,''),
		COALESCE(vi.insurance_class,''), COALESCE(vi.insurance_class_name,''),
		COALESCE(vi.license_date,''), COALESCE(vi.card_no,''), COALESCE(vi.amount_raw,''),
		COALESCE(vi.notes,''), COALESCE(c.name,'')`+from+where+
		` ORDER BY vi.license_date DESC, vi.id DESC LIMIT 50`, args...)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var result []pages.VehicleInsuranceRow
	for rows.Next() {
		var id, vid, compID int64
		var plate, compName, compNo, cls, clsName, date, card, amount, notes, custName string
		if err := rows.Scan(&id, &vid, &plate, &compID, &compName, &compNo,
			&cls, &clsName, &date, &card, &amount, &notes, &custName); err != nil {
			continue
		}
		result = append(result, pages.VehicleInsuranceRow{
			ID:        id,
			VehicleID: vid,
			Plate:     plate,
			Customer:  custName,
			Company:   compName,
			ClassName: clsName,
			Class:     cls,
			Date:      normDate(date),
			CardNo:    card,
			Amount:    amount,
		})
	}
	pages.VehicleInsuranceSearchRows(result).Render(r.Context(), w)
}

// ListVehicleInspections 定檢排程列表。
func ListVehicleInspections(w http.ResponseWriter, r *http.Request, user model.User) {
	qv := r.URL.Query()
	q := strings.TrimSpace(qv.Get("q"))
	pendingOnly := qv.Get("pending") == "1"
	page := pageNo(r)
	size := pageSize(r)

	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (v.plate_number LIKE ? OR c.name LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	if pendingOnly {
		where += ` AND COALESCE(vi.check_date,'') = ''`
	}

	from := ` FROM active_vehicle_inspections vi
		JOIN active_vehicles v ON v.id = vi.vehicle_id
		JOIN active_customers c ON c.id = v.customer_id`

	total := countRows(`SELECT COUNT(1)`+from+where, args...)
	pendingCount := countRows(`SELECT COUNT(1) FROM active_vehicle_inspections WHERE COALESCE(check_date,'')=''`)

	query := `SELECT vi.id, vi.vehicle_id, v.plate_number, COALESCE(vi.order_date,''),
		COALESCE(vi.check_date,''), COALESCE(vi.firm_no,''), COALESCE(vi.notes,''),
		COALESCE(c.name,'')
		` + from + where + ` ORDER BY vi.order_date DESC, vi.id DESC LIMIT ? OFFSET ?`
	pageArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := db.DB.Query(query, pageArgs...)
	if err != nil {
		fail(w, "查詢定檢排程失敗")
		return
	}
	defer rows.Close()

	today := today()
	var inspRows []pages.InspectionRow
	for rows.Next() {
		var id, vid int64
		var plate, orderDate, checkDate, firmNo, notes, custName string
		if err := rows.Scan(&id, &vid, &plate, &orderDate, &checkDate, &firmNo, &notes, &custName); err != nil {
			continue
		}
		statusPill, statusLabel := "p-completed", "已完成"
		if checkDate == "" {
			if od := normDate(orderDate); od != "" && od < today {
				statusPill, statusLabel = "p-apprno", "已逾期"
			} else {
				statusPill, statusLabel = "p-waitparts", "待檢驗"
			}
		}
		inspRows = append(inspRows, pages.InspectionRow{
			ID:          id,
			VehicleID:   vid,
			Plate:       plate,
			Customer:    custName,
			OrderDate:   normDate(orderDate),
			CheckDate:   normDate(checkDate),
			StatusPill:  statusPill,
			StatusLabel: statusLabel,
		})
	}

	pages.ListVehicleInspectionsPage(inspRows, total, page, size, q, pendingOnly,
		pendingCount).Render(r.Context(), w)
}

// SearchInspectionsAPI 定檢排程即時搜尋 API（HTMX 用）。
func SearchInspectionsAPI(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	from := ` FROM active_vehicle_inspections vi
		JOIN active_vehicles v ON v.id = vi.vehicle_id
		JOIN active_customers c ON c.id = v.customer_id`
	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (v.plate_number LIKE ? OR c.name LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	rows, err := db.DB.Query(`SELECT vi.id, vi.vehicle_id, v.plate_number, COALESCE(vi.order_date,''),
		COALESCE(vi.check_date,''), COALESCE(vi.firm_no,''), COALESCE(vi.notes,''),
		COALESCE(c.name,'')`+from+where+
		` ORDER BY vi.order_date DESC, vi.id DESC LIMIT 50`, args...)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	todayStr := today()
	var result []pages.InspectionRow
	for rows.Next() {
		var id, vid int64
		var plate, orderDate, checkDate, firmNo, notes, custName string
		if err := rows.Scan(&id, &vid, &plate, &orderDate, &checkDate, &firmNo, &notes, &custName); err != nil {
			continue
		}
		statusPill, statusLabel := "p-completed", "已完成"
		if checkDate == "" {
			if od := normDate(orderDate); od != "" && od < todayStr {
				statusPill, statusLabel = "p-apprno", "已逾期"
			} else {
				statusPill, statusLabel = "p-waitparts", "待檢驗"
			}
		}
		result = append(result, pages.InspectionRow{
			ID:          id,
			VehicleID:   vid,
			Plate:       plate,
			Customer:    custName,
			OrderDate:   normDate(orderDate),
			CheckDate:   normDate(checkDate),
			StatusPill:  statusPill,
			StatusLabel: statusLabel,
		})
	}
	pages.VehicleInspectionSearchRows(result).Render(r.Context(), w)
}
