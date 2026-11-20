package handler

import (
	"strings"
	"testing"
	"time"

	"autorepairos/internal/db"
)

func seedApprovalCustomer(t *testing.T) {
	t.Helper()
	newMigratedTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone, line_user_id, telegram_user_id, notification_preference) VALUES (1,'王小明','0912','U1','T1','line')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (11,1,'ABC-1234','Toyota')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO work_orders (id, customer_id, vehicle_id, status, total_amount, current_mileage) VALUES (1,1,11,'in_progress',0,50000)`); err != nil {
		t.Fatal(err)
	}
}

func seedPendingApproval(t *testing.T, id, woID int64) {
	t.Helper()
	exp := time.Now().Add(24 * time.Hour).Format("2006-01-02 15:04:05")
	summary := `[{"description":"煞車來令片","quantity":2,"unit_price":1500,"item_type":"part"}]`
	if _, err := db.DB.Exec(`INSERT INTO approval_requests (id, work_order_id, customer_id, token, item_summary, status, expires_at, created_at, decision_source)
		VALUES (?, ?, 1, ?, ?, 'pending', ?, ?, 'line')`,
		id, woID, "tok"+strings.Repeat("x", 10)+string(rune('0'+id)), summary, exp, time.Now().Format("2006-01-02 15:04:05")); err != nil {
		t.Fatal(err)
	}
}

func TestChatApprovalApprove(t *testing.T) {
	seedApprovalCustomer(t)
	seedPendingApproval(t, 1, 1)

	reply := handleCustomerCommand("line", "U1", "同意")
	if !strings.Contains(reply, "已同意") || !strings.Contains(reply, "煞車來令片") {
		t.Fatalf("reply=%q", reply)
	}
	var status, source string
	if err := db.DB.QueryRow(`SELECT status, decision_source FROM approval_requests WHERE id=1`).Scan(&status, &source); err != nil {
		t.Fatal(err)
	}
	if status != "approved" || source != "chat" {
		t.Fatalf("status=%q source=%q", status, source)
	}
	var items int
	var total float64
	var woApproval string
	db.DB.QueryRow(`SELECT COUNT(*) FROM work_order_items WHERE work_order_id=1`).Scan(&items)
	db.DB.QueryRow(`SELECT total_amount, approval_status FROM work_orders WHERE id=1`).Scan(&total, &woApproval)
	if items != 1 || total != 3000 || woApproval != "approved" {
		t.Fatalf("items=%d total=%.0f woApproval=%q", items, total, woApproval)
	}
	// 已決定後再回同意 → 沒有待確認。
	if reply := handleCustomerCommand("line", "U1", "同意"); !strings.Contains(reply, "目前沒有") {
		t.Fatalf("second reply=%q", reply)
	}
}

func TestChatApprovalReject(t *testing.T) {
	seedApprovalCustomer(t)
	seedPendingApproval(t, 1, 1)
	reply := handleCustomerCommand("telegram", "T1", "拒絕")
	if !strings.Contains(reply, "已拒絕") {
		t.Fatalf("reply=%q", reply)
	}
	var status string
	var items int
	db.DB.QueryRow(`SELECT status FROM approval_requests WHERE id=1`).Scan(&status)
	db.DB.QueryRow(`SELECT COUNT(*) FROM work_order_items WHERE work_order_id=1`).Scan(&items)
	if status != "rejected" || items != 0 {
		t.Fatalf("status=%q items=%d", status, items)
	}
}

func TestChatApprovalMultipleAndFilter(t *testing.T) {
	seedApprovalCustomer(t)
	seedPendingApproval(t, 1, 1)
	if _, err := db.DB.Exec(`INSERT INTO work_orders (id, customer_id, vehicle_id, status, total_amount, current_mileage) VALUES (2,1,11,'in_progress',0,50000)`); err != nil {
		t.Fatal(err)
	}
	seedPendingApproval(t, 2, 2)

	reply := handleCustomerCommand("line", "U1", "同意")
	if !strings.Contains(reply, "多筆") || !strings.Contains(reply, "同意 工單編號") {
		t.Fatalf("multi reply=%q", reply)
	}
	if reply := handleCustomerCommand("line", "U1", "同意 2"); !strings.Contains(reply, "已同意工單 #2") {
		t.Fatalf("filter reply=%q", reply)
	}
	if reply := handleCustomerCommand("line", "U1", "同意 999"); !strings.Contains(reply, "沒有待確認") {
		t.Fatalf("bad filter reply=%q", reply)
	}
}

func TestChatApprovalUnboundAndNone(t *testing.T) {
	seedApprovalCustomer(t)
	if reply := handleCustomerCommand("line", "U9", "同意"); !strings.Contains(reply, "查不到") {
		t.Fatalf("unbound reply=%q", reply)
	}
	if reply := handleCustomerCommand("line", "U1", "同意"); !strings.Contains(reply, "目前沒有") {
		t.Fatalf("none reply=%q", reply)
	}
}

func TestCustomerNotificationChannel(t *testing.T) {
	seedApprovalCustomer(t)
	// 偏好 line 且已綁 → line。
	if ch := customerNotificationChannel(1); ch != "line" {
		t.Fatalf("ch=%q", ch)
	}
	// 偏好改成 discord 但未綁、line 也解綁，telegram 有綁 → 降級 telegram。
	if _, err := db.DB.Exec(`UPDATE customers SET notification_preference='discord', line_user_id=NULL WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if ch := customerNotificationChannel(1); ch != "telegram" {
		t.Fatalf("fallback ch=%q", ch)
	}
	// 全解綁 → ""。
	if _, err := db.DB.Exec(`UPDATE customers SET telegram_user_id=NULL WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if ch := customerNotificationChannel(1); ch != "" {
		t.Fatalf("unbound ch=%q", ch)
	}
}