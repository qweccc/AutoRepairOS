package handler

import (
	"strings"
	"testing"
	"time"

	"autorepairos/internal/db"
)

func seedTelegramCustomer(t *testing.T) {
	t.Helper()
	newMigratedTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone, telegram_user_id) VALUES (1,'王小明','0912','T1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (11,1,'ABC-1234','Toyota')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO work_orders (id, customer_id, vehicle_id, status, total_amount, current_mileage) VALUES (1,1,11,'in_progress',1000,50000)`); err != nil {
		t.Fatal(err)
	}
}

func TestTelegramCommands(t *testing.T) {
	seedTelegramCustomer(t)

	if got := handleCustomerCommand("telegram", "T1", "進度"); !strings.Contains(got, "ABC-1234") {
		t.Fatalf("進度 reply=%q", got)
	}
	if got := handleCustomerCommand("telegram", "T1", "工單 1"); !strings.Contains(got, "ABC-1234") {
		t.Fatalf("工單 1 reply=%q", got)
	}
	if got := handleCustomerCommand("telegram", "T1", "工單 999"); !strings.Contains(got, "找不到") {
		t.Fatalf("工單 999 reply=%q", got)
	}
	if got := handleCustomerCommand("telegram", "T1", "工單 abc"); !strings.Contains(got, "工單編號") {
		t.Fatalf("工單 abc reply=%q", got)
	}
	if got := handleCustomerCommand("telegram", "T1", "履歷"); got == "" {
		t.Fatal("履歷 empty")
	}
	if got := handleCustomerCommand("telegram", "T9", "進度"); !strings.Contains(got, "查不到") {
		t.Fatalf("unbound reply=%q", got)
	}
	if got := handleCustomerCommand("telegram", "T1", "你好"); !strings.Contains(got, "預約") {
		t.Fatalf("help reply=%q", got)
	}
}

func TestTelegramBookingFlow(t *testing.T) {
	seedTelegramCustomer(t)
	date := time.Now().AddDate(0, 0, 3).Format("2006-01-02")

	if reply := startBooking("telegram", "T1"); !strings.Contains(reply, "預約日期") {
		t.Fatalf("start reply=%q", reply)
	}
	// 單車直接進服務步驟。
	reply, handled := handleBookingMessage("telegram", "T1", date)
	if !handled || !strings.Contains(reply, "服務內容") {
		t.Fatalf("date step reply=%q handled=%v", reply, handled)
	}
	reply, handled = handleBookingMessage("telegram", "T1", "定期保養")
	if !handled || !strings.Contains(reply, "確認") {
		t.Fatalf("service step reply=%q", reply)
	}
	reply, handled = handleBookingMessage("telegram", "T1", "確認")
	if !handled || !strings.Contains(reply, "預約成功") {
		t.Fatalf("confirm reply=%q", reply)
	}
	var notes string
	var vid int64
	if err := db.DB.QueryRow(`SELECT notes, vehicle_id FROM appointments`).Scan(&notes, &vid); err != nil {
		t.Fatal(err)
	}
	if notes != "Telegram 預約" || vid != 11 {
		t.Fatalf("appointment notes=%q vehicle=%d", notes, vid)
	}
	// 對話結束；且與 LINE 通道隔離（同 userID 不共用 session）。
	if _, handled := handleBookingMessage("telegram", "T1", "確認"); handled {
		t.Fatal("session should be gone")
	}
	if _, handled := handleBookingMessage("line", "T1", date); handled {
		t.Fatal("line channel must not see telegram session")
	}
}