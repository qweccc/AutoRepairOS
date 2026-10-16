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
			line_user_id TEXT, telegram_user_id TEXT, deleted_at TEXT
		)`,
		`CREATE VIEW active_customers AS SELECT * FROM customers WHERE deleted_at IS NULL`,
		`CREATE TABLE vehicles (
			id INTEGER PRIMARY KEY, customer_id INTEGER, plate_number TEXT,
			next_maintenance_date TEXT, next_maintenance_mileage INTEGER,
			inspection_due_date TEXT, deleted_at TEXT
		)`,
		`CREATE VIEW active_vehicles AS SELECT * FROM vehicles WHERE deleted_at IS NULL`,
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
