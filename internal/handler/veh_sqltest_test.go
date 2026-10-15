package handler

import (
	"database/sql"
	"net/http/httptest"
	"strings"
	"testing"

	"autorepairos/internal/db"
	_ "modernc.org/sqlite"
)

func TestCreateVehicleRealSQL(t *testing.T) {
	orig := db.DB
	t.Cleanup(func() { db.DB = orig })
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = d.Close() })
	db.DB = d

	// 建最小 vehicles 表（從 migration 021_vehicle_extra_fields 對應的結構）
	stmts := []string{
		`CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT, phone TEXT)`,
		`CREATE TABLE vehicles (id INTEGER PRIMARY KEY AUTOINCREMENT, customer_id INTEGER, plate_number TEXT UNIQUE,
			brand_model TEXT, vehicle_type TEXT, driver_name TEXT, driver_phone TEXT, contact_name TEXT, contact_phone TEXT,
			owner_name TEXT, owner_address TEXT, owner_id_number TEXT, license_expiry_date TEXT, color TEXT, door_count INTEGER,
			manufacture_year INTEGER, manufacture_date TEXT, license_issue_date TEXT, fuel_type TEXT, displacement INTEGER,
			body_style TEXT, seating_capacity INTEGER, curb_weight INTEGER, gross_weight INTEGER, tire_size TEXT,
			wheelbase INTEGER, vin TEXT, engine_number TEXT, inspection_due_date TEXT, next_maintenance_date TEXT,
			next_maintenance_mileage INTEGER, last_maintenance_date TEXT, last_maintenance_mileage INTEGER,
			next_maintenance_interval_months INTEGER, next_maintenance_interval_km INTEGER)`,
		`INSERT INTO customers (id, name, phone) VALUES (1, '測試', '0912')`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			t.Fatal(err)
		}
	}

	form := "customer_id=1&plate_number=TEST-1234&brand_model=Altis&vehicle_type=sedan&color=白&fuel_type=gasoline&body_style=sedan&maint_interval_months=6&maint_interval_km=5000"
	r := httptest.NewRequest("POST", "/vehicles", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	CreateVehicle(w, r)
	t.Logf("code=%d body=%q", w.Code, strings.TrimSpace(w.Body.String()))

	var n int
	if err := d.QueryRow("SELECT COUNT(*) FROM vehicles").Scan(&n); err != nil {
		t.Fatal(err)
	}
	t.Logf("vehicles rows: %d", n)
	if n != 1 {
		t.Fatalf("車輛未寫入")
	}
}
