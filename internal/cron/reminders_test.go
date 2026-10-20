package cron

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"autorepairos/internal/db"
	_ "modernc.org/sqlite"
)

func reminderTestDB(t *testing.T) *sql.DB {
	t.Helper()
	original := db.DB
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	db.DB = d
	t.Cleanup(func() {
		db.DB = original
		_ = d.Close()
	})
	for _, stmt := range []string{
		`CREATE TABLE customers (
			id INTEGER PRIMARY KEY, name TEXT, notification_preference TEXT,
			line_user_id TEXT, telegram_user_id TEXT, birthday TEXT, deleted_at TEXT
		)`,
		`CREATE VIEW active_customers AS SELECT * FROM customers WHERE deleted_at IS NULL`,
		`CREATE TABLE vehicles (
			id INTEGER PRIMARY KEY, customer_id INTEGER, plate_number TEXT,
			next_maintenance_date TEXT, next_maintenance_mileage INTEGER,
			inspection_due_date TEXT, deleted_at TEXT
		)`,
		`CREATE VIEW active_vehicles AS SELECT * FROM vehicles WHERE deleted_at IS NULL`,
		`CREATE TABLE appointments (
			id INTEGER PRIMARY KEY, customer_id INTEGER, vehicle_id INTEGER,
			appointment_date TEXT, appointment_time TEXT, service_type TEXT,
			status TEXT DEFAULT 'booked', deleted_at TEXT
		)`,
		`CREATE VIEW active_appointments AS SELECT * FROM appointments WHERE deleted_at IS NULL`,
		`CREATE TABLE work_orders (id INTEGER PRIMARY KEY, vehicle_id INTEGER, current_mileage INTEGER, deleted_at TEXT)`,
		`CREATE VIEW active_work_orders AS SELECT * FROM work_orders WHERE deleted_at IS NULL`,
		`CREATE TABLE vehicle_mileages (id INTEGER PRIMARY KEY, vehicle_id INTEGER, mileage INTEGER)`,
		`CREATE TABLE vehicle_inspections (
			id INTEGER PRIMARY KEY, vehicle_id INTEGER, order_date TEXT, check_date TEXT, deleted_at TEXT
		)`,
		`CREATE VIEW active_vehicle_inspections AS SELECT * FROM vehicle_inspections WHERE deleted_at IS NULL`,
		`CREATE TABLE vehicle_insurances (
			id INTEGER PRIMARY KEY, vehicle_id INTEGER, expiry_date TEXT,
			policy_number TEXT, insurance_company_id INTEGER, company_no TEXT, deleted_at TEXT
		)`,
		`CREATE VIEW active_vehicle_insurances AS SELECT * FROM vehicle_insurances WHERE deleted_at IS NULL`,
		`CREATE TABLE insurance_companies (id INTEGER PRIMARY KEY, name TEXT)`,
		`CREATE TABLE notifications (
			id INTEGER PRIMARY KEY AUTOINCREMENT, work_order_id INTEGER, customer_id INTEGER,
			channel TEXT, kind TEXT, subject TEXT, body TEXT, scheduled_at TEXT, sent_at TEXT,
			status TEXT DEFAULT 'pending', retry_count INTEGER DEFAULT 0,
			error_message TEXT, processing_at TEXT, created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func addReminderVehicle(t *testing.T, d *sql.DB, pref, lineID, telegramID string) {
	t.Helper()
	if _, err := d.Exec(`INSERT INTO customers
		(id,name,notification_preference,line_user_id,telegram_user_id)
		VALUES (1,'客戶甲',?,?,?)`, pref, lineID, telegramID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO vehicles
		(id,customer_id,plate_number) VALUES (1,1,'ABC-1234')`); err != nil {
		t.Fatal(err)
	}
}

func countQueuedReminders(t *testing.T, d *sql.DB) int {
	t.Helper()
	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM notifications`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestMaintenanceReminderByDateAndMileage(t *testing.T) {
	d := reminderTestDB(t)
	addReminderVehicle(t, d, "line", "LINE-1", "")
	dueDate := time.Now().AddDate(0, 0, 3).Format("2006-01-02")
	if _, err := d.Exec(`UPDATE vehicles SET next_maintenance_date=?, next_maintenance_mileage=50000 WHERE id=1`, dueDate); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO vehicle_mileages(vehicle_id,mileage) VALUES(1,50000)`); err != nil {
		t.Fatal(err)
	}
	count, err := scanMaintenanceDueInternal()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("queued %d maintenance reminders; want one", count)
	}
	var channel, body string
	if err := d.QueryRow(`SELECT channel,body FROM notifications`).Scan(&channel, &body); err != nil {
		t.Fatal(err)
	}
	if channel != "line" || !containsAll(body, dueDate, "50000 公里") {
		t.Fatalf("channel=%q body=%q; want LINE reminder with both due conditions", channel, body)
	}
}

