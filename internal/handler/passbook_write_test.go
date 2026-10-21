package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/integration/line"
)

func passbookWriteTestDB(t *testing.T) {
	t.Helper()
	d, err := openTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	db.DB = d

	for _, statement := range []string{
		`CREATE TABLE customers (
			id INTEGER PRIMARY KEY, name TEXT, line_user_id TEXT,
			total_spent REAL DEFAULT 0, unpaid_amount REAL DEFAULT 0,
			notification_preference TEXT DEFAULT 'none', telegram_user_id TEXT, deleted_at TEXT
		)`,
		`CREATE VIEW active_customers AS SELECT * FROM customers WHERE deleted_at IS NULL`,
		`CREATE TABLE vehicles (
			id INTEGER PRIMARY KEY, customer_id INTEGER, plate_number TEXT, deleted_at TEXT
		)`,
		`CREATE VIEW active_vehicles AS SELECT * FROM vehicles WHERE deleted_at IS NULL`,
		`CREATE TABLE work_orders (
			id INTEGER PRIMARY KEY, customer_id INTEGER, vehicle_id INTEGER, status TEXT,
			total_amount REAL DEFAULT 0, updated_at TEXT, deleted_at TEXT
		)`,
		`CREATE VIEW active_work_orders AS SELECT * FROM work_orders WHERE deleted_at IS NULL`,
		`CREATE TABLE work_order_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT, work_order_id INTEGER, item_type TEXT,
			description TEXT, quantity INTEGER DEFAULT 1, subtotal REAL DEFAULT 0,
			inventory_item_id INTEGER
		)`,
		`CREATE TABLE passbook (
			id INTEGER PRIMARY KEY AUTOINCREMENT, customer_id INTEGER NOT NULL,
			vehicle_id INTEGER NOT NULL, work_order_id INTEGER, service_type TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '', parts_cost REAL NOT NULL DEFAULT 0,
			labor_cost REAL NOT NULL DEFAULT 0, subcontract_cost REAL NOT NULL DEFAULT 0,
			subtotal REAL NOT NULL DEFAULT 0, tax REAL NOT NULL DEFAULT 0,
			discount_amount REAL NOT NULL DEFAULT 0, total_amount REAL NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'pending', notes TEXT NOT NULL DEFAULT '',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP, updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE work_order_sop_records (
			id INTEGER PRIMARY KEY, work_order_id INTEGER, required INTEGER DEFAULT 0, completed INTEGER DEFAULT 0
		)`,
		`CREATE TABLE inventory_items (
			id INTEGER PRIMARY KEY, stock_quantity INTEGER DEFAULT 0, reserved_quantity INTEGER DEFAULT 0
		)`,
		`CREATE TABLE inventory_movements (
			id INTEGER PRIMARY KEY, inventory_item_id INTEGER, delta INTEGER,
			before_quantity INTEGER DEFAULT 0, after_quantity INTEGER DEFAULT 0,
			reason TEXT DEFAULT '', source TEXT, work_order_id INTEGER, created_by INTEGER
		)`,
		`CREATE TABLE receivables (
			id INTEGER PRIMARY KEY, voucher_no TEXT, customer_id INTEGER, vehicle_id INTEGER,
			work_order_id INTEGER, pay_date TEXT, detail_total REAL, real_total REAL,
			discount REAL, tax REAL, pay_total REAL, cash REAL, card REAL,
			check_amount REAL, transfer_amount REAL DEFAULT 0, transfer_reference TEXT DEFAULT '',
			card_no TEXT, check_no TEXT, check_date TEXT, bank TEXT, notes TEXT,
			source TEXT, created_by INTEGER, voided INTEGER DEFAULT 0
		)`,
		`CREATE TABLE notifications (
			id INTEGER PRIMARY KEY AUTOINCREMENT, work_order_id INTEGER, customer_id INTEGER,
			channel TEXT, kind TEXT, subject TEXT, body TEXT, scheduled_at TEXT,
			status TEXT DEFAULT 'pending'
		)`,
		`INSERT INTO customers (id, name, line_user_id) VALUES (1, '履歷車主', 'LINE-OWNER')`,
		`INSERT INTO vehicles (id, customer_id, plate_number) VALUES (1, 1, 'PASS-001')`,
		`INSERT INTO work_orders (id, customer_id, vehicle_id, status, total_amount)
			VALUES (10, 1, 1, 'completed', 350)`,
		`INSERT INTO work_order_items (work_order_id,item_type,description,quantity,subtotal)
			VALUES (10,'part','煞車來令片',2,100),
			       (10,'labor','更換工資',1,200),
			       (10,'subcontract','碟盤研磨',1,50)`,
	} {
		if _, err := d.Exec(statement); err != nil {
			t.Fatalf("setup database: %v\nSQL: %s", err, statement)
		}
	}
}

func postWorkOrderStatus(t *testing.T, id int64, status string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"status": {status}}
	r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/work-orders/%d/status", id), strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.SetPathValue("id", fmt.Sprint(id))
	w := httptest.NewRecorder()
	UpdateWorkOrderStatus(w, r)
	return w
}

