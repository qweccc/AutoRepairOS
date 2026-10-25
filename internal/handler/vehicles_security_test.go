package handler

import (
	"bytes"
	"strings"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

func TestVehicleListQueryUsesBatchLookups(t *testing.T) {
	if strings.Count(vehicleListJoins, "LEFT JOIN lookup_items") != 3 {
		t.Fatalf("vehicle lookup labels must be loaded by three JOINs: %s", vehicleListJoins)
	}
	if !strings.Contains(vehicleListJoins, "GROUP BY vehicle_id") {
		t.Fatalf("latest mileage must be batch aggregated: %s", vehicleListJoins)
	}
	if strings.Contains(vehicleListCols, "SELECT MAX(current_mileage)") {
		t.Fatal("vehicle list must not use a per-row mileage subquery")
	}
}

func TestVehicleSearchRowsEscapesDynamicFields(t *testing.T) {
	var out bytes.Buffer
	err := pages.VehicleSearchRows([]pages.VehicleRow{{
		ID:               1,
		PlateNumber:      `"><script>alert(1)</script>`,
		BrandModel:       `<img src=x onerror=alert(2)>`,
		VehicleTypeLabel: `"><svg onload=alert(3)>`,
		CustomerName:     `<script>alert(4)</script>`,
	}}).Render(t.Context(), &out)
	if err != nil {
		t.Fatal(err)
	}
	body := out.String()
	if strings.Contains(body, "<script>") || strings.Contains(body, "<img") || strings.Contains(body, "<svg") {
		t.Fatalf("unescaped markup in search result: %s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") || !strings.Contains(body, "&lt;img") {
		t.Fatalf("escaped values missing: %s", body)
	}
}

func TestPhotoViewAuthorizationRejectsUnknownRole(t *testing.T) {
	setupPhotoUploadTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO work_orders (id, technician_id) VALUES (20, 8)`); err != nil {
		t.Fatal(err)
	}
	allowed, err := canViewWorkOrder(model.User{ID: 7, Role: "guest"}, 20)
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("unknown role must not view work order photos")
	}
}
