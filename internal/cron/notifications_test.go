package cron

import (
	"database/sql"
	"errors"
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
			id INTEGER PRIMARY KEY, line_user_id TEXT, telegram_user_id TEXT,
			notification_preference TEXT DEFAULT 'line', deleted_at TEXT
		)`,
		`CREATE VIEW active_customers AS SELECT * FROM customers WHERE deleted_at IS NULL`,
		`CREATE TABLE notifications (
			id INTEGER PRIMARY KEY AUTOINCREMENT, work_order_id INTEGER, customer_id INTEGER,
			channel TEXT, kind TEXT, subject TEXT, body TEXT, scheduled_at TEXT, sent_at TEXT,
			status TEXT, retry_count INTEGER DEFAULT 0, error_message TEXT, processing_at TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`INSERT INTO customers (id, line_user_id) VALUES (1, 'LINE-USER')`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	return d
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