func TestCheckoutCreatesPassbookOnce(t *testing.T) {
	passbookWriteTestDB(t)

	for attempt := 0; attempt < 2; attempt++ {
		w := postWorkOrderStatus(t, 10, "paid")
		if w.Code != http.StatusFound {
			t.Fatalf("checkout attempt %d status=%d body=%s", attempt+1, w.Code, w.Body.String())
		}
	}

	var count int
	var parts, labor, subcontract, subtotal, total float64
	var status, description string
	if err := db.DB.QueryRow(`SELECT COUNT(*), MAX(parts_cost), MAX(labor_cost),
		MAX(subcontract_cost), MAX(subtotal), MAX(total_amount), MAX(status), MAX(description)
		FROM passbook WHERE work_order_id=10`).
		Scan(&count, &parts, &labor, &subcontract, &subtotal, &total, &status, &description); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("passbook count=%d, want exactly one", count)
	}
	if parts != 100 || labor != 200 || subcontract != 50 || subtotal != 350 || total != 350 || status != "paid" {
		t.Fatalf("unexpected passbook amounts/status: part %.0f labor %.0f subcontract %.0f subtotal %.0f total %.0f status %q",
			parts, labor, subcontract, subtotal, total, status)
	}
	if !strings.Contains(description, "煞車來令片 x2") {
		t.Fatalf("passbook description %q does not summarize work-order items", description)
	}
}

func TestCheckoutPassbookFailureDoesNotFailCheckout(t *testing.T) {
	passbookWriteTestDB(t)
	if _, err := db.DB.Exec(`CREATE TRIGGER reject_passbook_insert
		BEFORE INSERT ON passbook BEGIN SELECT RAISE(ABORT, 'passbook unavailable'); END`); err != nil {
		t.Fatal(err)
	}

	w := postWorkOrderStatus(t, 10, "paid")
	if w.Code != http.StatusFound {
		t.Fatalf("checkout status=%d body=%s; passbook failure must not block checkout", w.Code, w.Body.String())
	}
	var status string
	if err := db.DB.QueryRow(`SELECT status FROM work_orders WHERE id=10`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "paid" {
		t.Fatalf("work order status=%q, want paid", status)
	}
}

func TestWorkOrderCompletionQueuesOneNotification(t *testing.T) {
	passbookWriteTestDB(t)
	if _, err := db.DB.Exec(`UPDATE work_orders SET status='in_progress' WHERE id=10`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`UPDATE customers SET notification_preference='line', line_user_id='LINE-OWNER' WHERE id=1`); err != nil {
		t.Fatal(err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		w := postWorkOrderStatus(t, 10, "completed")
		if w.Code != http.StatusFound {
			t.Fatalf("completion attempt %d status=%d body=%s", attempt+1, w.Code, w.Body.String())
		}
	}
	var count int
	var channel, kind, body string
	if err := db.DB.QueryRow(`SELECT COUNT(*), MAX(channel), MAX(kind), MAX(body)
		FROM notifications WHERE work_order_id=10`).Scan(&count, &channel, &kind, &body); err != nil {
		t.Fatal(err)
	}
	if count != 1 || channel != "line" || kind != "work_order_completed" {
		t.Fatalf("notification count=%d channel=%q kind=%q", count, channel, kind)
	}
	if !strings.Contains(body, "PASS-001") || !strings.Contains(body, "工單編號：#10") {
		t.Fatalf("notification card missing vehicle/order information: %s", body)
	}
}

func TestWorkOrderCompletionNotificationFailureDoesNotFailCompletion(t *testing.T) {
	passbookWriteTestDB(t)
	if _, err := db.DB.Exec(`UPDATE work_orders SET status='in_progress' WHERE id=10`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`CREATE TRIGGER reject_completion_notification
		BEFORE INSERT ON notifications BEGIN SELECT RAISE(ABORT, 'notification unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	w := postWorkOrderStatus(t, 10, "completed")
	if w.Code != http.StatusFound {
		t.Fatalf("completion status=%d body=%s; notification failure must not block completion", w.Code, w.Body.String())
	}
	var status string
	if err := db.DB.QueryRow(`SELECT status FROM work_orders WHERE id=10`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "completed" {
		t.Fatalf("work order status=%q, want completed", status)
	}
}

func TestLinePassbookLookupIsBoundToCustomerVehicles(t *testing.T) {
	passbookWriteTestDB(t)
	for _, statement := range []string{
		`INSERT INTO customers (id,name,line_user_id) VALUES (2,'其他車主','LINE-OTHER')`,
		`INSERT INTO vehicles (id,customer_id,plate_number) VALUES (2,2,'OTHER-999')`,
		`INSERT INTO work_orders (id,customer_id,vehicle_id,status,total_amount) VALUES (20,2,2,'paid',999)`,
		`INSERT INTO passbook (customer_id,vehicle_id,work_order_id,service_type,description,total_amount,status)
			VALUES (1,1,10,'maintenance','定期保養',350,'paid')`,
		`INSERT INTO passbook (customer_id,vehicle_id,work_order_id,service_type,description,total_amount,status)
			VALUES (2,2,20,'repair','其他客戶資料',999,'paid')`,
	} {
		if _, err := db.DB.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}

	own := customerPassbookSummary("LINE-OWNER")
	if !strings.Contains(own, "PASS-001") || !strings.Contains(own, "定期保養") || strings.Contains(own, "OTHER-999") {
		t.Fatalf("LINE 履歷回覆未符合客戶資料邊界: %s", own)
	}
	if got := customerPassbookSummary("LINE-UNBOUND"); !strings.Contains(got, "查不到") {
		t.Fatalf("unbound LINE should not get a history: %s", got)
	}

	if _, err := db.DB.Exec(`DELETE FROM passbook WHERE customer_id=1`); err != nil {
		t.Fatal(err)
	}
	if got := handleCustomerMessage(nil, line.MessageEvent{UserID: "LINE-OWNER", Text: "履歷"}); got != "目前沒有維修履歷紀錄" {
		t.Fatalf("empty history reply=%q", got)
	}
}