func TestInspectionReminderByVehicleDate(t *testing.T) {
	d := reminderTestDB(t)
	addReminderVehicle(t, d, "line", "LINE-1", "")
	dueDate := time.Now().AddDate(0, 0, 10).Format("2006-01-02")
	if _, err := d.Exec(`UPDATE vehicles SET inspection_due_date=? WHERE id=1`, dueDate); err != nil {
		t.Fatal(err)
	}
	count, err := scanMaintenanceDueInternal()
	if err != nil {
		t.Fatal(err)
	}
	var kind, body string
	if count != 1 {
		t.Fatalf("queued %d reminders; want one", count)
	}
	if err := d.QueryRow(`SELECT kind,body FROM notifications`).Scan(&kind, &body); err != nil {
		t.Fatal(err)
	}
	if kind != "inspection_due" || !containsAll(body, "定期檢驗", dueDate) {
		t.Fatalf("kind=%q body=%q; want dated inspection reminder", kind, body)
	}
}

func TestInsuranceExpiryReminder(t *testing.T) {
	d := reminderTestDB(t)
	addReminderVehicle(t, d, "telegram", "", "TG-1")
	dueDate := time.Now().AddDate(0, 0, 20).Format("2006-01-02")
	if _, err := d.Exec(`INSERT INTO vehicle_insurances
		(id,vehicle_id,expiry_date,policy_number,company_no) VALUES(1,1,?,'POL-123','安心保險')`, dueDate); err != nil {
		t.Fatal(err)
	}
	count, err := scanMaintenanceDueInternal()
	if err != nil {
		t.Fatal(err)
	}
	var kind, channel, body string
	if count != 1 {
		t.Fatalf("queued %d reminders; want one", count)
	}
	if err := d.QueryRow(`SELECT kind,channel,body FROM notifications`).Scan(&kind, &channel, &body); err != nil {
		t.Fatal(err)
	}
	if kind != "insurance_due" || channel != "telegram" || !containsAll(body, dueDate, "POL-123") {
		t.Fatalf("kind=%q channel=%q body=%q; want bound Telegram insurance reminder", kind, channel, body)
	}
}

func TestReminderHonorsNotificationPreferenceAndBinding(t *testing.T) {
	tests := []struct {
		name, pref, lineID, telegramID string
		wantCount                      int
		wantChannel                    string
	}{
		{name: "none", pref: "none"},
		{name: "missing LINE binding", pref: "line"},
		{name: "missing Telegram binding", pref: "telegram", lineID: "LINE-1"},
		{name: "LINE", pref: "line", lineID: "LINE-1", wantCount: 1, wantChannel: "line"},
		{name: "Telegram", pref: "telegram", telegramID: "TG-1", wantCount: 1, wantChannel: "telegram"},
		{name: "legacy SMS is not silently routed", pref: "sms", lineID: "LINE-1", wantCount: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := reminderTestDB(t)
			addReminderVehicle(t, d, tt.pref, tt.lineID, tt.telegramID)
			if _, err := d.Exec(`UPDATE vehicles SET next_maintenance_mileage=100 WHERE id=1`); err != nil {
				t.Fatal(err)
			}
			if _, err := d.Exec(`INSERT INTO work_orders(vehicle_id,current_mileage) VALUES(1,100)`); err != nil {
				t.Fatal(err)
			}
			count, err := scanMaintenanceDueInternal()
			if err != nil {
				t.Fatal(err)
			}
			if count != tt.wantCount || countQueuedReminders(t, d) != tt.wantCount {
				t.Fatalf("queued=%d; want %d", count, tt.wantCount)
			}
			if count == 1 {
				var channel string
				if err := d.QueryRow(`SELECT channel FROM notifications`).Scan(&channel); err != nil {
					t.Fatal(err)
				}
				if channel != tt.wantChannel {
					t.Fatalf("channel=%q; want %q", channel, tt.wantChannel)
				}
			}
		})
	}
}

