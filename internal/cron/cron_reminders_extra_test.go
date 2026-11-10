package cron

import (
	"strings"
	"testing"
	"time"
)

func TestReminderDate(t *testing.T) {
	for raw, want := range map[string]string{
		"":                    "",
		"short":               "",
		"2026-13-45":          "",
		"2026-01-02":          "2026-01-02",
		"2026-01-02T15:00:00": "2026-01-02",
		" 2026-01-02 ":        "2026-01-02",
	} {
		if got := reminderDate(raw); got != want {
			t.Errorf("reminderDate(%q)=%q want %q", raw, got, want)
		}
	}
	today, cutoff := reminderDateRange()
	if today == "" || cutoff <= today {
		t.Fatalf("range %q..%q", today, cutoff)
	}
}

func TestCandidateChannel(t *testing.T) {
	for _, tc := range []struct {
		c       reminderCandidate
		channel string
		enabled bool
	}{
		{reminderCandidate{pref: "line", lineID: "L"}, "line", true},
		{reminderCandidate{pref: "LINE", lineID: "L"}, "line", true},
		{reminderCandidate{pref: " Telegram ", telegramID: "T"}, "telegram", true},
		{reminderCandidate{pref: "line"}, "line", false},
		{reminderCandidate{pref: "telegram"}, "telegram", false},
		{reminderCandidate{pref: "none"}, "", false},
		{reminderCandidate{pref: ""}, "", false},
	} {
		ch, ok := tc.c.channel()
		if ch != tc.channel || ok != tc.enabled {
			t.Errorf("%+v → %q,%v want %q,%v", tc.c, ch, ok, tc.channel, tc.enabled)
		}
	}
}

func TestScanWrappersEmpty(t *testing.T) {
	reminderTestDB(t)
	if err := scanMaintenanceDue(); err != nil {
		t.Fatal(err)
	}
	if err := scanBirthdayReminders(); err != nil {
		t.Fatal(err)
	}
	if err := scanAppointmentReminders(); err != nil {
		t.Fatal(err)
	}
	if n, err := ScanMaintenanceDueNow(); err != nil || n != 0 {
		t.Fatalf("ScanMaintenanceDueNow=%d/%v", n, err)
	}
}

func TestScanWorkOrderReadyWrapper(t *testing.T) {
	notificationTestDB(t)
	if err := scanWorkOrderReady(); err != nil {
		t.Fatal(err)
	}
}

func TestScanMaintenanceDueInternalErrors(t *testing.T) {
	cases := []struct {
		name string
		drop string
	}{
		{"maintenance query fails", "vehicle_mileages"},
		{"inspection query fails", "vehicle_inspections"},
		{"insurance query fails", "vehicle_insurances"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := reminderTestDB(t)
			if _, err := d.Exec(`DROP TABLE ` + tc.drop); err != nil {
				t.Fatal(err)
			}
			if _, err := scanMaintenanceDueInternal(); err == nil {
				t.Fatalf("%s: expected error", tc.name)
			}
		})
	}
}

func TestScanBirthdayAndAppointmentQueryErrors(t *testing.T) {
	d := reminderTestDB(t)
	if _, err := d.Exec(`DROP TABLE customers`); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanBirthdayRemindersNow(); err == nil {
		t.Fatal("birthday query should error without customers")
	}
	d2 := reminderTestDB(t)
	if _, err := d2.Exec(`DROP TABLE appointments`); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanAppointmentRemindersNow(); err == nil {
		t.Fatal("appointment query should error without appointments")
	}
}

func TestQueueVehicleReminderError(t *testing.T) {
	d := reminderTestDB(t)
	if _, err := d.Exec(`DROP TABLE notifications`); err != nil {
		t.Fatal(err)
	}
	c := reminderCandidate{customerID: 1, pref: "line", lineID: "L"}
	if _, err := queueVehicleReminder("k", "s", "b", c); err == nil {
		t.Fatal("expected error without notifications table")
	}
	// Disabled channel → false, nil without touching DB.
	c2 := reminderCandidate{customerID: 1, pref: "none"}
	added, err := queueVehicleReminder("k", "s", "b", c2)
	if added || err != nil {
		t.Fatalf("disabled channel: added=%v err=%v", added, err)
	}
}

