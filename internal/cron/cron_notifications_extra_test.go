package cron

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"autorepairos/internal/integration/line"
	"autorepairos/internal/integration/telegram"
)

func insertNotificationFor(t *testing.T, d *sql.DB, customerID interface{}, channel string) int64 {
	t.Helper()
	res, err := d.Exec(`INSERT INTO notifications
		(customer_id, channel, kind, subject, body, scheduled_at, status, retry_count)
		VALUES (?, ?, 'test', 'subject', 'body', '2000-01-01T00:00:00Z', 'pending', 0)`, customerID, channel)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestQueueNotification(t *testing.T) {
	d := notificationTestDB(t)
	now := time.Now()
	if err := QueueNotification("kind1", "internal", "subj", "body", 0, 0, now); err != nil {
		t.Fatal(err)
	}
	if err := QueueNotification("kind2", "line", "subj2", "body2", 1, 42, now); err != nil {
		t.Fatal(err)
	}
	var cust, wo sql.NullInt64
	var sched string
	if err := d.QueryRow(`SELECT customer_id, work_order_id, scheduled_at FROM notifications WHERE kind='kind2'`).
		Scan(&cust, &wo, &sched); err != nil {
		t.Fatal(err)
	}
	if cust.Int64 != 1 || wo.Int64 != 42 {
		t.Fatalf("cust=%v wo=%v", cust, wo)
	}
	if _, err := time.Parse(time.RFC3339, sched); err != nil {
		t.Fatalf("scheduled_at not RFC3339: %q %v", sched, err)
	}
	// Zero IDs are stored as NULL.
	if err := d.QueryRow(`SELECT customer_id FROM notifications WHERE kind='kind1'`).Scan(&cust); err != nil {
		t.Fatal(err)
	}
	if cust.Valid {
		t.Fatalf("zero customerID should be NULL, got %v", cust)
	}
	// Error path: table dropped.
	if _, err := d.Exec(`DROP TABLE notifications`); err != nil {
		t.Fatal(err)
	}
	if err := QueueNotification("k", "internal", "s", "b", 0, 0, now); err == nil {
		t.Fatal("expected error without table")
	}
}

func TestPendingAndRecentNotifications(t *testing.T) {
	d := notificationTestDB(t)
	insertNotification(t, d, "internal")
	insertNotification(t, d, "line")
	if _, err := d.Exec(`INSERT INTO notifications
		(customer_id, channel, kind, subject, body, scheduled_at, status)
		VALUES (1,'line','test','s','b','2000-01-01T00:00:00Z','sent')`); err != nil {
		t.Fatal(err)
	}
	pending := PendingNotifications(10)
	if len(pending) != 2 {
		t.Fatalf("pending=%d want 2", len(pending))
	}
	for _, n := range pending {
		if n.Status != "pending" {
			t.Fatalf("pending row status=%q", n.Status)
		}
	}
	recent := RecentNotifications(10)
	if len(recent) != 3 {
		t.Fatalf("recent=%d want 3", len(recent))
	}
	// Query error → nil for both.
	if _, err := d.Exec(`DROP TABLE notifications`); err != nil {
		t.Fatal(err)
	}
	if got := PendingNotifications(10); got != nil {
		t.Fatalf("PendingNotifications error case should be nil, got %v", got)
	}
	if got := RecentNotifications(10); got != nil {
		t.Fatalf("RecentNotifications error case should be nil, got %v", got)
	}
}

func TestProcessNotificationWrappers(t *testing.T) {
	// Clear credentials so Shared clients stay in mock mode regardless of repo .env.
	t.Setenv("LINE_CHANNEL_ID", "")
	t.Setenv("LINE_CHANNEL_SECRET", "")
	t.Setenv("LINE_CHANNEL_ACCESS_TOKEN", "")
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	d := notificationTestDB(t)
	if err := processNotifications(); err != nil {
		t.Fatal(err)
	}
	if n := ProcessNotificationsNow(); n != 0 {
		t.Fatalf("ProcessNotificationsNow=%d want 0", n)
	}
	// internal channel: marked sent without sender call.
	id := insertNotification(t, d, "internal")
	if n := ProcessNotificationsNow(); n != 1 {
		t.Fatalf("ProcessNotificationsNow=%d want 1", n)
	}
	if st, _ := notificationState(t, d, id); st != "sent" {
		t.Fatalf("status=%q want sent", st)
	}
	// "none" channel also short-circuits.
	id2 := insertNotification(t, d, "none")
	if n := ProcessNotificationsNow(); n != 1 {
		t.Fatalf("ProcessNotificationsNow=%d want 1", n)
	}
	if st, _ := notificationState(t, d, id2); st != "sent" {
		t.Fatalf("status=%q want sent", st)
	}
}

func TestProcessPendingChannelsWithoutBinding(t *testing.T) {
	d := notificationTestDB(t)
	// LINE channel but customer has no line binding (unknown customer → LEFT JOIN empty).
	idLine := insertNotificationFor(t, d, int64(999), "line")
	// Telegram channel, customer 1 has no telegram binding.
	idTg := insertNotificationFor(t, d, int64(1), "telegram")
	sent := false
	count, err := processPendingNotificationsWithSender(func(notificationItem) error {
		sent = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 || sent {
		t.Fatalf("count=%d sent=%v", count, sent)
	}
	for _, id := range []int64{idLine, idTg} {
		if st, _ := notificationState(t, d, id); st != "permanent_failure" {
			t.Fatalf("id=%d status=%q", id, st)
		}
	}
	// Telegram send error (non-permanent) → retry pending.
	if _, err := d.Exec(`UPDATE customers SET telegram_user_id='TG-1' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	idTg2 := insertNotificationFor(t, d, int64(1), "telegram")
	if _, err := processPendingNotificationsWithSender(func(notificationItem) error {
		return errors.New("tg outage")
	}); err != nil {
		t.Fatal(err)
	}
	if st, rc := notificationState(t, d, idTg2); st != "pending" || rc != 1 {
		t.Fatalf("status=%q retries=%d", st, rc)
	}
	// Query error: drop notifications table.
	if _, err := d.Exec(`DROP TABLE notifications`); err != nil {
		t.Fatal(err)
	}
	if _, err := processPendingNotificationsWithSender(func(notificationItem) error { return nil }); err == nil {
		t.Fatal("expected query error")
	}
}

func TestMarkSentFailedWithWrongToken(t *testing.T) {
	d := notificationTestDB(t)
	id := insertNotification(t, d, "internal")
	now := time.Now().UTC()
	expired := notificationProcessingTime(now.Add(-notificationLease))
	ok, token, err := claimNotification(id, now, expired)
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	// Wrong token → requireClaimedUpdate error (rows != 1).
	if err := markSent(id, "wrong-token"); err == nil {
		t.Fatal("markSent with wrong token should error")
	}
	// Non-permanent with wrong token → currentRetryCount fails first.
	if err := markFailed(id, "reason", false, now, "wrong-token"); err == nil {
		t.Fatal("markFailed with wrong token should error")
	}
	// Permanent with wrong token → update rows != 1.
	if err := markFailed(id, "reason", true, now, "wrong-token"); err == nil {
		t.Fatal("markFailed permanent with wrong token should error")
	}
	// Correct token works.
	if err := markSent(id, token); err != nil {
		t.Fatal(err)
	}
	// Claim on already-sent row → not claimed.
	ok, _, err = claimNotification(id, now, expired)
	if err != nil || ok {
		t.Fatalf("re-claim ok=%v err=%v", ok, err)
	}
}

func TestRequireClaimedUpdateDirect(t *testing.T) {
	d := notificationTestDB(t)
	res, err := d.Exec(`UPDATE notifications SET status='sent' WHERE id=999`)
	if err != nil {
		t.Fatal(err)
	}
	if err := requireClaimedUpdate(res, err, 999); err == nil {
		t.Fatal("0 rows affected should error")
	}
	if err := requireClaimedUpdate(nil, errors.New("exec fail"), 1); err == nil {
		t.Fatal("exec error should propagate")
	}
}

func TestDeliver(t *testing.T) {
	// Telegram: nil config → permanent error.
	err := deliver(nil, nil, nil, nil, notificationItem{channel: "telegram"})
	if !isPermanentNotificationError(err) {
		t.Fatalf("nil telegram cfg err=%v should be permanent", err)
	}
	// Telegram: valid token but empty chat → SendMessage error (no HTTP).
	tgCfg := &telegram.Config{BotToken: "123:abc"}
	tgClient := telegram.NewClient(tgCfg)
	if err := deliver(nil, nil, tgCfg, tgClient, notificationItem{channel: "telegram"}); err == nil {
		t.Fatal("empty chat id should error")
	}
	// Telegram mock client → nil.
	mockCfg := &telegram.Config{BotToken: "123:abc", Mock: true}
	if err := deliver(nil, nil, mockCfg, telegram.NewClient(mockCfg),
		notificationItem{channel: "telegram", telegramUserID: "42", body: "hi"}); err != nil {
		t.Fatal(err)
	}
	// SMS → permanent error.
	if err := deliver(nil, nil, nil, nil, notificationItem{channel: "sms"}); !isPermanentNotificationError(err) {
		t.Fatalf("sms err=%v should be permanent", err)
	}
	// LINE mock client (no credentials) → nil.
	lineClient := line.NewClient(&line.Config{})
	if err := deliver(nil, lineClient, nil, nil,
		notificationItem{channel: "line", lineUserID: "U1", body: "hi"}); err != nil {
		t.Fatal(err)
	}
}

func TestPermanentErrorIs(t *testing.T) {
	inner := errors.New("inner")
	err := permanentNotificationError(inner)
	if err.Error() != "inner" {
		t.Fatalf("Error()=%q", err.Error())
	}
	if !errors.Is(err, inner) {
		t.Fatal("Unwrap should expose inner error")
	}
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{permanentNotificationError(errors.New("x")), true},
		{&line.APIError{StatusCode: 400}, true},
		{&line.APIError{StatusCode: 404}, true},
		{&line.APIError{StatusCode: 408}, false},
		{&line.APIError{StatusCode: 429}, false},
		{&line.APIError{StatusCode: 500}, false},
		{errors.New("plain"), false},
	} {
		if got := isPermanentNotificationError(tc.err); got != tc.want {
			t.Errorf("isPermanent(%v)=%v want %v", tc.err, got, tc.want)
		}
	}
}

func TestQueueCompletedMissingWorkOrder(t *testing.T) {
	notificationTestDB(t)
	if err := QueueWorkOrderCompletedNotification(9999, 1); err == nil {
		t.Fatal("missing work order should error")
	}
}

func TestScanWorkOrderReadyQueueError(t *testing.T) {
	d := notificationTestDB(t)
	addCompletedWorkOrder(t, d, 501, 1, 51)
	if _, err := d.Exec(`CREATE TRIGGER reject_completed_notification
		BEFORE INSERT ON notifications BEGIN SELECT RAISE(ABORT, 'queue unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanWorkOrderReadyNow(); err == nil {
		t.Fatal("queue failure should be returned")
	}
	// Query error path: drop work_orders.
	d2 := notificationTestDB(t)
	if _, err := d2.Exec(`DROP TABLE work_orders`); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanWorkOrderReadyNow(); err == nil {
		t.Fatal("query error should be returned")
	}
}

func TestCompletedNotificationNoItems(t *testing.T) {
	d := notificationTestDB(t)
	addCompletedWorkOrder(t, d, 601, 1, 61)
	if err := QueueWorkOrderCompletedNotification(601, 1); err != nil {
		t.Fatal(err)
	}
	var body string
	if err := d.QueryRow(`SELECT body FROM notifications WHERE work_order_id=601`).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "無明細") {
		t.Fatalf("body=%q", body)
	}
}
