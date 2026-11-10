package handler

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"autorepairos/internal/db"
)

func TestPhaseA4ValidationFormErrors(t *testing.T) {
	newMigratedTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (7,1,'ABC-1234','TOYOTA')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO work_orders (id, customer_id, vehicle_id, status, total_amount, current_mileage) VALUES (10,1,7,'pending',0,50000)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO inventory_items (id, part_number, name, bin_location, stock_quantity) VALUES (1,'PN-1','煞車皮','A-1',10)`); err != nil {
		t.Fatal(err)
	}
	check := func(name string, rec *httptest.ResponseRecorder, want string) {
		t.Helper()
		if rec.Code != 400 {
			t.Fatalf("%s: code=%d want 400", name, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), want) || !strings.Contains(rec.Body.String(), "msg-err") || !strings.Contains(rec.Body.String(), "返回表單重填") {
			t.Fatalf("%s: body missing form error for %q", name, want)
		}
	}

	// inventory update: missing required fields
	req := postReq(t, "/inventory/1/update", url.Values{"name": {"x"}})
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	UpdateInventoryItem(rec, req)
	check("inventory update missing", rec, "料號、名稱、庫位為必填")
	// stock adjust: zero quantity
	req = postReq(t, "/inventory/1/adjust", url.Values{"quantity": {"0"}})
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	ApplyStockAdjust(rec, req)
	check("stock adjust zero", rec, "異動數量不可為 0")

	// worklogs: missing technician
	rec = httptest.NewRecorder()
	AddTechnicianWorklog(rec, postReq(t, "/work-orders/10/worklogs", url.Values{"work_date": {"2026-01-01"}, "hours": {"1"}}), Actor{ID: 1, Role: "owner"})
	check("worklog no tech", rec, "請選擇技師")
	// worklogs: bad date via update
	req2 := postReq(t, "/work-orders/10/worklogs/1/update", url.Values{"work_date": {"bad"}, "hours": {"1"}})
	req2.SetPathValue("id", "10")
	req2.SetPathValue("log_id", "1")
	rec = httptest.NewRecorder()
	UpdateTechnicianWorklog(rec, req2)
	check("worklog update bad date", rec, "工作日期格式錯誤")

	// legacy insurance: missing vehicle -> templ form re-rendered in place
	rec = httptest.NewRecorder()
	CreateVehicleInsurance(rec, postReq(t, "/vehicles/insurance", url.Values{"expiry_date": {"2027-01-01"}}))
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "請選擇有效的車輛") || !strings.Contains(rec.Body.String(), "新增車輛投保紀錄") {
		t.Fatalf("insurance no vehicle: code=%d body=%.200s", rec.Code, rec.Body.String())
	}
	// legacy insurance: bad expiry -> form re-rendered
	rec = httptest.NewRecorder()
	CreateVehicleInsurance(rec, postReq(t, "/vehicles/insurance", url.Values{"vehicle_id": {"7"}, "expiry_date": {"bad"}}))
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "保單有效期限格式錯誤") || !strings.Contains(rec.Body.String(), "新增車輛投保紀錄") {
		t.Fatalf("insurance bad expiry: code=%d body=%.200s", rec.Code, rec.Body.String())
	}
	// inspection: missing vehicle
	rec = httptest.NewRecorder()
	CreateVehicleInspection(rec, postReq(t, "/vehicles/inspection", url.Values{}))
	check("inspection no vehicle", rec, "請選擇有效的車輛")

	// appointments: missing fields
	rec = httptest.NewRecorder()
	CreateAppointment(rec, postReq(t, "/appointments", url.Values{}))
	check("appointment missing", rec, "客戶、車輛、預約日期為必填")
	// appointments: vehicle not belonging
	rec = httptest.NewRecorder()
	CreateAppointment(rec, postReq(t, "/appointments", url.Values{"customer_id": {"1"}, "vehicle_id": {"999"}, "appointment_date": {"2026-11-10"}, "appointment_time": {"09:00"}}))
	check("appointment wrong vehicle", rec, "所選車輛不屬於所選客戶")
	// appointments update: missing fields
	req3 := postReq(t, "/appointments/1/update", url.Values{})
	req3.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	UpdateAppointment(rec, req3)
	check("appointment update missing", rec, "客戶、車輛、預約日期為必填")
}
