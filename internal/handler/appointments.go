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

// CreateAppointment 建立預約（SRS 5.9）。
func CreateAppointment(w http.ResponseWriter, r *http.Request) {
	customerID := mustInt(r.FormValue("customer_id"))
	vehicleID := mustInt(r.FormValue("vehicle_id"))
	appointmentDate := strings.TrimSpace(r.FormValue("appointment_date"))
	appointmentTime := strings.TrimSpace(r.FormValue("appointment_time"))
	serviceType := strings.TrimSpace(r.FormValue("service_type"))
	bay := normBay(r.FormValue("bay"))
	notes := strings.TrimSpace(r.FormValue("notes"))

	if customerID == 0 || vehicleID == 0 || !validAppointmentDate(appointmentDate) {
		formError(w, r, "新增預約", "/appointments", "客戶、車輛、預約日期為必填")
		return
	}
	if !appointmentVehicleBelongsToCustomer(vehicleID, customerID) {
		formError(w, r, "新增預約", "/appointments", "所選車輛不屬於所選客戶，請重新選擇")
		return
	}
	if !validAppointmentTime(appointmentTime) || (bay != "" && appointmentTime == "") {
		formError(w, r, "新增預約", "/appointments", "請輸入有效的預約時間（HH:MM）；指定工位時必須設定時間")
		return
	}

	// 工位區間採起始時間後 60 分鐘的半開區間；取消狀態不占位。
	// 已取消的預約不佔位（取消即釋放資源）。
	if cid, plate, cust, t, hit := bayConflict(bay, appointmentDate, appointmentTime, 0); hit {
		fail(w, bayConflictMessage(bay, appointmentDate, appointmentTime)+
			" 衝突預約："+t+" "+plate+"（"+cust+"，#"+strconv.FormatInt(cid, 10)+"）。")
		return
	}

	_, err := db.DB.Exec(`INSERT INTO appointments
		(customer_id, vehicle_id, appointment_date, appointment_time, service_type, bay, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		customerID, vehicleID, appointmentDate, appointmentTime, serviceType, bay, notes)
	if err != nil {
		if strings.Contains(err.Error(), "appointment_bay_overlap") {
			fail(w, bayConflictMessage(bay, appointmentDate, appointmentTime))
			return
		}
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
		FROM active_appointments a
		JOIN active_customers c ON c.id = a.customer_id
		JOIN active_vehicles v ON v.id = a.vehicle_id
		WHERE a.id = ?`, id).
		Scan(&a.ID, &a.CustomerID, &a.CustomerName, &a.VehicleID,
			&a.PlateNumber, &a.AppointmentDate, &a.AppointmentTime,
			&a.ServiceType, &a.Bay, &a.Status, &a.Notes)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	bayOpts, err := bayOptionsHTML(a.Bay)
	if err != nil {
		fail(w, "載入工位選項失敗")
		return
	}

	pages.EditAppointmentPage(a, bayOpts, user).Render(r.Context(), w)
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

	if customerID == 0 || vehicleID == 0 || !validAppointmentDate(appointmentDate) {
		formError(w, r, "編輯預約", "/appointments/"+strconv.FormatInt(id, 10)+"/edit", "客戶、車輛、預約日期為必填")
		return
	}
	if !appointmentVehicleBelongsToCustomer(vehicleID, customerID) {
		formError(w, r, "編輯預約", "/appointments/"+strconv.FormatInt(id, 10)+"/edit", "所選車輛不屬於所選客戶，請重新選擇")
		return
	}
	if !validAppointmentTime(appointmentTime) || (bay != "" && appointmentTime == "") {
		formError(w, r, "編輯預約", "/appointments/"+strconv.FormatInt(id, 10)+"/edit", "請輸入有效的預約時間（HH:MM）；指定工位時必須設定時間")
		return
	}

	// 工位衝突：excludeID 傳自己的 id，否則編輯自己的預約會被自己擋下。
	if status != "cancelled" {
		if cid, plate, cust, t, hit := bayConflict(bay, appointmentDate, appointmentTime, id); hit {
			fail(w, bayConflictMessage(bay, appointmentDate, appointmentTime)+
				" 衝突預約："+t+" "+plate+"（"+cust+"，#"+strconv.FormatInt(cid, 10)+"）。")
			return
		}
	}

	res, err := db.DB.Exec(`UPDATE appointments SET customer_id=?, vehicle_id=?,
		appointment_date=?, appointment_time=?, service_type=?, bay=?, status=?, notes=?,
		updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		customerID, vehicleID, appointmentDate, appointmentTime, serviceType, bay, status, notes, id)
	if err != nil {
		if strings.Contains(err.Error(), "appointment_bay_overlap") {
			fail(w, bayConflictMessage(bay, appointmentDate, appointmentTime))
			return
		}
		fail(w, "更新預約失敗")
		return
	}

	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}

	redirectMsg(w, r, "/appointments", "預約已更新")
}

func appointmentVehicleBelongsToCustomer(vehicleID, customerID int64) bool {
	var ownerID int64
	return db.DB.QueryRow(`SELECT customer_id FROM active_vehicles WHERE id = ?`, vehicleID).Scan(&ownerID) == nil &&
		ownerID == customerID
}

func validAppointmentTime(value string) bool {
	if value == "" {
		return true
	}
	parsed, err := time.Parse("15:04", value)
	return err == nil && parsed.Format("15:04") == value
}

func validAppointmentDate(value string) bool {
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

func bayConflictMessage(bay, date, timeSlot string) string {
	return "工位衝突：" + bayLabel(bay) + " 在 " + date + " " + timeSlot +
		" 的 60 分鐘時段與其他預約重疊。請改時段、改工位，或先取消該預約。"
}

// DeleteAppointment 刪除預約（SRS 5.9）。
func DeleteAppointment(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	res, err := db.DB.Exec("UPDATE appointments SET deleted_at = datetime('now') WHERE id = ? AND deleted_at IS NULL", id)
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

// appointmentWorkOrderIDs 回傳「已經有工單」的預約 id 集合。
//
// 為什麼一次查完：預約列表一頁可能 50 筆，逐筆查會變成 N+1。
// 回傳 map 讓呼叫端 O(1) 查詢；空集合時不發 SQL（SQLite 的 IN () 語法無效）。
// 查失敗時回空集合（按鈕不出現）而不是讓整個預約列表失敗——
// 預約資料本身是完整的，不該被一個輔助顯示拖垮。
func appointmentWorkOrderIDs(ids []int64) map[int64]bool {
	out := map[int64]bool{}
	if len(ids) == 0 {
		return out
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	q := `SELECT DISTINCT appointment_id FROM work_orders
		WHERE appointment_id IS NOT NULL AND appointment_id IN (` +
		strings.Join(placeholders, ",") + `)`
	rows, err := db.DB.Query(q, args...)
	if err != nil {
		log.Printf("查詢預約的工單關聯失敗: %v", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out[id] = true
		}
	}
	return out
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
