package handler

import (
	"strings"
	"testing"
	"time"

	"autorepairos/internal/db"
)

func seedDiscordSlackCustomers(t *testing.T) {
	t.Helper()
	newMigratedTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone, discord_user_id, slack_user_id) VALUES (1,'王小明','0912','123456789012345678','U01234ABCDE')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (2,'陳大文','0933')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (11,1,'ABC-1234','Toyota')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO work_orders (id, customer_id, vehicle_id, status, total_amount, current_mileage) VALUES (1,1,11,'in_progress',1000,50000)`); err != nil {
		t.Fatal(err)
	}
}

func TestDiscordSlackCommands(t *testing.T) {
	seedDiscordSlackCustomers(t)
	for _, ch := range []struct{ name, uid string }{{"discord", "123456789012345678"}, {"slack", "U01234ABCDE"}} {
		if got := handleCustomerCommand(ch.name, ch.uid, "進度"); !strings.Contains(got, "ABC-1234") {
			t.Fatalf("%s 進度 reply=%q", ch.name, got)
		}
		if got := handleCustomerCommand(ch.name, ch.uid, "工單 1"); !strings.Contains(got, "ABC-1234") {
			t.Fatalf("%s 工單 reply=%q", ch.name, got)
		}
		if got := handleCustomerCommand(ch.name, ch.uid, "履歷"); got == "" {
			t.Fatalf("%s 履歷 empty", ch.name)
		}
		if got := handleCustomerCommand(ch.name, "nobody", "進度"); !strings.Contains(got, "查不到") {
			t.Fatalf("%s unbound reply=%q", ch.name, got)
		}
	}
}

func TestDiscordBookingChannelNote(t *testing.T) {
	seedDiscordSlackCustomers(t)
	date := time.Now().AddDate(0, 0, 3).Format("2006-01-02")
	if reply := startBooking("discord", "123456789012345678"); !strings.Contains(reply, "預約日期") {
		t.Fatalf("start reply=%q", reply)
	}
	if _, handled := handleBookingMessage("discord", "123456789012345678", date); !handled {
		t.Fatal("date step not handled")
	}
	if _, handled := handleBookingMessage("discord", "123456789012345678", "定期保養"); !handled {
		t.Fatal("service step not handled")
	}
	reply, handled := handleBookingMessage("discord", "123456789012345678", "確認")
	if !handled || !strings.Contains(reply, "預約成功") {
		t.Fatalf("confirm reply=%q", reply)
	}
	var notes string
	if err := db.DB.QueryRow(`SELECT notes FROM appointments`).Scan(&notes); err != nil {
		t.Fatal(err)
	}
	if notes != "Discord 預約" {
		t.Fatalf("notes=%q", notes)
	}
}

func TestChannelBindingUniqueness(t *testing.T) {
	seedDiscordSlackCustomers(t)
	// 已被客戶 1 佔用的 Discord ID 不可綁到客戶 2。
	if err := bindChannelUserByID("discord", "123456789012345678", 2); err == nil {
		t.Fatal("expected uniqueness rejection")
	}
	// 新 ID 可綁；重複綁自己（換 ID）允許。
	if err := bindChannelUserByID("slack", "U99999ZZZZ", 2); err != nil {
		t.Fatal(err)
	}
	if !isChannelBound("slack", "U99999ZZZZ") {
		t.Fatal("slack binding not found")
	}
	if err := validateChannelUserID("discord", "abc"); err == nil {
		t.Fatal("bad discord id accepted")
	}
	if err := validateChannelUserID("slack", "12345"); err == nil {
		t.Fatal("bad slack id accepted")
	}
	if err := validateChannelUserID("discord", "123456789012345678"); err != nil {
		t.Fatal(err)
	}
}