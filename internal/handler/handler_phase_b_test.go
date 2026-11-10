package handler

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"autorepairos/internal/db"
)

func TestPhaseBWorkOrderStatePreserved(t *testing.T) {
	newMigratedTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (7,1,'ABC-1234','TOYOTA')`); err != nil {
		t.Fatal(err)
	}

	// Missing vehicle: same-screen re-render with error + complaint preserved.
	rec := httptest.NewRecorder()
	CreateWorkOrder(rec, postReq(t, "/work-orders", url.Values{
		"customer_complaint": {"煞車異音"}, "current_mileage": {"52000"},
	}))
	if rec.Code != 400 {
		t.Fatalf("code=%d want 400", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "請選擇車輛") || !strings.Contains(body, "煞車異音") || !strings.Contains(body, "52000") {
		t.Fatalf("error/complaint/mileage not preserved: %.300s", body)
	}

	// Missing mileage with vehicle picked: error + complaint preserved.
	rec = httptest.NewRecorder()
	CreateWorkOrder(rec, postReq(t, "/work-orders", url.Values{
		"vehicle_id": {"7"}, "customer_complaint": {"待保養"},
	}))
	if rec.Code != 400 {
		t.Fatalf("code=%d want 400", rec.Code)
	}
	body = rec.Body.String()
	if !strings.Contains(body, "請填寫進廠里程") || !strings.Contains(body, "待保養") || !strings.Contains(body, "ABC-1234") {
		t.Fatalf("mileage error/complaint/vehicle not preserved: %.300s", body)
	}
}
