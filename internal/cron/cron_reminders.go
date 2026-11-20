package cron

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"autorepairos/internal/db"
)

func scanMaintenanceDue() error {
	_, err := scanMaintenanceDueInternal()
	return err
}

// ScanMaintenanceDueNow 供 UI 手動觸發，回傳新增筆數。
func ScanMaintenanceDueNow() (int, error) {
	return scanMaintenanceDueInternal()
}

func scanMaintenanceDueInternal() (int, error) {
	queued, err := scanMaintenanceReminders()
	if err != nil {
		return 0, err
	}
	inspectionCount, err := scanInspectionReminders()
	if err != nil {
		return queued, err
	}
	queued += inspectionCount
	insuranceCount, err := scanInsuranceReminders()
	if err != nil {
		return queued, err
	}
	queued += insuranceCount
	if queued > 0 {
		log.Printf("車輛到期提醒掃描：新增 %d 筆提醒", queued)
	}
	return queued, nil
}

// scanBirthdayReminders 每日將當天生日的客戶排入生日關懷通知佇列。
// 去重靠 notifications 表的 (customer_id, kind, body, created_at) 判斷，
// 因此服務重啟後同一年度同一天不會重複產生。
func scanBirthdayReminders() error {
	_, err := ScanBirthdayRemindersNow()
	return err
}

