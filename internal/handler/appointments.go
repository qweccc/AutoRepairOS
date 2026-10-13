package handler

import (
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

// ListAppointments 顯示預約排程列表（SRS 5.9）。
func ListAppointments(w http.ResponseWriter, r *http.Request, user model.User) {
	qv := r.URL.Query()
	date := strings.TrimSpace(qv.Get("date"))
	status := strings.TrimSpace(qv.Get("status"))
	page := pageNo(r)
	size := pageSize(r)

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

	total := countRows(`SELECT COUNT(1) FROM appointments a`+where, args...)

	query := `SELECT a.id, a.customer_id, COALESCE(c.name,''), a.vehicle_id,
		COALESCE(v.plate_number,''), a.appointment_date, COALESCE(a.appointment_time,''),
		COALESCE(a.service_type,''), COALESCE(a.bay,''), a.status, COALESCE(a.notes,''),
		a.created_at, a.updated_at
		FROM appointments a
		JOIN customers c ON c.id = a.customer_id
		JOIN vehicles v ON v.id = a.vehicle_id` + where +
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

	var apptRows []pages.AppointmentRow
	for _, a := range list {
		bayLbl := ""
		if a.Bay != "" {
			bayLbl = bayLabel(a.Bay)
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
		})
	}

	// 工位總覽卡（有篩選日期時才顯示）
	bayCard := ""
	if date != "" && len(bayUsage) > 0 {
		var bb strings.Builder
		bb.WriteString(`<div class="card"><h2>工位占用（` + htmlEsc(date) + `）</h2><table><tr><th>工位</th><th>當日占用</th></tr>`)
		for _, b := range bayList {
			slots := bayUsage[b.code]
			if len(slots) == 0 {
				bb.WriteString(`<tr><td>` + b.label + `</td><td style="color:#86efac">空缺</td></tr>`)
				continue
			}
			bb.WriteString(`<tr><td>` + b.label + `</td><td style="font-size:12.5px">` +
				strings.Join(slots, "<br>") + `</td></tr>`)
		}
		for code, slots := range bayUsage {
			if bayKnown(code) {
				continue
			}
			bb.WriteString(`<tr><td>` + htmlEsc(code) + `（非標準）</td><td style="font-size:12.5px">` +
				strings.Join(slots, "<br>") + `</td></tr>`)
		}
		bb.WriteString(`</table></div>`)
		bayCard = bb.String()
	}

	pages.ListAppointmentsPage(apptRows, total, page, size, date, status,
		customerOptions(), vehicleOptions(), bayOptions(""), bayCard).Render(r.Context(), w)
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
		COALESCE(v.plate_number,''), a.appointment_date, COALESCE(a.appointment_time,''),
		COALESCE(a.service_type,''), COALESCE(a.bay,''), a.status, COALESCE(a.notes,''),
		a.created_at, a.updated_at
		FROM appointments a
		JOIN customers c ON c.id = a.customer_id
		JOIN vehicles v ON v.id = a.vehicle_id`+where+
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
	pages.AppointmentSearchRows(result).Render(r.Context(), w)
}

// CreateAppointment 建立預約（SRS 5.9）。
func CreateAppointment(w http.ResponseWriter, r *http.Request) {
	customerID := mustInt(r.FormValue("customer_id"))
	vehicleID := mustInt(r.FormValue("vehicle_id"))
	appointmentDate := strings.TrimSpace(r.FormValue("appointment_date"))
	appointmentTime := strings.TrimSpace(r.FormValue("appointment_time"))
	serviceType := strings.TrimSpace(r.FormValue("service_type"))
	bay := normBay(r.FormValue("bay"))
	notes := strings.TrimSpace(r.FormValue("notes"))

	if customerID == 0 || vehicleID == 0 || appointmentDate == "" {
		fail(w, "客戶、車輛、預約日期為必填")
		return
	}

	// 工位衝突（FR-APP-04）：同一工位同日期同時段只能一台車。
	// 已取消的預約不佔位（取消即釋放資源）。
	if cid, plate, cust, t, hit := bayConflict(bay, appointmentDate, appointmentTime, 0); hit {
		fail(w, "工位衝突："+bayLabel(bay)+" 在 "+appointmentDate+" "+t+
			" 已被 "+plate+"（"+cust+"，預約 #"+strconv.FormatInt(cid, 10)+
			"）占用。請改時段、改工位，或先取消該預約。")
		return
	}

	_, err := db.DB.Exec(`INSERT INTO appointments
		(customer_id, vehicle_id, appointment_date, appointment_time, service_type, bay, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		customerID, vehicleID, appointmentDate, appointmentTime, serviceType, bay, notes)
	if err != nil {
		fail(w, "建立預約失敗")
		return
	}

	redirectMsg(w, r, "/appointments", "預約已建立")
}

