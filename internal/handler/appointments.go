package handler

import (
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
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
		COALESCE(a.service_type,''), a.status, COALESCE(a.notes,''),
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
			&a.ServiceType, &a.Status, &a.Notes, &createdAt, &updatedAt); err != nil {
			continue
		}
		a.CreatedAt = parseFlexTime(createdAt)
		a.UpdatedAt = parseFlexTime(updatedAt)
		list = append(list, a)
	}

	var tb strings.Builder
	tb.WriteString(`<table><tr><th>ID</th><th>日期</th><th>時段</th><th>車牌</th><th>客戶</th><th>服務內容</th><th>狀態</th><th>操作</th></tr>`)
	for _, a := range list {
		tb.WriteString(`<tr><td>` + strconv.FormatInt(a.ID, 10) + `</td><td>` + htmlEsc(a.AppointmentDate) +
			`</td><td>` + htmlEsc(a.AppointmentTime) + `</td><td>` + htmlEsc(a.PlateNumber) +
			`</td><td>` + htmlEsc(a.CustomerName) + `</td><td>` + htmlEsc(a.ServiceType) +
			`</td><td><span class="pill ` + appointmentPill(a.Status) + `">` + appointmentLabel(a.Status) + `</span>` +
			`</td><td><a class="btn btn-s" href="/appointments/` + strconv.FormatInt(a.ID, 10) + `/edit">編輯</a> ` +
			`<a class="btn btn-d" href="/appointments/` + strconv.FormatInt(a.ID, 10) + `/delete" onclick="return confirm('確定刪除這筆預約？')">刪除</a></td></tr>`)
	}
	tb.WriteString(`</table>`)

	table := tb.String()
	if len(list) == 0 {
		table = `<div class="empty">沒有符合條件的預約</div>`
	}

	body := flash(r) + `
    <div class="card">
      <h2>新增預約</h2>
      <form class="frm" method="POST" action="/appointments">
        <label>客戶
          <select name="customer_id" required>
            <option value="">請選擇客戶</option>` + customerOptions() + `
          </select>
        </label>
        <label>車輛
          <select name="vehicle_id" required>
            <option value="">請選擇車輛</option>` + vehicleOptions() + `
          </select>
        </label>
        <label>預約日期<input type="date" name="appointment_date" required></label>
        <label>時段<input name="appointment_time" placeholder="例：09:00"></label>
        <label>服務內容<input name="service_type" placeholder="例：定期保養"></label>
        <label class="full">備註<textarea name="notes"></textarea></label>
        <div class="full"><button class="btn btn-p" type="submit">建立預約</button></div>
      </form>
    </div>
    <div class="card">
      <h2>預約列表（共 ` + strconv.Itoa(total) + ` 筆）</h2>
      <form method="GET" action="/appointments" style="margin-bottom:12px">
        <input type="date" name="date" value="` + htmlEsc(date) + `" style="padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0">
        <select name="status" style="padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0">
          <option value="">全部狀態</option>
          <option value="booked"` + selIf(status == "booked") + `>已預約</option>
          <option value="confirmed"` + selIf(status == "confirmed") + `>已確認</option>
          <option value="arrived"` + selIf(status == "arrived") + `>已進廠</option>
          <option value="completed"` + selIf(status == "completed") + `>已完成</option>
          <option value="cancelled"` + selIf(status == "cancelled") + `>已取消</option>
        </select>
        <button class="btn btn-p" type="submit">篩選</button>
        <a class="btn btn-s" href="/appointments">清除</a>
      </form>` + table + pager("/appointments", qv, total, page, size) + `
    </div>`

	renderPage(w, "預約排程", "/appointments", user.Username, user.Role, body)
}

// CreateAppointment 建立預約（SRS 5.9）。
func CreateAppointment(w http.ResponseWriter, r *http.Request) {
	customerID := mustInt(r.FormValue("customer_id"))
	vehicleID := mustInt(r.FormValue("vehicle_id"))
	appointmentDate := strings.TrimSpace(r.FormValue("appointment_date"))
	appointmentTime := strings.TrimSpace(r.FormValue("appointment_time"))
	serviceType := strings.TrimSpace(r.FormValue("service_type"))
	notes := strings.TrimSpace(r.FormValue("notes"))

	if customerID == 0 || vehicleID == 0 || appointmentDate == "" {
		fail(w, "客戶、車輛、預約日期為必填")
		return
	}

	_, err := db.DB.Exec(`INSERT INTO appointments
		(customer_id, vehicle_id, appointment_date, appointment_time, service_type, notes)
		VALUES (?, ?, ?, ?, ?, ?)`,
		customerID, vehicleID, appointmentDate, appointmentTime, serviceType, notes)
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
		COALESCE(a.service_type,''), a.status, COALESCE(a.notes,'')
		FROM appointments a
		JOIN customers c ON c.id = a.customer_id
		JOIN vehicles v ON v.id = a.vehicle_id
		WHERE a.id = ?`, id).
		Scan(&a.ID, &a.CustomerID, &a.CustomerName, &a.VehicleID,
			&a.PlateNumber, &a.AppointmentDate, &a.AppointmentTime,
			&a.ServiceType, &a.Status, &a.Notes)
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
	status := strings.TrimSpace(r.FormValue("status"))
	notes := strings.TrimSpace(r.FormValue("notes"))

	if customerID == 0 || vehicleID == 0 || appointmentDate == "" {
		fail(w, "客戶、車輛、預約日期為必填")
		return
	}

	res, err := db.DB.Exec(`UPDATE appointments SET customer_id=?, vehicle_id=?,
		appointment_date=?, appointment_time=?, service_type=?, status=?, notes=?,
		updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		customerID, vehicleID, appointmentDate, appointmentTime, serviceType, status, notes, id)
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