func TestReminderDeduplicatesForSevenDays(t *testing.T) {
	d := reminderTestDB(t)
	addReminderVehicle(t, d, "line", "LINE-1", "")
	if _, err := d.Exec(`UPDATE vehicles SET next_maintenance_mileage=100 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO work_orders(vehicle_id,current_mileage) VALUES(1,100)`); err != nil {
		t.Fatal(err)
	}
	first, err := scanMaintenanceDueInternal()
	if err != nil {
		t.Fatal(err)
	}
	second, err := scanMaintenanceDueInternal()
	if err != nil {
		t.Fatal(err)
	}
	if first != 1 || second != 0 || countQueuedReminders(t, d) != 1 {
		t.Fatalf("first=%d second=%d total=%d; want 1, 0, 1",
			first, second, countQueuedReminders(t, d))
	}
	old := time.Now().UTC().Add(-8 * 24 * time.Hour).Format(sqlTimeLayout)
	if _, err := d.Exec(`UPDATE notifications SET created_at=?`, old); err != nil {
		t.Fatal(err)
	}
	third, err := scanMaintenanceDueInternal()
	if err != nil {
		t.Fatal(err)
	}
	if third != 1 || countQueuedReminders(t, d) != 2 {
		t.Fatalf("third=%d total=%d; want 1, 2", third, countQueuedReminders(t, d))
	}
}

func TestAppointmentReminderIncludesDetailsAndDeduplicates(t *testing.T) {
	d := reminderTestDB(t)
	tomorrow := time.Now().In(time.FixedZone("Asia/Taipei", 8*60*60)).
		AddDate(0, 0, 1).Format("2006-01-02")
	if _, err := d.Exec(`INSERT INTO customers
		(id,name,notification_preference,line_user_id,telegram_user_id)
		VALUES (1,'客戶甲','line','LINE-1',''), (2,'客戶乙','telegram','','TG-2'),
		       (3,'客戶丙','none','','')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO vehicles (id,customer_id,plate_number)
		VALUES (1,1,'ABC-1234'), (2,2,'XYZ-9876'), (3,3,'NO-ALERT')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO appointments
		(id,customer_id,vehicle_id,appointment_date,appointment_time,service_type,status)
		VALUES (11,1,1,?,'09:30','定期保養','booked'),
		       (12,2,2,?,'14:00','煞車檢查','confirmed'),
		       (13,3,3,?,'15:00','輪胎更換','booked'),
		       (14,1,1,?,'16:00','取消項目','cancelled')`,
		tomorrow, tomorrow, tomorrow, tomorrow); err != nil {
		t.Fatal(err)
	}

	first, err := ScanAppointmentRemindersNow()
	if err != nil {
		t.Fatal(err)
	}
	second, err := ScanAppointmentRemindersNow()
	if err != nil {
		t.Fatal(err)
	}
	if first != 2 || second != 0 || countQueuedReminders(t, d) != 2 {
		t.Fatalf("first=%d second=%d queued=%d; want 2, 0, 2",
			first, second, countQueuedReminders(t, d))
	}
	rows, err := d.Query(`SELECT channel, body FROM notifications ORDER BY customer_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var channel, body string
		if err := rows.Scan(&channel, &body); err != nil {
			t.Fatal(err)
		}
		got[channel] = body
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !containsAll(got["line"], "ABC-1234", "09:30", "定期保養") ||
		!containsAll(got["telegram"], "XYZ-9876", "14:00", "煞車檢查") {
		t.Fatalf("reminder bodies missing channel/appointment details: %#v", got)
	}
}

func containsAll(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(value, fragment) {
			return false
		}
	}
	return true
}

func TestInspectionScheduleReminderAndCompletedExclusion(t *testing.T) {
	d := reminderTestDB(t)
	addReminderVehicle(t, d, "line", "LINE-1", "")
	dueDate := time.Now().AddDate(0, 0, 5).Format("2006-01-02")
	if _, err := d.Exec(`INSERT INTO vehicle_inspections(vehicle_id,order_date,check_date) VALUES(1,?,'')`, dueDate); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO vehicle_inspections(vehicle_id,order_date,check_date) VALUES(1,?,'2026-01-01')`, dueDate); err != nil {
		t.Fatal(err)
	}
	count, err := scanMaintenanceDueInternal()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("queued %d reminders; want one pending inspection reminder", count)
	}
	var kind, body string
	if err := d.QueryRow(`SELECT kind,body FROM notifications`).Scan(&kind, &body); err != nil {
		t.Fatal(err)
	}
	if kind != "inspection_due" || !strings.Contains(body, dueDate) {
		t.Fatalf("kind=%q body=%q; want pending inspection reminder", kind, body)
	}
}

