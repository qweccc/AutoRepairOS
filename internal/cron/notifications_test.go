package cron

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"autorepairos/internal/db"
	_ "modernc.org/sqlite"
)

func notificationTestDB(t *testing.T) *sql.DB {
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
			id INTEGER PRIMARY KEY, name TEXT, line_user_id TEXT, telegram_user_id TEXT,
			notification_preference TEXT DEFAULT 'line', deleted_at TEXT
		)`,
		`CREATE VIEW active_customers AS SELECT * FROM customers WHERE deleted_at IS NULL`,
		`CREATE TABLE vehicles (
			id INTEGER PRIMARY KEY, customer_id INTEGER, plate_number TEXT, deleted_at TEXT
		)`,
		`CREATE VIEW active_vehicles AS SELECT * FROM vehicles WHERE deleted_at IS NULL`,
		`CREATE TABLE work_orders (
			id INTEGER PRIMARY KEY, customer_id INTEGER, vehicle_id INTEGER, status TEXT,
			total_amount REAL DEFAULT 0, deleted_at TEXT
		)`,
		`CREATE VIEW active_work_orders AS SELECT * FROM work_orders WHERE deleted_at IS NULL`,
		`CREATE TABLE work_order_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT, work_order_id INTEGER,
			description TEXT, quantity INTEGER DEFAULT 1
		)`,
		`CREATE TABLE notifications (
			id INTEGER PRIMARY KEY AUTOINCREMENT, work_order_id INTEGER, customer_id INTEGER,
			channel TEXT, kind TEXT, subject TEXT, body TEXT, scheduled_at TEXT, sent_at TEXT,
			status TEXT, retry_count INTEGER DEFAULT 0, error_message TEXT, processing_at TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`INSERT INTO customers (id, name, line_user_id) VALUES (1, '通知車主', 'LINE-USER')`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func addCompletedWorkOrder(t *testing.T, d *sql.DB, woID, customerID, vehicleID int64) {
	t.Helper()
	for _, statement := range []struct {
		query string
		args  []interface{}
	}{
		{`INSERT INTO vehicles (id,customer_id,plate_number) VALUES (?,?,?)`, []interface{}{vehicleID, customerID, fmt.Sprintf("TEST-%d", vehicleID)}},
		{`INSERT INTO work_orders (id,customer_id,vehicle_id,status,total_amount) VALUES (?,?,?,'completed',1250)`, []interface{}{woID, customerID, vehicleID}},
	} {
		if _, err := d.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
}

func insertNotification(t *testing.T, d *sql.DB, channel string) int64 {
	t.Helper()
	res, err := d.Exec(`INSERT INTO notifications
		(customer_id, channel, kind, subject, body, scheduled_at, status, retry_count)
		VALUES (1, ?, 'test', 'subject', 'body', '2000-01-01T00:00:00Z', 'pending', 0)`, channel)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func notificationState(t *testing.T, d *sql.DB, id int64) (string, int) {
	t.Helper()
	var status string
	var retries int
	if err := d.QueryRow(`SELECT status, retry_count FROM notifications WHERE id=?`, id).
		Scan(&status, &retries); err != nil {
		t.Fatal(err)
	}
	return status, retries
}

func makeNotificationDue(t *testing.T, d *sql.DB, id int64) {
	t.Helper()
	if _, err := d.Exec(`UPDATE notifications SET scheduled_at='2000-01-01T00:00:00Z' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
}

func TestProcessPendingNotificationSuccess(t *testing.T) {
	d := notificationTestDB(t)
	id := insertNotification(t, d, "line")
	sent := 0

	count, err := processPendingNotificationsWithSender(func(notificationItem) error {
		sent++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	status, retries := notificationState(t, d, id)
	if count != 1 || sent != 1 || status != "sent" || retries != 0 {
		t.Fatalf("count=%d sent=%d status=%q retries=%d", count, sent, status, retries)
	}
}

func TestProcessPendingTelegramNotificationUsesCustomerBinding(t *testing.T) {
	d := notificationTestDB(t)
	id := insertNotification(t, d, "telegram")
	if _, err := d.Exec(`UPDATE customers SET telegram_user_id='TG-CUSTOMER' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	var received string
	count, err := processPendingNotificationsWithSender(func(item notificationItem) error {
		received = item.telegramUserID
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	status, retries := notificationState(t, d, id)
	if count != 1 || received != "TG-CUSTOMER" || status != "sent" || retries != 0 {
		t.Fatalf("count=%d recipient=%q status=%q retries=%d; want 1/TG-CUSTOMER/sent/0",
			count, received, status, retries)
	}
}

func TestProcessPendingNotificationRetriesThenSucceeds(t *testing.T) {
	d := notificationTestDB(t)
	id := insertNotification(t, d, "line")
	calls := 0
	send := func(notificationItem) error {
		calls++
		if calls == 1 {
			return errors.New("temporary outage")
		}
		return nil
	}

	if _, err := processPendingNotificationsWithSender(send); err != nil {
		t.Fatal(err)
	}
	status, retries := notificationState(t, d, id)
	if status != "pending" || retries != 1 {
		t.Fatalf("after first attempt status=%q retries=%d; want pending/1", status, retries)
	}
	var scheduledAt string
	if err := d.QueryRow(`SELECT scheduled_at FROM notifications WHERE id=?`, id).Scan(&scheduledAt); err != nil {
		t.Fatal(err)
	}
	retryAt, err := time.Parse(time.RFC3339, scheduledAt)
	if err != nil {
		t.Fatal(err)
	}
	if remaining := time.Until(retryAt); remaining < 58*time.Second || remaining > time.Minute+2*time.Second {
		t.Fatalf("first retry scheduled in %s; want about one minute", remaining)
	}
	makeNotificationDue(t, d, id)
	if _, err := processPendingNotificationsWithSender(send); err != nil {
		t.Fatal(err)
	}
	status, retries = notificationState(t, d, id)
	if calls != 2 || status != "sent" || retries != 1 {
		t.Fatalf("calls=%d status=%q retries=%d; want 2/sent/1", calls, status, retries)
	}
}

func TestProcessPendingNotificationStopsAtRetryLimit(t *testing.T) {
	d := notificationTestDB(t)
	id := insertNotification(t, d, "line")
	calls := 0

	for attempt := 0; attempt <= maxNotificationRetries; attempt++ {
		if attempt > 0 {
			makeNotificationDue(t, d, id)
		}
		_, err := processPendingNotificationsWithSender(func(notificationItem) error {
			calls++
			return errors.New("temporary outage")
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	status, retries := notificationState(t, d, id)
	if calls != maxNotificationRetries+1 || status != "permanent_failure" || retries != maxNotificationRetries+1 {
		t.Fatalf("calls=%d status=%q retries=%d", calls, status, retries)
	}
}

func TestProcessPendingNotificationPermanentFailure(t *testing.T) {
	d := notificationTestDB(t)
	id := insertNotification(t, d, "unknown")
	sent := false

	_, err := processPendingNotificationsWithSender(func(notificationItem) error {
		sent = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	status, retries := notificationState(t, d, id)
	if sent || status != "permanent_failure" || retries != 1 {
		t.Fatalf("sent=%v status=%q retries=%d", sent, status, retries)
	}
}

func TestProcessPendingNotificationsClaimsOnlyOnce(t *testing.T) {
	d := notificationTestDB(t)
	id := insertNotification(t, d, "line")
	started := make(chan struct{})
	release := make(chan struct{})
	sent := make(chan struct{}, 2)
	done := make(chan error, 2)
	var startOnce sync.Once
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	send := func(notificationItem) error {
		sent <- struct{}{}
		startOnce.Do(func() { close(started) })
		<-release
		return nil
	}

	go func() {
		_, err := processPendingNotificationsWithSender(send)
		done <- err
	}()
	<-started
	go func() {
		_, err := processPendingNotificationsWithSender(send)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		unblock()
		t.Fatal("second worker did not finish while the notification was claimed")
	}
	unblock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	status, retries := notificationState(t, d, id)
	if len(sent) != 1 || status != "sent" || retries != 0 {
		t.Fatalf("deliveries=%d status=%q retries=%d", len(sent), status, retries)
	}
}

func TestProcessPendingSMSChannelFailsPermanently(t *testing.T) {
	d := notificationTestDB(t)
	id := insertNotification(t, d, "sms")
	sent := false

	_, err := processPendingNotificationsWithSender(func(notificationItem) error {
		sent = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	status, retries := notificationState(t, d, id)
	var errMsg string
	if err := d.QueryRow(`SELECT error_message FROM notifications WHERE id=?`, id).Scan(&errMsg); err != nil {
		t.Fatal(err)
	}
	if sent || status != "permanent_failure" || retries != 1 {
		t.Fatalf("sent=%v status=%q retries=%d err=%q", sent, status, retries, errMsg)
	}
	if !strings.Contains(errMsg, "SMS 通道已停用") {
		t.Fatalf("error message %q does not mention SMS deprecation", errMsg)
	}
}

func TestProcessPendingTelegramChannelFailsWithoutToken(t *testing.T) {
	d := notificationTestDB(t)
	id := insertNotification(t, d, "telegram")
	if _, err := d.Exec(`UPDATE customers SET telegram_user_id='TG-CUSTOMER' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	sent := false

	_, err := processPendingNotificationsWithSender(func(item notificationItem) error {
		if item.channel == "telegram" {
			// 模擬 Telegram token 未設定的情況
			return permanentNotificationError(fmt.Errorf("Telegram Bot Token 未設定"))
		}
		sent = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	status, retries := notificationState(t, d, id)
	if sent || status != "permanent_failure" || retries != 1 {
		t.Fatalf("sent=%v status=%q retries=%d; want false/permanent_failure/1", sent, status, retries)
	}
}

func TestQueueWorkOrderCompletedNotificationCardAndDeduplication(t *testing.T) {
	d := notificationTestDB(t)
	addCompletedWorkOrder(t, d, 101, 1, 11)
	for i, item := range []string{"更換煞車皮", "更換機油", "檢查電瓶", "輪胎定位"} {
		if _, err := d.Exec(`INSERT INTO work_order_items(work_order_id,description,quantity) VALUES(?,?,?)`,
			101, item, i+1); err != nil {
			t.Fatal(err)
		}
	}

	if err := QueueWorkOrderCompletedNotification(101, 1); err != nil {
		t.Fatal(err)
	}
	if err := QueueWorkOrderCompletedNotification(101, 1); err != nil {
		t.Fatal(err)
	}

	var count int
	var subject, body string
	if err := d.QueryRow(`SELECT COUNT(*), MAX(subject), MAX(body) FROM notifications
		WHERE work_order_id=101 AND kind='work_order_completed'`).Scan(&count, &subject, &body); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("completed notification count=%d, want 1", count)
	}
	if subject != "完工通知 — 您的愛車已完成維修" {
		t.Fatalf("subject=%q", subject)
	}
	for _, required := range []string{"TEST-11", "工單編號：#101", "更換煞車皮 x1", "更換機油 x2", "檢查電瓶 x3", "另有 1 項", "工單總金額：$1250", "歡迎來廠取車並結帳"} {
		if !strings.Contains(body, required) {
			t.Errorf("card body missing %q: %s", required, body)
		}
	}
	if strings.Contains(body, "輪胎定位") {
		t.Errorf("fourth item should be omitted from summary: %s", body)
	}
}

func TestCompletedNotificationChannelRequiresCustomerBinding(t *testing.T) {
	d := notificationTestDB(t)
	tests := []struct {
		name       string
		pref       string
		lineID     string
		telegramID string
		want       string
	}{
		{name: "none preference", pref: "none", want: "internal"},
		{name: "line without binding", pref: "line", want: "internal"},
		{name: "telegram without binding", pref: "telegram", want: "internal"},
		{name: "telegram customer binding", pref: "telegram", telegramID: "TG-CUSTOMER", want: "telegram"},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			customerID, vehicleID, woID := int64(i+2), int64(i+20), int64(i+200)
			if _, err := d.Exec(`INSERT INTO customers (id,name,notification_preference,line_user_id,telegram_user_id)
				VALUES (?,?,?,?,?)`, customerID, tt.name, tt.pref, tt.lineID, tt.telegramID); err != nil {
				t.Fatal(err)
			}
			addCompletedWorkOrder(t, d, woID, customerID, vehicleID)
			if err := QueueWorkOrderCompletedNotification(woID, customerID); err != nil {
				t.Fatal(err)
			}
			var channel string
			if err := d.QueryRow(`SELECT channel FROM notifications
				WHERE work_order_id=? AND kind='work_order_completed'`, woID).Scan(&channel); err != nil {
				t.Fatal(err)
			}
			if channel != tt.want {
				t.Fatalf("notification channel=%q, want %q", channel, tt.want)
			}
		})
	}
}

func TestWorkOrderReadyBackfillQueuesOnlyMissingCompletedNotifications(t *testing.T) {
	d := notificationTestDB(t)
	addCompletedWorkOrder(t, d, 301, 1, 31)

	if count, err := ScanWorkOrderReadyNow(); err != nil || count != 1 {
		t.Fatalf("first backfill count=%d err=%v, want 1/nil", count, err)
	}
	if count, err := ScanWorkOrderReadyNow(); err != nil || count != 0 {
		t.Fatalf("second backfill count=%d err=%v, want 0/nil", count, err)
	}
}

func TestCompletedNotificationWriteFailureIsReturned(t *testing.T) {
	d := notificationTestDB(t)
	addCompletedWorkOrder(t, d, 401, 1, 41)
	if _, err := d.Exec(`CREATE TRIGGER reject_completed_notification
		BEFORE INSERT ON notifications BEGIN SELECT RAISE(ABORT, 'queue unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if err := QueueWorkOrderCompletedNotification(401, 1); err == nil {
		t.Fatal("queue error was not returned")
	}
}