// EditAppointmentForm 顯示編輯預約表單（SRS 5.9）。
func EditAppointmentForm(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))

	var a model.Appointment
	err := db.DB.QueryRow(`SELECT a.id, a.customer_id, COALESCE(c.name,''), a.vehicle_id,
		COALESCE(v.plate_number,''), a.appointment_date, COALESCE(a.appointment_time,''),
		COALESCE(a.service_type,''), COALESCE(a.bay,''), a.status, COALESCE(a.notes,'')
		FROM appointments a
		JOIN customers c ON c.id = a.customer_id
		JOIN vehicles v ON v.id = a.vehicle_id
		WHERE a.id = ?`, id).
		Scan(&a.ID, &a.CustomerID, &a.CustomerName, &a.VehicleID,
			&a.PlateNumber, &a.AppointmentDate, &a.AppointmentTime,
			&a.ServiceType, &a.Bay, &a.Status, &a.Notes)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	body := `
    <div class="card">
      <h2>編輯預約 #` + strconv.FormatInt(a.ID, 10) + `</h2>
      <form class="frm" method="POST" action="/appointments/` + strconv.FormatInt(a.ID, 10) + `">
        <label>客戶
          <select name="customer_id" required>` + customerOptions() + `</select>
        </label>
        <label>車輛
          <select name="vehicle_id" required>` + vehicleOptions() + `</select>
        </label>
        <label>預約日期<input type="date" name="appointment_date" value="` + htmlEsc(a.AppointmentDate) + `" required></label>
        <label>時段<input name="appointment_time" value="` + htmlEsc(a.AppointmentTime) + `"></label>
        <label>工位<select name="bay">` + bayOptions(a.Bay) + `</select></label>
        <label>服務內容<input name="service_type" value="` + htmlEsc(a.ServiceType) + `"></label>
        <label>狀態
          <select name="status">
            <option value="booked"` + selIf(a.Status == "booked") + `>已預約</option>
            <option value="confirmed"` + selIf(a.Status == "confirmed") + `>已確認</option>
            <option value="arrived"` + selIf(a.Status == "arrived") + `>已進廠</option>
            <option value="completed"` + selIf(a.Status == "completed") + `>已完成</option>
            <option value="cancelled"` + selIf(a.Status == "cancelled") + `>已取消</option>
          </select>
        </label>
        <label class="full">備註<textarea name="notes">` + htmlEsc(a.Notes) + `</textarea></label>
        <div class="full"><button class="btn btn-p" type="submit">儲存</button> <a class="btn btn-s" href="/appointments">取消</a></div>
      </form>
    </div>`

	renderPage(w, "編輯預約", "/appointments", user.Username, user.Role, body)
}

// UpdateAppointment 更新預約（SRS 5.9）。
func UpdateAppointment(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	customerID := mustInt(r.FormValue("customer_id"))
	vehicleID := mustInt(r.FormValue("vehicle_id"))
	appointmentDate := strings.TrimSpace(r.FormValue("appointment_date"))
	appointmentTime := strings.TrimSpace(r.FormValue("appointment_time"))
	serviceType := strings.TrimSpace(r.FormValue("service_type"))
	bay := normBay(r.FormValue("bay"))
	status := strings.TrimSpace(r.FormValue("status"))
	notes := strings.TrimSpace(r.FormValue("notes"))

	if customerID == 0 || vehicleID == 0 || appointmentDate == "" {
		fail(w, "客戶、車輛、預約日期為必填")
		return
	}

	// 工位衝突：excludeID 傳自己的 id，否則編輯自己的預約會被自己擋下。
	if cid, plate, cust, t, hit := bayConflict(bay, appointmentDate, appointmentTime, id); hit {
		fail(w, "工位衝突："+bayLabel(bay)+" 在 "+appointmentDate+" "+t+
			" 已被 "+plate+"（"+cust+"，預約 #"+strconv.FormatInt(cid, 10)+
			"）占用。請改時段、改工位，或先取消該預約。")
		return
	}

	res, err := db.DB.Exec(`UPDATE appointments SET customer_id=?, vehicle_id=?,
		appointment_date=?, appointment_time=?, service_type=?, bay=?, status=?, notes=?,
		updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		customerID, vehicleID, appointmentDate, appointmentTime, serviceType, bay, status, notes, id)
	if err != nil {
		fail(w, "更新預約失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}

	redirectMsg(w, r, "/appointments", "預約已更新")
}

// DeleteAppointment 刪除預約（SRS 5.9）。
func DeleteAppointment(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	res, err := db.DB.Exec("DELETE FROM appointments WHERE id = ?", id)
	if err != nil {
		fail(w, "刪除預約失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}

	redirectMsg(w, r, "/appointments", "預約已刪除")
}

func appointmentLabel(status string) string {
	switch status {
	case "booked":
		return "已預約"
	case "confirmed":
		return "已確認"
	case "arrived":
		return "已進廠"
	case "completed":
		return "已完成"
	case "cancelled":
		return "已取消"
	default:
		return status
	}
}

func appointmentPill(status string) string {
	switch status {
	case "booked":
		return "p-pending"
	case "confirmed":
		return "p-progress"
	case "arrived":
		return "p-waitparts"
	case "completed":
		return "p-completed"
	case "cancelled":
		return "p-apprno"
	default:
		return "p-pending"
	}
}
