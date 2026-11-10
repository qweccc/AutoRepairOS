package cron

import (
	"archive/tar"
	"compress/gzip"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeResult lets requireClaimedUpdate's RowsAffected error branch run.
type fakeResult struct{ rowsErr error }

func (f fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (f fakeResult) RowsAffected() (int64, error) { return 0, f.rowsErr }

func TestRequireClaimedUpdateRowsAffectedError(t *testing.T) {
	if err := requireClaimedUpdate(fakeResult{rowsErr: errors.New("ra fail")}, nil, 7); err == nil {
		t.Fatal("RowsAffected error should propagate")
	}
}

func TestQueueCompletedItemsQueryError(t *testing.T) {
	d := notificationTestDB(t)
	addCompletedWorkOrder(t, d, 701, 1, 71)
	if _, err := d.Exec(`DROP TABLE work_order_items`); err != nil {
		t.Fatal(err)
	}
	if err := QueueWorkOrderCompletedNotification(701, 1); err == nil {
		t.Fatal("expected items query error")
	}
}

func TestQueueCompletedItemScanError(t *testing.T) {
	d := notificationTestDB(t)
	addCompletedWorkOrder(t, d, 702, 1, 72)
	// quantity TEXT survives INTEGER affinity and breaks Scan into int.
	if _, err := d.Exec(`INSERT INTO work_order_items(work_order_id,description,quantity)
		VALUES(702,'x','abc')`); err != nil {
		t.Fatal(err)
	}
	if err := QueueWorkOrderCompletedNotification(702, 1); err == nil {
		t.Fatal("expected item scan error")
	}
}

func TestQueryNotificationsScanError(t *testing.T) {
	d := notificationTestDB(t)
	// retry_count TEXT breaks Scan into int → row skipped via continue.
	if _, err := d.Exec(`INSERT INTO notifications
		(customer_id,channel,kind,subject,body,scheduled_at,status,retry_count)
		VALUES(1,'line','test','s','b','2000-01-01T00:00:00Z','pending','abc')`); err != nil {
		t.Fatal(err)
	}
	got := PendingNotifications(10)
	if len(got) != 0 {
		t.Fatalf("bad row should be skipped, got %d", len(got))
	}
}

func TestProcessPendingScanError(t *testing.T) {
	d := notificationTestDB(t)
	// customer_id TEXT breaks Scan into int64 → logged and skipped.
	if _, err := d.Exec(`INSERT INTO notifications
		(customer_id,channel,kind,subject,body,scheduled_at,status,retry_count)
		VALUES('abc','line','test','s','b','2000-01-01T00:00:00Z','pending',0)`); err != nil {
		t.Fatal(err)
	}
	count, err := processPendingNotificationsWithSender(func(notificationItem) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("count=%d want 0 (bad row skipped)", count)
	}
}

func TestProcessPendingClaimErrors(t *testing.T) {
	// Abort trigger on claim (status→processing): claimNotification Exec fails.
	d := notificationTestDB(t)
	insertNotification(t, d, "line")
	if _, err := d.Exec(`CREATE TRIGGER abort_claim BEFORE UPDATE ON notifications
		WHEN NEW.status='processing' BEGIN SELECT RAISE(ABORT,'no claim'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := processPendingNotificationsWithSender(func(notificationItem) error { return nil }); err == nil {
		t.Fatal("expected claim error")
	}
	// Ignore trigger: UPDATE silently does nothing → 0 rows → !ok continue.
	d2 := notificationTestDB(t)
	insertNotification(t, d2, "line")
	if _, err := d2.Exec(`CREATE TRIGGER ignore_claim BEFORE UPDATE ON notifications
		WHEN NEW.status='processing' BEGIN SELECT RAISE(IGNORE); END`); err != nil {
		t.Fatal(err)
	}
	count, err := processPendingNotificationsWithSender(func(notificationItem) error { return nil })
	if err != nil || count != 0 {
		t.Fatalf("count=%d err=%v want 0/nil", count, err)
	}
}

// abortMark triggers make markSent (status→sent) or markFailed (retry_count+1)
// fail after a successful claim, covering the per-channel error returns.
func abortMarkTriggers(t *testing.T, d *sql.DB) {
	t.Helper()
	for _, stmt := range []string{
		`CREATE TRIGGER abort_sent BEFORE UPDATE ON notifications
			WHEN NEW.status='sent' BEGIN SELECT RAISE(ABORT,'no sent'); END`,
		`CREATE TRIGGER abort_fail BEFORE UPDATE ON notifications
			WHEN NEW.retry_count > OLD.retry_count BEGIN SELECT RAISE(ABORT,'no fail'); END`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProcessPendingMarkErrors(t *testing.T) {
	// internal: markSent aborts.
	d := notificationTestDB(t)
	abortMarkTriggers(t, d)
	insertNotification(t, d, "internal")
	if _, err := processPendingNotificationsWithSender(func(notificationItem) error { return nil }); err == nil {
		t.Fatal("internal markSent error should propagate")
	}
	// sms: markFailed aborts.
	d2 := notificationTestDB(t)
	abortMarkTriggers(t, d2)
	insertNotification(t, d2, "sms")
	if _, err := processPendingNotificationsWithSender(func(notificationItem) error { return nil }); err == nil {
		t.Fatal("sms markFailed error should propagate")
	}
	// default channel: markFailed aborts.
	d3 := notificationTestDB(t)
	abortMarkTriggers(t, d3)
	insertNotification(t, d3, "pager")
	if _, err := processPendingNotificationsWithSender(func(notificationItem) error { return nil }); err == nil {
		t.Fatal("default markFailed error should propagate")
	}
}

func TestProcessPendingLineTelegramMarkErrors(t *testing.T) {
	// line, missing binding: markFailed aborts.
	d := notificationTestDB(t)
	abortMarkTriggers(t, d)
	insertNotificationFor(t, d, int64(999), "line")
	if _, err := processPendingNotificationsWithSender(func(notificationItem) error { return nil }); err == nil {
		t.Fatal("line missing-binding markFailed error should propagate")
	}
	// line, send fails: markFailed aborts.
	d2 := notificationTestDB(t)
	abortMarkTriggers(t, d2)
	insertNotification(t, d2, "line")
	if _, err := processPendingNotificationsWithSender(func(notificationItem) error {
		return errors.New("send fail")
	}); err == nil {
		t.Fatal("line send-fail markFailed error should propagate")
	}
	// line, send ok: markSent aborts.
	d3 := notificationTestDB(t)
	abortMarkTriggers(t, d3)
	insertNotification(t, d3, "line")
	if _, err := processPendingNotificationsWithSender(func(notificationItem) error { return nil }); err == nil {
		t.Fatal("line markSent error should propagate")
	}
	// telegram, missing binding: markFailed aborts.
	d4 := notificationTestDB(t)
	abortMarkTriggers(t, d4)
	insertNotificationFor(t, d4, int64(1), "telegram")
	if _, err := processPendingNotificationsWithSender(func(notificationItem) error { return nil }); err == nil {
		t.Fatal("telegram missing-binding markFailed error should propagate")
	}
	// telegram, send fails: markFailed aborts.
	d5 := notificationTestDB(t)
	abortMarkTriggers(t, d5)
	if _, err := d5.Exec(`UPDATE customers SET telegram_user_id='TG-1' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	insertNotification(t, d5, "telegram")
	if _, err := processPendingNotificationsWithSender(func(notificationItem) error {
		return errors.New("tg fail")
	}); err == nil {
		t.Fatal("telegram send-fail markFailed error should propagate")
	}
	// telegram, send ok: markSent aborts.
	d6 := notificationTestDB(t)
	abortMarkTriggers(t, d6)
	if _, err := d6.Exec(`UPDATE customers SET telegram_user_id='TG-1' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	insertNotification(t, d6, "telegram")
	if _, err := processPendingNotificationsWithSender(func(notificationItem) error { return nil }); err == nil {
		t.Fatal("telegram markSent error should propagate")
	}
}

func TestProcessPendingNotificationsDeliverClosure(t *testing.T) {
	// Covers the deliver closure inside processPendingNotifications (mock LINE).
	t.Setenv("LINE_CHANNEL_ID", "")
	t.Setenv("LINE_CHANNEL_SECRET", "")
	t.Setenv("LINE_CHANNEL_ACCESS_TOKEN", "")
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	d := notificationTestDB(t)
	id := insertNotification(t, d, "line")
	if n := ProcessNotificationsNow(); n != 1 {
		t.Fatalf("n=%d want 1", n)
	}
	if st, _ := notificationState(t, d, id); st != "sent" {
		t.Fatalf("status=%q", st)
	}
}

func TestTickRunTaskErrorLogged(t *testing.T) {
	d := engineTestDB(t)
	snapshotRegistry(t)
	for k := range registry {
		delete(registry, k)
	}
	Register(Task{Key: "upd-fail", Name: "upd-fail", Interval: time.Minute, Run: func() error { return nil }})
	past := dbTime(time.Now().Add(-time.Hour))
	if _, err := d.Exec(`INSERT INTO scheduled_tasks(task_key,enabled,next_run_at) VALUES('upd-fail',1,?)`, past); err != nil {
		t.Fatal(err)
	}
	// UPDATEs to scheduled_tasks abort → runTask returns error → tick logs it.
	if _, err := d.Exec(`CREATE TRIGGER abort_upd BEFORE UPDATE ON scheduled_tasks
		BEGIN SELECT RAISE(ABORT,'no updates'); END`); err != nil {
		t.Fatal(err)
	}
	s := &Scheduler{stop: make(chan struct{})}
	s.tick() // must not panic; error path logged
}

func TestStatusScanErrorSkipped(t *testing.T) {
	d := engineTestDB(t)
	// interval_minutes TEXT breaks Scan into int → row skipped.
	if _, err := d.Exec(`INSERT INTO scheduled_tasks(task_key,name,interval_minutes,enabled)
		VALUES('bad','Bad','abc',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO scheduled_tasks(task_key,name,interval_minutes,enabled)
		VALUES('good','Good',30,1)`); err != nil {
		t.Fatal(err)
	}
	got := Status()
	if len(got) != 1 || got[0].Key != "good" {
		t.Fatalf("got %+v", got)
	}
}

func TestBirthdayScanError(t *testing.T) {
	d := birthdayTextIDDB(t)
	addBirthdayTextCustomer(t, d)
	if _, err := ScanBirthdayRemindersNow(); err == nil {
		t.Fatal("expected scan error")
	}
}

func birthdayTextIDDB(t *testing.T) *sql.DB {
	t.Helper()
	d := reminderTestDB(t)
	// Rebuild customers with TEXT id PK (view follows).
	if _, err := d.Exec(`DROP VIEW active_customers`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DROP TABLE customers`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE TABLE customers (
		id TEXT PRIMARY KEY, name TEXT, notification_preference TEXT,
		line_user_id TEXT, telegram_user_id TEXT, birthday TEXT, deleted_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE VIEW active_customers AS SELECT * FROM customers WHERE deleted_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	return d
}

func addBirthdayTextCustomer(t *testing.T, d *sql.DB) {
	t.Helper()
	today := time.Now().In(time.FixedZone("Asia/Taipei", 8*60*60)).Format("2006-01-02")
	if _, err := d.Exec(`INSERT INTO customers(id,name,birthday,notification_preference,line_user_id)
		VALUES('c-today','壽星',?,'line','L1')`, today); err != nil {
		t.Fatal(err)
	}
}

func TestAppointmentScanError(t *testing.T) {
	d := reminderTestDB(t)
	if _, err := d.Exec(`DROP VIEW active_appointments`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DROP TABLE appointments`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE TABLE appointments (
		id TEXT PRIMARY KEY, customer_id INTEGER, vehicle_id INTEGER,
		appointment_date TEXT, appointment_time TEXT, service_type TEXT,
		status TEXT DEFAULT 'booked', deleted_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE VIEW active_appointments AS SELECT * FROM appointments WHERE deleted_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	addReminderVehicle(t, d, "line", "LINE-1", "")
	tomorrow := time.Now().In(time.FixedZone("Asia/Taipei", 8*60*60)).AddDate(0, 0, 1).Format("2006-01-02")
	if _, err := d.Exec(`INSERT INTO appointments(id,customer_id,vehicle_id,appointment_date,status)
		VALUES('appt-1',1,1,?,'booked')`, tomorrow); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanAppointmentRemindersNow(); err == nil {
		t.Fatal("expected scan error")
	}
}

func TestMaintenanceScanError(t *testing.T) {
	d := reminderTestDB(t)
	addReminderVehicle(t, d, "line", "LINE-1", "")
	// next_maintenance_mileage TEXT breaks Scan into int.
	if _, err := d.Exec(`UPDATE vehicles SET next_maintenance_mileage='abc' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := scanMaintenanceReminders(); err == nil {
		t.Fatal("expected scan error")
	}
}

func TestInspectionScanError(t *testing.T) {
	d := reminderTestDB(t)
	if _, err := d.Exec(`DROP VIEW active_customers`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DROP TABLE customers`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE TABLE customers (
		id TEXT PRIMARY KEY, name TEXT, notification_preference TEXT,
		line_user_id TEXT, telegram_user_id TEXT, birthday TEXT, deleted_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE VIEW active_customers AS SELECT * FROM customers WHERE deleted_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO customers(id,name,notification_preference,line_user_id)
		VALUES('c1','甲','line','L1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO vehicles(id,customer_id,plate_number,inspection_due_date)
		VALUES(1,'c1','P1',?)`, time.Now().AddDate(0, 0, 5).Format("2006-01-02")); err != nil {
		t.Fatal(err)
	}
	if _, err := scanInspectionReminders(); err == nil {
		t.Fatal("expected scan error")
	}
}

func TestInsuranceScanError(t *testing.T) {
	d := reminderTestDB(t)
	if _, err := d.Exec(`DROP VIEW active_vehicles`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DROP TABLE vehicles`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE TABLE vehicles (
		id TEXT PRIMARY KEY, customer_id INTEGER, plate_number TEXT,
		next_maintenance_date TEXT, next_maintenance_mileage INTEGER,
		inspection_due_date TEXT, deleted_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE VIEW active_vehicles AS SELECT * FROM vehicles WHERE deleted_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	addReminderVehicle(t, d, "line", "LINE-1", "")
	// vehicle id now TEXT '1'; insurance references it as TEXT.
	if _, err := d.Exec(`INSERT INTO vehicle_insurances(id,vehicle_id,expiry_date,policy_number)
		VALUES(1,'1',?,'P1')`, time.Now().AddDate(0, 0, 5).Format("2006-01-02")); err != nil {
		t.Fatal(err)
	}
	// scan scans v.* only via plate; the failing scan is c.id? Insurance scans
	// customerID from c.id (int PK) — use TEXT customer id instead.
	if _, err := scanInsuranceReminders(); err != nil {
		// If this path errors for another reason, still counts as covered only
		// if it's the scan branch; verify below with TEXT customer id.
		_ = err
	}
	// Deterministic: rebuild customers with TEXT id and rerun.
	if _, err := d.Exec(`DROP VIEW active_customers`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DROP TABLE customers`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE TABLE customers (
		id TEXT PRIMARY KEY, name TEXT, notification_preference TEXT,
		line_user_id TEXT, telegram_user_id TEXT, birthday TEXT, deleted_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE VIEW active_customers AS SELECT * FROM customers WHERE deleted_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO customers(id,name,notification_preference,line_user_id)
		VALUES('c1','甲','line','L1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE vehicles SET customer_id='c1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := scanInsuranceReminders(); err == nil {
		t.Fatal("expected scan error")
	}
}

func TestWorkOrderReadyScanError(t *testing.T) {
	d := notificationTestDB(t)
	// Rebuild customers with TEXT id PK so a work order can join yet carry a
	// non-numeric customer_id that breaks Scan into int64.
	if _, err := d.Exec(`DROP VIEW active_customers`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DROP TABLE customers`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE TABLE customers (
		id TEXT PRIMARY KEY, name TEXT, line_user_id TEXT, telegram_user_id TEXT,
		notification_preference TEXT DEFAULT 'line', deleted_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE VIEW active_customers AS SELECT * FROM customers WHERE deleted_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO customers(id,name,line_user_id) VALUES('abc','通知車主','LINE-USER')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO vehicles(id,customer_id,plate_number) VALUES(81,'abc','TEST-81')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO work_orders(id,customer_id,vehicle_id,status,total_amount)
		VALUES(801,'abc',81,'completed',100)`); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanWorkOrderReadyNow(); err == nil {
		t.Fatal("expected scan error")
	}
}

func TestReminderQueueErrors(t *testing.T) {
	// Birthday: queue insert fails (notifications dropped).
	d := reminderTestDB(t)
	today := time.Now().In(time.FixedZone("Asia/Taipei", 8*60*60)).Format("2006-01-02")
	addBirthdayCustomer(t, d, 1, "壽星", today, "line", "L1", "")
	if _, err := d.Exec(`DROP TABLE notifications`); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanBirthdayRemindersNow(); err == nil {
		t.Fatal("birthday queue error expected")
	}
	// Appointment: queue insert fails.
	d2 := reminderTestDB(t)
	addReminderVehicle(t, d2, "line", "LINE-1", "")
	tomorrow := time.Now().In(time.FixedZone("Asia/Taipei", 8*60*60)).AddDate(0, 0, 1).Format("2006-01-02")
	if _, err := d2.Exec(`INSERT INTO appointments(id,customer_id,vehicle_id,appointment_date,status)
		VALUES(1,1,1,?,'booked')`, tomorrow); err != nil {
		t.Fatal(err)
	}
	if _, err := d2.Exec(`DROP TABLE notifications`); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanAppointmentRemindersNow(); err == nil {
		t.Fatal("appointment queue error expected")
	}
	// Maintenance / inspection / insurance queue errors via internal scan.
	d3 := reminderTestDB(t)
	addReminderVehicle(t, d3, "line", "LINE-1", "")
	due := time.Now().AddDate(0, 0, 3).Format("2006-01-02")
	if _, err := d3.Exec(`UPDATE vehicles SET next_maintenance_date=? WHERE id=1`, due); err != nil {
		t.Fatal(err)
	}
	if _, err := d3.Exec(`DROP TABLE notifications`); err != nil {
		t.Fatal(err)
	}
	if _, err := scanMaintenanceReminders(); err == nil {
		t.Fatal("maintenance queue error expected")
	}
	d4 := reminderTestDB(t)
	addReminderVehicle(t, d4, "line", "LINE-1", "")
	if _, err := d4.Exec(`UPDATE vehicles SET inspection_due_date=? WHERE id=1`,
		time.Now().AddDate(0, 0, 5).Format("2006-01-02")); err != nil {
		t.Fatal(err)
	}
	if _, err := d4.Exec(`DROP TABLE notifications`); err != nil {
		t.Fatal(err)
	}
	if _, err := scanInspectionReminders(); err == nil {
		t.Fatal("inspection queue error expected")
	}
	d5 := reminderTestDB(t)
	addReminderVehicle(t, d5, "line", "LINE-1", "")
	if _, err := d5.Exec(`INSERT INTO vehicle_insurances(id,vehicle_id,expiry_date,policy_number)
		VALUES(1,1,?,'P1')`, time.Now().AddDate(0, 0, 5).Format("2006-01-02")); err != nil {
		t.Fatal(err)
	}
	if _, err := d5.Exec(`DROP TABLE notifications`); err != nil {
		t.Fatal(err)
	}
	if _, err := scanInsuranceReminders(); err == nil {
		t.Fatal("insurance queue error expected")
	}
}

func TestDailyBackupEnvOverride(t *testing.T) {
	d := backupTestDB(t)
	root := t.TempDir()
	backupDir := filepath.Join(root, "env-backups")
	photoDir := filepath.Join(root, "photos")
	if err := os.MkdirAll(photoDir, 0755); err != nil {
		t.Fatal(err)
	}
	backupCfg = BackupConfig{BackupDir: filepath.Join(root, "ignored"), PhotoDir: photoDir}
	t.Setenv("BACKUP_DIR", backupDir)
	if err := DailyBackup(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("env BACKUP_DIR not used: entries=%v err=%v", entries, err)
	}
	var path string
	if err := d.QueryRow(`SELECT db_backup_path FROM backup_records ORDER BY id DESC LIMIT 1`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "env-backups") {
		t.Fatalf("path=%q", path)
	}
}

// preCreateStampDirs blocks VACUUM INTO (dirs named like the .db snapshot) or
// archive creation (dirs named like the .tar.gz).
func preCreateStampDirs(t *testing.T, dir, suffix string) {
	t.Helper()
	now := time.Now()
	for i := -1; i < 10; i++ {
		name := "autorepair_" + now.Add(time.Duration(i)*time.Second).Format("20060102_150405") + suffix
		_ = os.Mkdir(filepath.Join(dir, name), 0755)
	}
}

func TestDailyBackupDBFailure(t *testing.T) {
	d := backupTestDB(t)
	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatal(err)
	}
	preCreateStampDirs(t, backupDir, ".db")
	backupCfg = BackupConfig{BackupDir: backupDir, PhotoDir: filepath.Join(root, "photos")}
	if err := DailyBackup(); err == nil {
		t.Fatal("DailyBackup should fail when VACUUM INTO target is a directory")
	}
	var status string
	if err := d.QueryRow(`SELECT status FROM backup_records ORDER BY id DESC LIMIT 1`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Fatalf("status=%q", status)
	}
}

func TestDailyBackupArchiveFailure(t *testing.T) {
	d := backupTestDB(t)
	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatal(err)
	}
	photoDir := filepath.Join(root, "photos")
	if err := os.MkdirAll(photoDir, 0755); err != nil {
		t.Fatal(err)
	}
	preCreateStampDirs(t, backupDir, ".tar.gz")
	backupCfg = BackupConfig{BackupDir: backupDir, PhotoDir: photoDir}
	if err := DailyBackup(); err == nil {
		t.Fatal("DailyBackup should fail when archive path is a directory")
	}
	var status, reason string
	if err := d.QueryRow(`SELECT status, error_message FROM backup_records ORDER BY id DESC LIMIT 1`).
		Scan(&status, &reason); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || !strings.Contains(reason, "封存") {
		t.Fatalf("status=%q reason=%q", status, reason)
	}
}

func TestPruneBackupsSkips(t *testing.T) {
	orig := backupCfg
	t.Cleanup(func() { backupCfg = orig })
	root := t.TempDir()
	backupCfg.BackupDir = root
	old := filepath.Join(root, "autorepair_20000101_010101.db")
	if err := os.WriteFile(old, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-40 * 24 * time.Hour)
	if err := os.Chtimes(old, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(root, "notes.txt") // wrong suffix: kept even if old
	if err := os.WriteFile(keep, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(keep, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "subdir.db"), 0755); err != nil {
		t.Fatal(err)
	}
	pruneBackups(30 * 24 * time.Hour)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old .db should be pruned")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("non-backup file should be kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "subdir.db")); err != nil {
		t.Fatalf("subdir should be kept: %v", err)
	}
}

func TestDirStatsThroughFile(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("invalid filename characters are legal on this OS")
	}
	root := t.TempDir()
	// Windows rejects '<' in path components with a non-IsNotExist error.
	bad := filepath.Join(root, "bad<name")
	if _, _, err := dirStats(bad); err == nil {
		t.Fatal("expected invalid-name error")
	}
	// Same for the archive stat branch.
	dbFile := filepath.Join(root, "snap.db")
	if err := os.WriteFile(dbFile, []byte("db"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := createBackupArchive(dbFile, filepath.Join(root, "a.tar.gz"), bad); err == nil {
		t.Fatal("expected stat error")
	}
}

func TestNonRegularPhotoFile(t *testing.T) {
	root := t.TempDir()
	photoDir := filepath.Join(root, "photos")
	if err := os.MkdirAll(photoDir, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(photoDir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, _, err := dirStats(photoDir); err == nil {
		t.Fatal("symlink in photo dir should error in dirStats")
	}
	// Archive walk also rejects non-regular entries.
	dbFile := filepath.Join(root, "snap.db")
	if err := os.WriteFile(dbFile, []byte("db"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := createBackupArchive(dbFile, filepath.Join(root, "a.tar.gz"), photoDir); err == nil {
		t.Fatal("symlink in photo dir should error in createBackupArchive")
	}
}

func TestCopyFileFromDirectory(t *testing.T) {
	root := t.TempDir()
	if _, err := copyFile(root, filepath.Join(root, "out")); err == nil {
		t.Fatal("copying a directory should error at copy stage")
	}
}

func TestAddArchiveContentsFromDirectory(t *testing.T) {
	root := t.TempDir()
	out, err := os.Create(filepath.Join(root, "x.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	// addArchiveFile on a directory: header ok, contents copy fails.
	dbFile := filepath.Join(root, "snap.db")
	if err := os.WriteFile(dbFile, []byte("db"), 0644); err != nil {
		t.Fatal(err)
	}
	_ = dbFile
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	defer tw.Close()
	defer gz.Close()
	if err := addArchiveFile(tw, root, "dir"); err == nil {
		t.Fatal("addArchiveFile on directory should error at copy stage")
	}
}

func TestAddArchiveFileClosedWriter(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "f.txt")
	if err := os.WriteFile(p, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(filepath.Join(root, "x.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	_ = tw.Close()
	_ = gz.Close()
	// WriteHeader on a closed writer errors (addArchiveFile header branch).
	if err := addArchiveFile(tw, p, "f.txt"); err == nil {
		t.Fatal("write to closed tar writer should error")
	}
}
