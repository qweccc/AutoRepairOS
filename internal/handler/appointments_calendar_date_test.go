package handler

import (
	"testing"

	"autorepairos/internal/db"
)

// Regression test for a real bug found while raising handler coverage:
// appointments.appointment_date is declared DATE (NUMERIC affinity), and
// modernc.org/sqlite scans such a column into a Go string as RFC3339Nano
// ("2026-11-10T00:00:00Z"). appointmentCalendar keys its map by the
// '2006-01-02' form, so every day rendered "此日沒有預約" — the calendar never
// showed an appointment. The earlier calendar tests missed this because their
// fixture schema declared the column TEXT.
//
// This test uses the real migrated schema, so it fails if the SELECT stops
// casting the column back to TEXT.
func TestAppointmentCalendarRealSchema(t *testing.T) {
	newMigratedTestDB(t)

	seedAppointment(t, "2026-11-10", "09:00")

	days, err := appointmentCalendar("2026-11-10", "day", "")
	if err != nil {
		t.Fatalf("appointmentCalendar: %v", err)
	}
	if len(days) != 1 {
		t.Fatalf("day view returned %d days, want 1", len(days))
	}
	if len(days[0].Appointments) != 1 {
		t.Fatalf("calendar day %s shows %d appointments, want 1 — DATE column scan regression",
			days[0].Date, len(days[0].Appointments))
	}
	if got := days[0].Appointments[0].AppointmentDate; got != "2026-11-10" {
		t.Fatalf("AppointmentDate = %q, want %q", got, "2026-11-10")
	}

	week, err := appointmentCalendar("2026-11-10", "week", "")
	if err != nil {
		t.Fatalf("appointmentCalendar(week): %v", err)
	}
	if len(week) != 7 {
		t.Fatalf("week view returned %d days, want 7", len(week))
	}
	var found bool
	for _, d := range week {
		if d.Date == "2026-11-10" && len(d.Appointments) == 1 {
			found = true
		}
	}
	if !found {
		t.Fatal("week view does not show the appointment on 2026-11-10")
	}
}

// seedAppointment inserts the minimum customer/vehicle/appointment rows the
// migrated schema requires.
func seedAppointment(t *testing.T, date, time string) {
	t.Helper()
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(q, args...); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	exec(`INSERT INTO customers(name, phone) VALUES('日曆回歸', '0912345678')`)
	exec(`INSERT INTO vehicles(customer_id, plate_number, brand_model) VALUES(1, 'CAL-0001', 'Probe')`)
	exec(`INSERT INTO appointments(customer_id, vehicle_id, appointment_date, appointment_time, service_type, status)
	      VALUES(1, 1, ?, ?, '定期保養', 'booked')`, date, time)
}