func addBirthdayCustomer(t *testing.T, d *sql.DB, id int64, name, birthday, pref, lineID, telegramID string) {
	t.Helper()
	if _, err := d.Exec(`INSERT INTO customers
		(id,name,birthday,notification_preference,line_user_id,telegram_user_id)
		VALUES (?,?,?,?,?,?)`, id, name, birthday, pref, lineID, telegramID); err != nil {
		t.Fatal(err)
	}
}

func TestBirthdayReminderQueuesOnlyToday(t *testing.T) {
	d := reminderTestDB(t)
	now := time.Now().In(time.FixedZone("Asia/Taipei", 8*60*60))
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")

	// 今天生日、已綁 LINE → 應產生
	addBirthdayCustomer(t, d, 1, "今天生日", today, "line", "LINE-1", "")
	// 今天生日、偏好不通知 → 不應產生
	addBirthdayCustomer(t, d, 2, "不通知", today, "none", "LINE-2", "")
	// 今天生日但未綁定任何通道 → 不應產生
	addBirthdayCustomer(t, d, 3, "未綁定", today, "line", "", "")
	// 不是今天生日 → 不應產生
	addBirthdayCustomer(t, d, 4, "昨天", yesterday, "line", "LINE-4", "")
	addBirthdayCustomer(t, d, 5, "明天", tomorrow, "line", "LINE-5", "")
	// 未設定生日 → 不應產生
	addBirthdayCustomer(t, d, 6, "沒生日", "", "line", "LINE-6", "")

	queued, err := ScanBirthdayRemindersNow()
	if err != nil {
		t.Fatal(err)
	}
	if queued != 1 {
		t.Fatalf("queued %d birthday reminders; want 1", queued)
	}
	var channel, body, name string
	if err := d.QueryRow(`SELECT n.channel, n.body, c.name FROM notifications n
		JOIN customers c ON c.id = n.customer_id`).Scan(&channel, &body, &name); err != nil {
		t.Fatal(err)
	}
	if channel != "line" || name != "今天生日" {
		t.Fatalf("channel=%q name=%q; want LINE to 今天生日", channel, name)
	}
	if !strings.Contains(body, "生日快樂") {
		t.Fatalf("body=%q; want birthday greeting", body)
	}
}

func TestBirthdayReminderDeduplicatesAcrossRuns(t *testing.T) {
	d := reminderTestDB(t)
	today := time.Now().In(time.FixedZone("Asia/Taipei", 8*60*60)).Format("2006-01-02")
	addBirthdayCustomer(t, d, 1, "壽星", today, "line", "LINE-1", "")

	if queued, err := ScanBirthdayRemindersNow(); err != nil || queued != 1 {
		t.Fatalf("first run queued=%d err=%v; want 1", queued, err)
	}
	// 模擬服務重啟後再跑一次：同一天同一客戶不得重複產生
	if queued, err := ScanBirthdayRemindersNow(); err != nil || queued != 0 {
		t.Fatalf("second run queued=%d err=%v; want 0 (dedup across restart)", queued, err)
	}
	if got := countQueuedReminders(t, d); got != 1 {
		t.Fatalf("notifications=%d; want 1", got)
	}
}

func TestBirthdayReminderRoutesByPreference(t *testing.T) {
	d := reminderTestDB(t)
	today := time.Now().In(time.FixedZone("Asia/Taipei", 8*60*60)).Format("2006-01-02")
	addBirthdayCustomer(t, d, 1, "電客戶", today, "telegram", "", "TG-1")

	queued, err := ScanBirthdayRemindersNow()
	if err != nil {
		t.Fatal(err)
	}
	if queued != 1 {
		t.Fatalf("queued %d; want 1", queued)
	}
	var channel string
	if err := d.QueryRow(`SELECT channel FROM notifications`).Scan(&channel); err != nil {
		t.Fatal(err)
	}
	if channel != "telegram" {
		t.Fatalf("channel=%q; want telegram", channel)
	}
}