func TestMaintenanceNoReasonSkipped(t *testing.T) {
	d := reminderTestDB(t)
	addReminderVehicle(t, d, "line", "LINE-1", "")
	// Future date (>7 days) and mileage not reached → no reasons.
	if _, err := d.Exec(`UPDATE vehicles SET next_maintenance_date=?, next_maintenance_mileage=90000 WHERE id=1`,
		time.Now().AddDate(0, 0, 30).Format("2006-01-02")); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO vehicle_mileages(vehicle_id,mileage) VALUES(1,1000)`); err != nil {
		t.Fatal(err)
	}
	if n, err := scanMaintenanceDueInternal(); err != nil || n != 0 {
		t.Fatalf("n=%d err=%v want 0", n, err)
	}
	// Invalid date string → reminderDate empty → skipped, no error.
	if _, err := d.Exec(`UPDATE vehicles SET next_maintenance_date='garbage', next_maintenance_mileage=0 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if n, err := scanMaintenanceDueInternal(); err != nil || n != 0 {
		t.Fatalf("invalid date: n=%d err=%v want 0", n, err)
	}
}

func TestInspectionWindowSkipped(t *testing.T) {
	d := reminderTestDB(t)
	addReminderVehicle(t, d, "line", "LINE-1", "")
	// Past date → out of window.
	if _, err := d.Exec(`UPDATE vehicles SET inspection_due_date=? WHERE id=1`,
		time.Now().AddDate(0, 0, -5).Format("2006-01-02")); err != nil {
		t.Fatal(err)
	}
	if n, err := scanMaintenanceDueInternal(); err != nil || n != 0 {
		t.Fatalf("past inspection: n=%d err=%v", n, err)
	}
	// Too far out (>30 days) → skipped.
	if _, err := d.Exec(`UPDATE vehicles SET inspection_due_date=? WHERE id=1`,
		time.Now().AddDate(0, 0, 60).Format("2006-01-02")); err != nil {
		t.Fatal(err)
	}
	if n, err := scanMaintenanceDueInternal(); err != nil || n != 0 {
		t.Fatalf("far inspection: n=%d err=%v", n, err)
	}
	// Invalid date → skipped.
	if _, err := d.Exec(`UPDATE vehicles SET inspection_due_date='not-a-date' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if n, err := scanMaintenanceDueInternal(); err != nil || n != 0 {
		t.Fatalf("invalid inspection: n=%d err=%v", n, err)
	}
}

func TestInsuranceWindowAndCompany(t *testing.T) {
	d := reminderTestDB(t)
	addReminderVehicle(t, d, "telegram", "", "TG-1")
	// Past expiry → skipped.
	if _, err := d.Exec(`INSERT INTO vehicle_insurances
		(id,vehicle_id,expiry_date,policy_number,company_no) VALUES(1,1,?,'P1','Co')`,
		time.Now().AddDate(0, 0, -10).Format("2006-01-02")); err != nil {
		t.Fatal(err)
	}
	if n, err := scanMaintenanceDueInternal(); err != nil || n != 0 {
		t.Fatalf("past insurance: n=%d err=%v", n, err)
	}
	// In-window with named company via insurance_companies join.
	if _, err := d.Exec(`INSERT INTO insurance_companies(id,name) VALUES(7,'富邦產險')`); err != nil {
		t.Fatal(err)
	}
	due := time.Now().AddDate(0, 0, 10).Format("2006-01-02")
	if _, err := d.Exec(`INSERT INTO vehicle_insurances
		(id,vehicle_id,expiry_date,policy_number,insurance_company_id) VALUES(2,1,?,'P2',7)`, due); err != nil {
		t.Fatal(err)
	}
	n, err := scanMaintenanceDueInternal()
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v want 1", n, err)
	}
	var body string
	if err := d.QueryRow(`SELECT body FROM notifications WHERE kind='insurance_due'`).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "富邦產險") || !strings.Contains(body, "P2") {
		t.Fatalf("body=%q", body)
	}
}

func TestAppointmentReminderDisabledCustomerSkipped(t *testing.T) {
	d := reminderTestDB(t)
	tomorrow := time.Now().In(time.FixedZone("Asia/Taipei", 8*60*60)).AddDate(0, 0, 1).Format("2006-01-02")
	// Customer prefers telegram but has no binding → skipped by channel().
	if _, err := d.Exec(`INSERT INTO customers(id,name,notification_preference) VALUES(1,'甲','telegram')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO vehicles(id,customer_id,plate_number) VALUES(1,1,'P1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO appointments(id,customer_id,vehicle_id,appointment_date,status)
		VALUES(1,1,1,?,'booked')`, tomorrow); err != nil {
		t.Fatal(err)
	}
	if n, err := ScanAppointmentRemindersNow(); err != nil || n != 0 {
		t.Fatalf("n=%d err=%v want 0", n, err)
	}
}
