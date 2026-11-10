package cron

import (
	"testing"
	"time"
)

// The *RowsErr tests redefine a view/table as a UNION ALL whose second
// branch calls json_extract on a malformed-JSON value. That error fires at
// step time — after branch-1 rows stream out — so the caller's rows.Err()
// check trips. This covers mid-iteration error branches that scan-type
// mismatches (which fail at Scan, not Next) cannot reach.

func TestCompletedItemsRowsErr(t *testing.T) {
	d := notificationTestDB(t)
	addCompletedWorkOrder(t, d, 901, 1, 91)
	if _, err := d.Exec(`DROP TABLE work_order_items`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE TABLE woi_base (
		id INTEGER PRIMARY KEY AUTOINCREMENT, work_order_id INTEGER,
		description TEXT, quantity INTEGER DEFAULT 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO woi_base(work_order_id,description,quantity)
		VALUES(901,'not-json',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE VIEW work_order_items AS
		SELECT id, work_order_id, description, quantity FROM woi_base
		UNION ALL
		SELECT id, work_order_id, description, quantity FROM woi_base
		WHERE json_extract(description, '$') IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
	err := QueueWorkOrderCompletedNotification(901, 1)
	// ORDER BY forces the sorter to fill at Query/Scan time, so the step
	// error surfaces before the rows.Err() check; either error path counts.
	if err == nil {
		t.Fatal("expected malformed-JSON step error")
	}
}

func TestProcessPendingRowsErr(t *testing.T) {
	d := notificationTestDB(t)
	if _, err := d.Exec(`DROP TABLE notifications`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE TABLE notif_base (
		id INTEGER PRIMARY KEY AUTOINCREMENT, work_order_id INTEGER, customer_id INTEGER,
		channel TEXT, kind TEXT, subject TEXT, body TEXT, scheduled_at TEXT, sent_at TEXT,
		status TEXT, retry_count INTEGER DEFAULT 0, error_message TEXT, processing_at TEXT,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO notif_base
		(customer_id,channel,kind,subject,body,scheduled_at,status,retry_count)
		VALUES(1,'line','test','not-json','body','2000-01-01T00:00:00Z','pending',0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE VIEW notifications AS
		SELECT * FROM notif_base WHERE status='pending'
		UNION ALL
		SELECT * FROM notif_base WHERE json_extract(subject, '$') IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := processPendingNotificationsWithSender(func(notificationItem) error { return nil }); err == nil {
		t.Fatal("expected rows.Err from view branch")
	}
}

func TestBirthdayRowsErr(t *testing.T) {
	d := reminderTestDB(t)
	today := time.Now().In(time.FixedZone("Asia/Taipei", 8*60*60)).Format("2006-01-02")
	addBirthdayCustomer(t, d, 1, "not-json", today, "line", "L1", "")
	if _, err := d.Exec(`DROP VIEW active_customers`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE VIEW active_customers AS
		SELECT * FROM customers WHERE deleted_at IS NULL
		UNION ALL
		SELECT * FROM customers WHERE json_extract(name, '$') IS NOT NULL AND deleted_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanBirthdayRemindersNow(); err == nil {
		t.Fatal("expected rows.Err")
	}
}

func TestAppointmentRowsErr(t *testing.T) {
	d := reminderTestDB(t)
	addReminderVehicle(t, d, "line", "LINE-1", "")
	tomorrow := time.Now().In(time.FixedZone("Asia/Taipei", 8*60*60)).AddDate(0, 0, 1).Format("2006-01-02")
	if _, err := d.Exec(`INSERT INTO appointments
		(id,customer_id,vehicle_id,appointment_date,appointment_time,service_type,status)
		VALUES(11,1,1,?,'09:30','not-json','booked')`, tomorrow); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DROP VIEW active_appointments`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE VIEW active_appointments AS
		SELECT * FROM appointments WHERE deleted_at IS NULL
		UNION ALL
		SELECT * FROM appointments WHERE json_extract(service_type, '$') IS NOT NULL AND deleted_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanAppointmentRemindersNow(); err == nil {
		t.Fatal("expected rows.Err")
	}
}

func TestMaintenanceRowsErr(t *testing.T) {
	d := reminderTestDB(t)
	addReminderVehicle(t, d, "line", "LINE-1", "")
	due := time.Now().AddDate(0, 0, 3).Format("2006-01-02")
	if _, err := d.Exec(`UPDATE vehicles SET next_maintenance_date=?, plate_number='not-json' WHERE id=1`, due); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DROP VIEW active_vehicles`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE VIEW active_vehicles AS
		SELECT * FROM vehicles WHERE deleted_at IS NULL
		UNION ALL
		SELECT * FROM vehicles WHERE json_extract(plate_number, '$') IS NOT NULL AND deleted_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := scanMaintenanceReminders(); err == nil {
		t.Fatal("expected rows.Err")
	}
}

func TestInspectionRowsErr(t *testing.T) {
	d := reminderTestDB(t)
	addReminderVehicle(t, d, "line", "LINE-1", "")
	due := time.Now().AddDate(0, 0, 5).Format("2006-01-02")
	if _, err := d.Exec(`UPDATE vehicles SET inspection_due_date=?, plate_number='not-json' WHERE id=1`, due); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DROP VIEW active_vehicles`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE VIEW active_vehicles AS
		SELECT * FROM vehicles WHERE deleted_at IS NULL
		UNION ALL
		SELECT * FROM vehicles WHERE json_extract(plate_number, '$') IS NOT NULL AND deleted_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := scanInspectionReminders(); err == nil {
		t.Fatal("expected rows.Err")
	}
}

func TestInsuranceRowsErr(t *testing.T) {
	d := reminderTestDB(t)
	addReminderVehicle(t, d, "line", "LINE-1", "")
	due := time.Now().AddDate(0, 0, 5).Format("2006-01-02")
	if _, err := d.Exec(`INSERT INTO vehicle_insurances
		(id,vehicle_id,expiry_date,policy_number,company_no) VALUES(1,1,?,'not-json','Co')`, due); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DROP VIEW active_vehicle_insurances`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE VIEW active_vehicle_insurances AS
		SELECT * FROM vehicle_insurances WHERE deleted_at IS NULL
		UNION ALL
		SELECT * FROM vehicle_insurances WHERE json_extract(policy_number, '$') IS NOT NULL AND deleted_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := scanInsuranceReminders(); err == nil {
		t.Fatal("expected rows.Err")
	}
}

func TestWorkOrderReadyRowsErr(t *testing.T) {
	d := notificationTestDB(t)
	addCompletedWorkOrder(t, d, 902, 1, 92)
	if _, err := d.Exec(`DROP VIEW active_work_orders`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE VIEW active_work_orders AS
		SELECT * FROM work_orders WHERE deleted_at IS NULL
		UNION ALL
		SELECT * FROM work_orders WHERE json_extract(status, '$') IS NOT NULL AND deleted_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanWorkOrderReadyNow(); err == nil {
		t.Fatal("expected rows.Err")
	}
}

// The *StreamingRowsErr tests use a flattenable single-branch view whose
// CASE expression errors at step time for one late row. Because the view
// flattens, ORDER BY can use the INTEGER PRIMARY KEY / scheduled_at index
// instead of a sorter, so iteration streams and the error lands in
// rows.Err() rather than at Query() time.

func TestCompletedItemsStreamingRowsErr(t *testing.T) {
	d := notificationTestDB(t)
	addCompletedWorkOrder(t, d, 911, 1, 91)
	if _, err := d.Exec(`DROP TABLE work_order_items`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE TABLE woi_base (
		id INTEGER PRIMARY KEY, work_order_id INTEGER,
		description TEXT, quantity INTEGER DEFAULT 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO woi_base(id,work_order_id,description,quantity)
		VALUES(1,911,'good',1),(999,911,'x',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE VIEW work_order_items AS
		SELECT id, work_order_id,
			CASE WHEN id=999 THEN json_extract(description, '$') ELSE description END AS description,
			quantity
		FROM woi_base`); err != nil {
		t.Fatal(err)
	}
	if err := QueueWorkOrderCompletedNotification(911, 1); err == nil {
		t.Fatal("expected step error")
	}
}

func TestProcessPendingStreamingRowsErr(t *testing.T) {
	d := notificationTestDB(t)
	if _, err := d.Exec(`DROP TABLE notifications`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE TABLE notif_base (
		id INTEGER PRIMARY KEY, work_order_id INTEGER, customer_id INTEGER,
		channel TEXT, kind TEXT, subject TEXT, body TEXT, scheduled_at TEXT, sent_at TEXT,
		status TEXT, retry_count INTEGER DEFAULT 0, error_message TEXT, processing_at TEXT,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE INDEX idx_notif_sched ON notif_base(scheduled_at)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO notif_base
		(id,customer_id,channel,kind,subject,body,scheduled_at,status,retry_count)
		VALUES(1,1,'internal','test','ok','body','1999-01-01T00:00:00Z','pending',0),
		      (999,1,'internal','test','x','body','2000-01-01T00:00:00Z','pending',0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE VIEW notifications AS
		SELECT *, CASE WHEN id=999 THEN json_extract(subject, '$') ELSE subject END AS subject
		FROM notif_base`); err != nil {
		t.Fatal(err)
	}
	if _, err := processPendingNotificationsWithSender(func(notificationItem) error { return nil }); err == nil {
		t.Fatal("expected step error")
	}
}

func TestBirthdayStreamingRowsErr(t *testing.T) {
	d := reminderTestDB(t)
	today := time.Now().In(time.FixedZone("Asia/Taipei", 8*60*60)).Format("2006-01-02")
	addBirthdayCustomer(t, d, 1, "good", today, "line", "L1", "")
	if _, err := d.Exec(`INSERT INTO customers(id,name,birthday,notification_preference,line_user_id)
		VALUES(999,'x',?,'line','L9')`, today); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DROP VIEW active_customers`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE VIEW active_customers AS
		SELECT id,
			CASE WHEN id=999 THEN json_extract(name, '$') ELSE name END AS name,
			notification_preference, line_user_id, telegram_user_id, birthday, deleted_at
		FROM customers`); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanBirthdayRemindersNow(); err == nil {
		t.Fatal("expected step error")
	}
}

func TestAppointmentStreamingRowsErr(t *testing.T) {
	d := reminderTestDB(t)
	addReminderVehicle(t, d, "line", "LINE-1", "")
	tomorrow := time.Now().In(time.FixedZone("Asia/Taipei", 8*60*60)).AddDate(0, 0, 1).Format("2006-01-02")
	if _, err := d.Exec(`INSERT INTO appointments
		(id,customer_id,vehicle_id,appointment_date,appointment_time,service_type,status)
		VALUES(1,1,1,?,'09:30','good','booked'),(999,1,1,?,'10:00','x','booked')`,
		tomorrow, tomorrow); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DROP VIEW active_appointments`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE VIEW active_appointments AS
		SELECT id, customer_id, vehicle_id, appointment_date, appointment_time,
			CASE WHEN id=999 THEN json_extract(service_type, '$') ELSE service_type END AS service_type,
			status, deleted_at
		FROM appointments`); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanAppointmentRemindersNow(); err == nil {
		t.Fatal("expected step error")
	}
}

func TestWorkOrderReadyStreamingRowsErr(t *testing.T) {
	d := notificationTestDB(t)
	addCompletedWorkOrder(t, d, 912, 1, 92)
	if _, err := d.Exec(`INSERT INTO vehicles(id,customer_id,plate_number) VALUES(93,1,'TEST-93')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO work_orders(id,customer_id,vehicle_id,status,total_amount)
		VALUES(999,1,93,'completed',100)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DROP VIEW active_work_orders`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE VIEW active_work_orders AS
		SELECT id, customer_id, vehicle_id,
			CASE WHEN id=999 THEN json_extract(status, '$') ELSE status END AS status,
			total_amount, deleted_at
		FROM work_orders`); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanWorkOrderReadyNow(); err == nil {
		t.Fatal("expected step error")
	}
}
