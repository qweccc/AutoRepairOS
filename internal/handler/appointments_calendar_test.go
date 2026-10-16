package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

func appointmentCalendarTestDB(t *testing.T) {
	t.Helper()
	d, err := openTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	db.DB = d
	for _, statement := range []string{
		`CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT, deleted_at TEXT)`,
		`CREATE VIEW active_customers AS SELECT * FROM customers WHERE deleted_at IS NULL`,
		`CREATE TABLE vehicles (id INTEGER PRIMARY KEY, customer_id INTEGER, plate_number TEXT, deleted_at TEXT)`,
		`CREATE VIEW active_vehicles AS SELECT * FROM vehicles WHERE deleted_at IS NULL`,
		`CREATE TABLE appointments (
			id INTEGER PRIMARY KEY, customer_id INTEGER, vehicle_id INTEGER,
			appointment_date TEXT, appointment_time TEXT, service_type TEXT,
			bay TEXT, status TEXT, deleted_at TEXT
		)`,
		`CREATE VIEW active_appointments AS SELECT * FROM appointments WHERE deleted_at IS NULL`,
		`INSERT INTO customers VALUES (1,'客戶甲',NULL),(2,'客戶乙',NULL)`,
		`INSERT INTO vehicles VALUES (1,1,'ABC-1234',NULL),(2,2,'XYZ-9876',NULL)`,
		`INSERT INTO appointments VALUES
			(1,1,1,'2026-10-19','09:00','定期保養','LIFT','booked',NULL),
			(2,2,2,'2026-10-22','11:00','煞車檢查','ALIGN','confirmed',NULL),
			(3,1,1,'2026-10-19','10:00','輪胎更換','TIRE','cancelled',NULL),
			(4,1,1,'2026-10-26','12:00','其他服務','DIAG','booked',NULL),
			(5,1,1,'2026-10-19','23:30','夜間檢查','BODY','booked',NULL)`,
	} {
		if _, err := d.Exec(statement); err != nil {
			t.Fatalf("setup failed: %v\n%s", err, statement)
		}
	}
}

func TestAppointmentCalendarDayAndWeekRendering(t *testing.T) {
	appointmentCalendarTestDB(t)

	dayRequest := httptest.NewRequest("GET", "/api/appointments/calendar?date=2026-10-19&view=day", nil)
	dayResponse := httptest.NewRecorder()
	AppointmentCalendarAPI(dayResponse, dayRequest, model.User{})
	dayHTML := dayResponse.Body.String()
	if dayResponse.Code != 200 || !strings.Contains(dayHTML, "ABC-1234") ||
		!strings.Contains(dayHTML, "09:00") || !strings.Contains(dayHTML, "頂高機") ||
		!strings.Contains(dayHTML, "已預約") || strings.Contains(dayHTML, "XYZ-9876") {
		t.Fatalf("unexpected day view (status %d): %s", dayResponse.Code, dayHTML)
	}

	weekRequest := httptest.NewRequest("GET",
		"/api/appointments/calendar?date=2026-10-21&view=week&status=confirmed", nil)
	weekResponse := httptest.NewRecorder()
	AppointmentCalendarAPI(weekResponse, weekRequest, model.User{})
	weekHTML := weekResponse.Body.String()
	if weekResponse.Code != 200 || !strings.Contains(weekHTML, "XYZ-9876") ||
		!strings.Contains(weekHTML, "煞車檢查") || strings.Contains(weekHTML, "ABC-1234") ||
		strings.Count(weekHTML, "<h3") != 7 {
		t.Fatalf("unexpected filtered week view (status %d): %s", weekResponse.Code, weekHTML)
	}
	if !strings.Contains(weekHTML, "hx-get=\"/api/appointments/calendar\"") ||
		!strings.Contains(weekHTML, "上一週") || !strings.Contains(weekHTML, "下一週") {
		t.Fatalf("week view is missing HTMX controls: %s", weekHTML)
	}
}

func TestBayConflictUsesOneHourHalfOpenSlots(t *testing.T) {
	appointmentCalendarTestDB(t)

	if _, _, _, _, hit := bayConflict("LIFT", "2026-10-19", "09:30", 0); !hit {
		t.Fatal("09:30 must overlap the existing 09:00 one-hour slot")
	}
	if _, _, _, _, hit := bayConflict("LIFT", "2026-10-19", "10:00", 0); hit {
		t.Fatal("10:00 starts at the exclusive end of the 09:00 slot")
	}
	if _, _, _, _, hit := bayConflict("TIRE", "2026-10-19", "09:00", 0); hit {
		t.Fatal("a different bay must not conflict")
	}
	if _, _, _, _, hit := bayConflict("LIFT", "2026-10-19", "09:00", 1); hit {
		t.Fatal("editing an appointment must exclude its own row")
	}
	if _, _, _, _, hit := bayConflict("BODY", "2026-10-20", "00:00", 0); !hit {
		t.Fatal("an appointment crossing midnight must block the overlapping next-day slot")
	}
	if _, _, _, _, hit := bayConflict("BODY", "2026-10-20", "00:30", 0); hit {
		t.Fatal("the next-day slot beginning at the exclusive end should be allowed")
	}
}
