package handler

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/integration/line"
)

func TestParseBookingDate(t *testing.T) {
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	nextYear := time.Now().AddDate(1, 0, 0).Format("2006")
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"2026-12-01", "2026-12-01", true},
		{"2026/12/1", "2026-12-01", true},
		{"明天", tomorrow, true},
		{"12/1", func() string {
			y := time.Now().Year()
			if time.Now().Format("2006-01-02") > time.Date(y, 12, 1, 0, 0, 0, 0, time.Local).Format("2006-01-02") {
				y++
			}
			return time.Date(y, 12, 1, 0, 0, 0, 0, time.Local).Format("2006-01-02")
		}(), true},
		{"abc", "", false},
		{"2/30", "", false},
		{"13/1", "", false},
		{"", "", false},
	}
	_ = nextYear
	for _, tc := range cases {
		got, ok := parseBookingDate(tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("parseBookingDate(%q)=(%q,%v) want (%q,%v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func seedBookingCustomer(t *testing.T, vehicles int) {
	t.Helper()
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone, line_user_id) VALUES (1,'王小明','0912','U1')`); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= vehicles; i++ {
		if _, err := db.DB.Exec(`INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (?,?,?,?)`,
			10+i, 1, "ABC-"+strconv.Itoa(i), "Toyota"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBookingFlowMultiVehicle(t *testing.T) {
	newMigratedTestDB(t)
	seedBookingCustomer(t, 2)
	date := time.Now().AddDate(0, 0, 3).Format("2006-01-02")

	reply := startBooking("line", "U1")
	if !strings.Contains(reply, "預約日期") {
		t.Fatalf("start reply=%q", reply)
	}
	// 多車 → 選車提示。
	reply, handled := handleBookingMessage("line", "U1", date)
	if !handled || !strings.Contains(reply, "編號") {
		t.Fatalf("date step reply=%q handled=%v", reply, handled)
	}
	// 選 2 號車。
	reply, handled = handleBookingMessage("line", "U1", "2")
	if !handled || !strings.Contains(reply, "ABC-2") || !strings.Contains(reply, "服務內容") {
		t.Fatalf("vehicle step reply=%q", reply)
	}
	// 服務內容 → 確認提示。
	reply, handled = handleBookingMessage("line", "U1", "定期保養")
	if !handled || !strings.Contains(reply, "確認") {
		t.Fatalf("service step reply=%q", reply)
	}
	// 確認 → 建立預約。
	reply, handled = handleBookingMessage("line", "U1", "確認")
	if !handled || !strings.Contains(reply, "預約成功") {
		t.Fatalf("confirm reply=%q", reply)
	}
	var cid, vid int64
	var d, svc, status, notes string
	err := db.DB.QueryRow(`SELECT customer_id, vehicle_id, appointment_date, service_type, status, notes FROM appointments`).
		Scan(&cid, &vid, &d, &svc, &status, &notes)
	if err != nil {
		t.Fatal(err)
	}
	if cid != 1 || vid != 12 || len(d) < 10 || d[:10] != date || svc != "定期保養" || status != "booked" || notes != "LINE 預約" {
		t.Fatalf("appointment wrong: %d %d %s %s %s %s", cid, vid, d, svc, status, notes)
	}
	// 對話結束。
	if _, handled := handleBookingMessage("line", "U1", "確認"); handled {
		t.Fatal("session should be gone after confirm")
	}
}

func TestBookingSingleVehicleSkipsChoice(t *testing.T) {
	newMigratedTestDB(t)
	seedBookingCustomer(t, 1)
	date := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	startBooking("line", "U1")
	reply, _ := handleBookingMessage("line", "U1", date)
	if !strings.Contains(reply, "服務內容") || strings.Contains(reply, "編號") {
		t.Fatalf("single vehicle should skip choice: %q", reply)
	}
}

func TestBookingCancelAndPastDate(t *testing.T) {
	newMigratedTestDB(t)
	seedBookingCustomer(t, 1)
	startBooking("line", "U1")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	reply, _ := handleBookingMessage("line", "U1", yesterday)
	if !strings.Contains(reply, "過去") {
		t.Fatalf("past date reply=%q", reply)
	}
	reply, _ = handleBookingMessage("line", "U1", "取消")
	if !strings.Contains(reply, "已取消") {
		t.Fatalf("cancel reply=%q", reply)
	}
	if _, handled := handleBookingMessage("line", "U1", "anything"); handled {
		t.Fatal("session should be gone after cancel")
	}
}

func TestBookingNoVehiclesAndExpiry(t *testing.T) {
	newMigratedTestDB(t)
	seedBookingCustomer(t, 0)
	if reply := startBooking("line", "U1"); !strings.Contains(reply, "沒有車輛") {
		t.Fatalf("no vehicles reply=%q", reply)
	}
	// 過期對話不攔截。
	if _, err := db.DB.Exec(`INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (21,1,'DEF-9','Mazda')`); err != nil {
		t.Fatal(err)
	}
	startBooking("line", "U1")
	bookingMu.Lock()
	bookingSessions["line:U1"].Expires = time.Now().Add(-time.Minute)
	bookingMu.Unlock()
	if _, handled := handleBookingMessage("line", "U1", "2026-12-01"); handled {
		t.Fatal("expired session should not intercept")
	}
}

func TestBookingCommandDispatch(t *testing.T) {
	newMigratedTestDB(t)
	seedBookingCustomer(t, 1)
	reply := handleCustomerMessage(nil, line.MessageEvent{UserID: "U1", Text: "預約"})
	if !strings.Contains(reply, "預約日期") {
		t.Fatalf("dispatch reply=%q", reply)
	}
}