// ScanBirthdayRemindersNow 供測試及手動觸發使用，回傳新增筆數。
func ScanBirthdayRemindersNow() (int, error) {
	now := time.Now().In(time.FixedZone("Asia/Taipei", 8*60*60))
	// 比對「月-日」，跨年不受影響；未設定生日（空值）會被 TRIM 濾掉。
	today := now.Format("01-02")
	rows, err := db.DB.Query(`SELECT c.id, COALESCE(c.name,''), COALESCE(c.notification_preference,'none'),
		COALESCE(c.line_user_id,''), COALESCE(c.telegram_user_id,''),
		COALESCE(c.discord_user_id,''), COALESCE(c.slack_user_id,'')
		FROM active_customers c
		WHERE TRIM(COALESCE(c.birthday,'')) != ''
		  AND strftime('%m-%d', c.birthday) = ?
		ORDER BY c.id`, today)
	if err != nil {
		return 0, err
	}

	var candidates []reminderCandidate
	for rows.Next() {
		var c reminderCandidate
		if err := rows.Scan(&c.customerID, &c.name, &c.pref, &c.lineID, &c.telegramID, &c.discordID, &c.slackID); err != nil {
			rows.Close()
			return 0, err
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	queued := 0
	for _, c := range candidates {
		body := fmt.Sprintf(tr("notify.birthday_body"), c.name)
		added, err := queueVehicleReminder("customer_birthday", tr("notify.birthday_subject"), body, c)
		if err != nil {
			return queued, err
		}
		if added {
			queued++
		}
	}
	if queued > 0 {
		log.Printf("生日關懷掃描：新增 %d 筆祝福", queued)
	}
	return queued, nil
}

// scanAppointmentReminders 每日將明日預約提醒排入既有通知佇列。
func scanAppointmentReminders() error {
	_, err := ScanAppointmentRemindersNow()
	return err
}

// ScanAppointmentRemindersNow 供測試及手動觸發使用，回傳新增筆數。
func ScanAppointmentRemindersNow() (int, error) {
	tpe := time.FixedZone("Asia/Taipei", 8*60*60)
	now := time.Now().In(tpe)
	// FR-APP-05：預約提醒包含「前一日提醒」與「當日進場前導」兩種。
	type round struct {
		date     string
		kind     string
		bodyTmpl string
		subjTmpl string
	}
	rounds := []round{
		{now.AddDate(0, 0, 1).Format("2006-01-02"), "appointment_reminder", "notify.appointment_body", "notify.appointment_subject"},
		{now.Format("2006-01-02"), "appointment_guide", "notify.appointment_guide_body", "notify.appointment_guide_subject"},
	}
	totalQueued := 0
	for _, rd := range rounds {
		rows, err := db.DB.Query(`SELECT a.id, c.id, COALESCE(c.name,''), COALESCE(c.notification_preference,'none'),
			COALESCE(c.line_user_id,''), COALESCE(c.telegram_user_id,''),
			COALESCE(c.discord_user_id,''), COALESCE(c.slack_user_id,''),
			COALESCE(v.plate_number,''), COALESCE(a.appointment_time,''), COALESCE(a.service_type,'')
			FROM active_appointments a
			JOIN active_customers c ON c.id=a.customer_id
			JOIN active_vehicles v ON v.id=a.vehicle_id
			WHERE a.appointment_date=? AND a.status IN ('booked','confirmed')
			ORDER BY a.id`, rd.date)
		if err != nil {
			return totalQueued, err
		}
		type item struct {
			appointmentID        int64
			customer             reminderCandidate
			plate, time, service string
		}
		var items []item
		for rows.Next() {
			var candidate item
			if err := rows.Scan(&candidate.appointmentID, &candidate.customer.customerID,
				&candidate.customer.name, &candidate.customer.pref, &candidate.customer.lineID,
				&candidate.customer.telegramID, &candidate.customer.discordID, &candidate.customer.slackID,
				&candidate.plate, &candidate.time, &candidate.service); err != nil {
				rows.Close()
				return totalQueued, err
			}
			items = append(items, candidate)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return totalQueued, err
		}
		rows.Close()

		for _, item := range items {
			channel, enabled := item.customer.channel()
			if !enabled {
				continue
			}
			body := fmt.Sprintf(tr(rd.bodyTmpl), item.plate, item.time, item.service)
			subject := fmt.Sprintf(tr(rd.subjTmpl), item.appointmentID)
			res, err := db.DB.Exec(`INSERT INTO notifications
				(customer_id, channel, kind, subject, body, scheduled_at)
				SELECT ?, ?, ?, ?, ?, ?
				WHERE NOT EXISTS (
					SELECT 1 FROM notifications
					WHERE customer_id=? AND kind=? AND subject=?
					  AND datetime(created_at) >= datetime(?)
				)`,
				item.customer.customerID, channel, rd.kind, subject, body, dbTime(time.Now().UTC()),
				item.customer.customerID, rd.kind, subject, time.Now().UTC().Add(-7*24*time.Hour).Format(sqlTimeLayout))
			if err != nil {
				return totalQueued, err
			}
			added, err := res.RowsAffected()
			if err != nil {
				return totalQueued, err
			}
			totalQueued += int(added)
		}
	}
	return totalQueued, nil
}

type reminderCandidate struct {
	customerID int64
	name       string
	pref       string
	lineID     string
	telegramID string
	discordID  string
	slackID    string
}

func (c reminderCandidate) channel() (string, bool) {
	switch strings.ToLower(strings.TrimSpace(c.pref)) {
	case "line":
		return "line", strings.TrimSpace(c.lineID) != ""
	case "telegram":
		return "telegram", strings.TrimSpace(c.telegramID) != ""
	case "discord":
		return "discord", strings.TrimSpace(c.discordID) != ""
	case "slack":
		return "slack", strings.TrimSpace(c.slackID) != ""
	default:
		return "", false
	}
}

// queueVehicleReminder verifies the selected channel binding and uses the same
// customer, event and body as the stable identity of a reminder for seven days.
func queueVehicleReminder(kind, subject, body string, c reminderCandidate) (bool, error) {
	channel, enabled := c.channel()
	if !enabled {
		return false, nil
	}
	now := time.Now().UTC()
	res, err := db.DB.Exec(`INSERT INTO notifications
		(customer_id, channel, kind, subject, body, scheduled_at)
		SELECT ?, ?, ?, ?, ?, ?
		WHERE NOT EXISTS (
			SELECT 1 FROM notifications
			WHERE customer_id=? AND kind=? AND body=?
			  AND datetime(created_at) >= datetime(?)
		)`,
		c.customerID, channel, kind, subject, body, dbTime(now),
		c.customerID, kind, body, now.Add(-7*24*time.Hour).Format(sqlTimeLayout))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func reminderDate(raw string) string {
	value := strings.TrimSpace(raw)
	if len(value) < 10 {
		return ""
	}
	date := value[:10]
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return ""
	}
	return date
}

func reminderDateRange() (today, cutoff string) {
	now := time.Now().In(time.FixedZone("Asia/Taipei", 8*60*60))
	today = now.Format("2006-01-02")
	cutoff = now.AddDate(0, 0, 30).Format("2006-01-02")
	return today, cutoff
}

func scanMaintenanceReminders() (int, error) {
	rows, err := db.DB.Query(`SELECT v.customer_id, COALESCE(v.plate_number,''),
		COALESCE(v.next_maintenance_date,''), COALESCE(v.next_maintenance_mileage,0),
		COALESCE((SELECT MAX(vm.mileage) FROM vehicle_mileages vm WHERE vm.vehicle_id=v.id),0),
		COALESCE((SELECT MAX(wo.current_mileage) FROM active_work_orders wo WHERE wo.vehicle_id=v.id),0),
		COALESCE(c.name,''), COALESCE(c.notification_preference,'none'),
		COALESCE(c.line_user_id,''), COALESCE(c.telegram_user_id,'')
		FROM active_vehicles v
		JOIN active_customers c ON c.id=v.customer_id
		WHERE TRIM(COALESCE(v.next_maintenance_date,''))!=''
		   OR COALESCE(v.next_maintenance_mileage,0)>0`)
	if err != nil {
		return 0, err
	}
	type candidate struct {
		customerID                               int64
		plate, dueDate                           string
		dueMileage, historyMileage, orderMileage int
		customer                                 reminderCandidate
	}
	var candidates []candidate
	dateCutoff := time.Now().In(time.FixedZone("Asia/Taipei", 8*60*60)).AddDate(0, 0, 7).Format("2006-01-02")
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.customerID, &c.plate, &c.dueDate,
			&c.dueMileage, &c.historyMileage, &c.orderMileage,
			&c.customer.name, &c.customer.pref, &c.customer.lineID, &c.customer.telegramID); err != nil {
			rows.Close()
			return 0, err
		}
		c.customer.customerID = c.customerID
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	queued := 0
	for _, c := range candidates {
		var reasons []string
		if dueDate := reminderDate(c.dueDate); dueDate != "" && dueDate <= dateCutoff {
			reasons = append(reasons, tr("notify.maintenance_reason_date")+dueDate)
		}
		currentMileage := c.historyMileage
		if c.orderMileage > currentMileage {
			currentMileage = c.orderMileage
		}
		if c.dueMileage > 0 && currentMileage >= c.dueMileage {
			reasons = append(reasons, fmt.Sprintf(tr("notify.maintenance_reason_km"), c.dueMileage))
		}
		if len(reasons) == 0 {
			continue
		}
		body := fmt.Sprintf(tr("notify.maintenance_body"),
			c.customer.name, c.plate, strings.Join(reasons, tr("notify.separator")))
		added, err := queueVehicleReminder("maintenance_due", tr("notify.maintenance_subject"), body, c.customer)
		if err != nil {
			return queued, err
		}
		if added {
			queued++
		}
	}
	return queued, nil
}

func scanInspectionReminders() (int, error) {
	rows, err := db.DB.Query(`SELECT c.id, COALESCE(v.plate_number,''), v.inspection_due_date,
		COALESCE(c.name,''), COALESCE(c.notification_preference,'none'),
		COALESCE(c.line_user_id,''), COALESCE(c.telegram_user_id,'')
		FROM active_vehicles v JOIN active_customers c ON c.id=v.customer_id
		WHERE TRIM(COALESCE(v.inspection_due_date,''))!=''
		UNION ALL
		SELECT c.id, COALESCE(v.plate_number,''), vi.order_date,
			COALESCE(c.name,''), COALESCE(c.notification_preference,'none'),
			COALESCE(c.line_user_id,''), COALESCE(c.telegram_user_id,'')
		FROM active_vehicle_inspections vi
		JOIN active_vehicles v ON v.id=vi.vehicle_id
		JOIN active_customers c ON c.id=v.customer_id
		WHERE COALESCE(vi.check_date,'')='' AND TRIM(COALESCE(vi.order_date,''))!=''`)
	if err != nil {
		return 0, err
	}
	type item struct {
		dueDate string
		plate   string
		reminderCandidate
	}
	var items []item
	for rows.Next() {
		var v item
		if err := rows.Scan(&v.reminderCandidate.customerID, &v.plate,
			&v.dueDate, &v.reminderCandidate.name,
			&v.reminderCandidate.pref, &v.reminderCandidate.lineID, &v.reminderCandidate.telegramID); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	today, cutoff := reminderDateRange()
	queued := 0
	for _, item := range items {
		dueDate := reminderDate(item.dueDate)
		if dueDate == "" || dueDate < today || dueDate > cutoff {
			continue
		}
		body := fmt.Sprintf(tr("notify.inspection_body"),
			item.name, item.plate, dueDate)
		added, err := queueVehicleReminder("inspection_due", tr("notify.inspection_subject"), body, item.reminderCandidate)
		if err != nil {
			return queued, err
		}
		if added {
			queued++
		}
	}
	return queued, nil
}

func scanInsuranceReminders() (int, error) {
	rows, err := db.DB.Query(`SELECT c.id, COALESCE(c.name,''), COALESCE(v.plate_number,''),
		COALESCE(c.notification_preference,'none'), COALESCE(c.line_user_id,''),
		COALESCE(c.telegram_user_id,''), COALESCE(vi.expiry_date,''),
		COALESCE(vi.policy_number,''), COALESCE(ic.name, vi.company_no, '')
		FROM active_vehicle_insurances vi
		JOIN active_vehicles v ON v.id=vi.vehicle_id
		JOIN active_customers c ON c.id=v.customer_id
		LEFT JOIN insurance_companies ic ON ic.id=vi.insurance_company_id
		WHERE TRIM(COALESCE(vi.expiry_date,''))!=''`)
	if err != nil {
		return 0, err
	}
	type item struct {
		dueDate, policy, company string
		plate                    string
		reminderCandidate
	}
	var items []item
	for rows.Next() {
		var v item
		if err := rows.Scan(&v.customerID, &v.name, &v.plate, &v.pref, &v.lineID,
			&v.telegramID, &v.dueDate, &v.policy, &v.company); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	today, cutoff := reminderDateRange()
	queued := 0
	for _, item := range items {
		dueDate := reminderDate(item.dueDate)
		if dueDate == "" || dueDate < today || dueDate > cutoff {
			continue
		}
		body := fmt.Sprintf(tr("notify.insurance_body"),
			item.name, item.plate, item.company, item.policy, dueDate)
		added, err := queueVehicleReminder("insurance_due", tr("notify.insurance_subject"), body, item.reminderCandidate)
		if err != nil {
			return queued, err
		}
		if added {
			queued++
		}
	}
	return queued, nil
}

// scanWorkOrderReady 補掃尚未建立完工通知的已完成工單。
func scanWorkOrderReady() error {
	_, err := scanWorkOrderReadyInternal()
	return err
}

// ScanWorkOrderReadyNow 供 UI 手動觸發，回傳新增筆數。
func ScanWorkOrderReadyNow() (int, error) {
	return scanWorkOrderReadyInternal()
}

func scanWorkOrderReadyInternal() (int, error) {
	rows, err := db.DB.Query(`SELECT wo.id, wo.customer_id
		FROM active_work_orders wo
		JOIN active_customers c ON c.id = wo.customer_id
		WHERE wo.status = 'completed'
		  AND NOT EXISTS (
			SELECT 1 FROM notifications n
			WHERE n.kind='work_order_completed' AND n.work_order_id=wo.id
		  )
		ORDER BY wo.id`)
	if err != nil {
		return 0, err
	}
	type candidate struct{ woID, customerID int64 }
	var candidates []candidate
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.woID, &item.customerID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("讀取待補掃完工工單失敗: %w", err)
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("列舉待補掃完工工單失敗: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("關閉待補掃工單查詢失敗: %w", err)
	}

	queued := 0
	var queueErrors []error
	for _, item := range candidates {
		added, err := queueWorkOrderCompletedNotification(item.woID, item.customerID)
		if err != nil {
			queueErrors = append(queueErrors, fmt.Errorf("補掃工單 #%d 通知失敗: %w", item.woID, err))
			continue
		}
		if added {
			queued++
		}
	}

	if queued > 0 {
		log.Printf("完工通知補掃：新增 %d 筆通知", queued)
	}
	return queued, errors.Join(queueErrors...)
}
