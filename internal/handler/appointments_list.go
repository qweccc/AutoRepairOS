package handler

import (
	"net/http"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

// ListAppointments 顯示預約排程列表（SRS 5.9）。
func ListAppointments(w http.ResponseWriter, r *http.Request, user model.User) {
	qv := r.URL.Query()
	date := strings.TrimSpace(qv.Get("date"))
	status := strings.TrimSpace(qv.Get("status"))
	calendarDate := normalizeCalendarDate(qv.Get("calendar_date"))
	if calendarDate == "" {
		calendarDate = normalizeCalendarDate(date)
	}
	if calendarDate == "" {
		calendarDate = taipeiToday()
	}
	calendarView := normalizeCalendarView(qv.Get("view"))
	page := pageNo(r)
	size := pageSize(r)
	knownBays := bayList()

	where := ` WHERE 1=1`
	var args []interface{}
	if date != "" {
		where += ` AND a.appointment_date = ?`
		args = append(args, date)
	}
	if status != "" {
		where += ` AND a.status = ?`
		args = append(args, status)
	}

	total := countRows(`SELECT COUNT(1) FROM active_appointments a`+where, args...)

	query := `SELECT a.id, a.customer_id, COALESCE(c.name,''), a.vehicle_id,
		COALESCE(v.plate_number,''), CAST(a.appointment_date AS TEXT), COALESCE(a.appointment_time,''),
		COALESCE(a.service_type,''), COALESCE(a.bay,''), a.status, COALESCE(a.notes,''),
		a.created_at, a.updated_at
		FROM active_appointments a
		JOIN active_customers c ON c.id = a.customer_id
		JOIN active_vehicles v ON v.id = a.vehicle_id` + where +
		` ORDER BY a.appointment_date DESC, a.appointment_time ASC LIMIT ? OFFSET ?`
	pageArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := db.DB.Query(query, pageArgs...)
	if err != nil {
		fail(w, "查詢預約失敗")
		return
	}
	defer rows.Close()

	var list []model.Appointment
	for rows.Next() {
		var a model.Appointment
		var createdAt, updatedAt string
		if err := rows.Scan(&a.ID, &a.CustomerID, &a.CustomerName, &a.VehicleID,
			&a.PlateNumber, &a.AppointmentDate, &a.AppointmentTime,
			&a.ServiceType, &a.Bay, &a.Status, &a.Notes, &createdAt, &updatedAt); err != nil {
			continue
		}
		a.CreatedAt = parseFlexTime(createdAt)
		a.UpdatedAt = parseFlexTime(updatedAt)
		list = append(list, a)
	}

	// 當天工位佔用總覽（一次查詢，避免每列一次 N+1）
	bayUsage := baySchedule(date)

	// 哪些預約已經有工單（一次查完，避免逐列 N+1）
	ids := make([]int64, 0, len(list))
	for _, a := range list {
		ids = append(ids, a.ID)
	}
	woIDs := appointmentWorkOrderIDs(ids)

	var apptRows []pages.AppointmentRow
	for _, a := range list {
		bayLbl := ""
		if a.Bay != "" {
			bayLbl = bayLabelFrom(a.Bay, knownBays)
		}
		apptRows = append(apptRows, pages.AppointmentRow{
			ID:              a.ID,
			AppointmentDate: a.AppointmentDate,
			AppointmentTime: a.AppointmentTime,
			Bay:             a.Bay,
			BayLabel:        bayLbl,
			PlateNumber:     a.PlateNumber,
			CustomerName:    a.CustomerName,
			ServiceType:     a.ServiceType,
			Status:          a.Status,
			StatusLabel:     appointmentLabel(a.Status),
			StatusPill:      appointmentPill(a.Status),
			HasWorkOrder:    woIDs[a.ID],
		})
	}

	// 工位總覽卡（有篩選日期時才顯示）
	var baySummary []pages.BaySummary
	if date != "" && len(bayUsage) > 0 {
		for _, b := range bayList() {
			slots := bayUsage[b.code]
			baySummary = append(baySummary, pages.BaySummary{Label: b.label, Slots: slots})
		}
		for code, slots := range bayUsage {
			if bayKnownFrom(code, knownBays) {
				continue
			}
			baySummary = append(baySummary, pages.BaySummary{Label: code + "（非標準）", Slots: slots})
		}
	}

	calendarDays, err := appointmentCalendar(calendarDate, calendarView, status)
	if err != nil {
		fail(w, "查詢預約行事曆失敗")
		return
	}
	pages.ListAppointmentsPage(apptRows, total, page, size, date, status,
		calendarDate, calendarView, calendarDays,
		bayOptions(""), baySummary, date,
		user).Render(r.Context(), w)
}

// AppointmentCalendarAPI 回傳行事曆片段，供日／週切換與日期導覽透過 HTMX 更新。
func AppointmentCalendarAPI(w http.ResponseWriter, r *http.Request, _ model.User) {
	date := normalizeCalendarDate(r.URL.Query().Get("date"))
	if date == "" {
		date = taipeiToday()
	}
	view := normalizeCalendarView(r.URL.Query().Get("view"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	days, err := appointmentCalendar(date, view, status)
	if err != nil {
		http.Error(w, "查詢預約行事曆失敗", http.StatusInternalServerError)
		return
	}
	pages.AppointmentCalendar(date, view, status, days).Render(r.Context(), w)
}

func normalizeCalendarDate(value string) string {
	value = strings.TrimSpace(value)
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return ""
	}
	return value
}

func normalizeCalendarView(view string) string {
	if view == "week" {
		return view
	}
	return "day"
}

func taipeiToday() string {
	return time.Now().In(time.FixedZone("Asia/Taipei", 8*60*60)).Format("2006-01-02")
}

func appointmentCalendar(date, view, status string) ([]pages.CalendarDay, error) {
	selected, err := time.Parse("2006-01-02", date)
	if err != nil {
		return nil, err
	}
	if view == "week" {
		selected = selected.AddDate(0, 0, -((int(selected.Weekday()) + 6) % 7))
	}
	end := selected.AddDate(0, 0, 1)
	if view == "week" {
		end = selected.AddDate(0, 0, 7)
	}

	where := ` WHERE a.appointment_date >= ? AND a.appointment_date < ?`
	args := []interface{}{selected.Format("2006-01-02"), end.Format("2006-01-02")}
	if status != "" {
		where += ` AND a.status = ?`
		args = append(args, status)
	}
	knownBays := bayList()
	rows, err := db.DB.Query(`SELECT a.id, CAST(a.appointment_date AS TEXT),
		COALESCE(a.appointment_time,''), UPPER(COALESCE(a.bay,'')),
		COALESCE(v.plate_number,''), COALESCE(c.name,''),
		COALESCE(a.service_type,''), a.status
		FROM active_appointments a
		JOIN active_customers c ON c.id=a.customer_id
		JOIN active_vehicles v ON v.id=a.vehicle_id`+where+
		` ORDER BY a.appointment_date, a.appointment_time, a.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byDate := make(map[string][]pages.AppointmentRow)
	for rows.Next() {
		var row pages.AppointmentRow
		if err := rows.Scan(&row.ID, &row.AppointmentDate, &row.AppointmentTime,
			&row.Bay, &row.PlateNumber, &row.CustomerName, &row.ServiceType, &row.Status); err != nil {
			return nil, err
		}
		row.BayLabel = ""
		if row.Bay != "" {
			row.BayLabel = bayLabelFrom(row.Bay, knownBays)
		}
		row.StatusLabel = appointmentLabel(row.Status)
		row.StatusPill = appointmentPill(row.Status)
		byDate[row.AppointmentDate] = append(byDate[row.AppointmentDate], row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	count := 1
	if view == "week" {
		count = 7
	}
	days := make([]pages.CalendarDay, 0, count)
	for i := 0; i < count; i++ {
		day := selected.AddDate(0, 0, i)
		dateValue := day.Format("2006-01-02")
		days = append(days, pages.CalendarDay{
			Date: dateValue, Label: dateValue + " " + []string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}[day.Weekday()],
			Appointments: byDate[dateValue],
		})
	}
	return days, nil
}

// SearchAppointmentsAPI 預約即時搜尋 API（HTMX 用）。
func SearchAppointmentsAPI(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (v.plate_number LIKE ? OR c.name LIKE ? OR COALESCE(a.service_type,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	rows, err := db.DB.Query(`SELECT a.id, a.customer_id, COALESCE(c.name,''), a.vehicle_id,
		COALESCE(v.plate_number,''), CAST(a.appointment_date AS TEXT), COALESCE(a.appointment_time,''),
		COALESCE(a.service_type,''), COALESCE(a.bay,''), a.status, COALESCE(a.notes,''),
		a.created_at, a.updated_at
		FROM active_appointments a
		JOIN active_customers c ON c.id = a.customer_id
		JOIN active_vehicles v ON v.id = a.vehicle_id`+where+
		` ORDER BY a.appointment_date DESC, a.appointment_time ASC LIMIT 50`, args...)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var result []pages.AppointmentRow
	for rows.Next() {
		var a model.Appointment
		var createdAt, updatedAt string
		if err := rows.Scan(&a.ID, &a.CustomerID, &a.CustomerName, &a.VehicleID,
			&a.PlateNumber, &a.AppointmentDate, &a.AppointmentTime,
			&a.ServiceType, &a.Bay, &a.Status, &a.Notes, &createdAt, &updatedAt); err != nil {
			continue
		}
		bayLbl := ""
		if a.Bay != "" {
			bayLbl = bayLabel(a.Bay)
		}
		result = append(result, pages.AppointmentRow{
			ID:              a.ID,
			AppointmentDate: a.AppointmentDate,
			AppointmentTime: a.AppointmentTime,
			Bay:             a.Bay,
			BayLabel:        bayLbl,
			PlateNumber:     a.PlateNumber,
			CustomerName:    a.CustomerName,
			ServiceType:     a.ServiceType,
			Status:          a.Status,
			StatusLabel:     appointmentLabel(a.Status),
			StatusPill:      appointmentPill(a.Status),
		})
	}
	// 補上「是否已有工單」（批次查詢，避免 N+1）
	if len(result) > 0 {
		sids := make([]int64, 0, len(result))
		for _, r := range result {
			sids = append(sids, r.ID)
		}
		woMap := appointmentWorkOrderIDs(sids)
		for i := range result {
			result[i].HasWorkOrder = woMap[result[i].ID]
		}
	}
	pages.AppointmentSearchRows(result).Render(r.Context(), w)
}
