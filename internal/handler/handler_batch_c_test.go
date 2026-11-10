package handler

import (
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

func TestBatchCTelegramBinding(t *testing.T) {
	// offset file helpers: save/load round-trip via bindingOffsetPath's directory.
	orig := db.DB
	origPath := db.Path()
	// newMigratedTestDB replaces db.DB; dbFilePath stays whatever Init set.
	// Instead exercise load/save via bindingOffsetPath's directory directly.
	newMigratedTestDB(t)
	defer func() { db.DB = orig }()
	_ = origPath
	// load with no file -> 0
	if loadBindingOffset() != 0 {
		t.Fatal("expected 0 offset")
	}
	// prompted map
	if wasPrompted(42) {
		t.Fatal("not prompted yet")
	}
	markPrompted(42)
	if !wasPrompted(42) {
		t.Fatal("should be prompted")
	}
	// save + load round-trip (path may point at ./data relative to cwd; ensure dir exists)
	saveBindingOffset(12345)
	if got := loadBindingOffset(); got != 12345 {
		t.Fatalf("offset=%d", got)
	}
	// corrupt file -> 0
	_ = os.WriteFile(bindingOffsetPath(), []byte("not-a-number"), 0o644)
	if loadBindingOffset() != 0 {
		t.Fatal("corrupt offset should be 0")
	}
	_ = os.Remove(bindingOffsetPath())
	_ = filepath.Dir(bindingOffsetPath())

	// isTelegramBound / bindTelegramUser
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912'),(2,'李大華','0923')`); err != nil {
		t.Fatal(err)
	}
	if isTelegramBound("100") {
		t.Fatal("not bound yet")
	}
	if err := bindTelegramUser("", 1); err == nil {
		t.Fatal("empty chat id should fail")
	}
	if err := bindTelegramUser("100", 1); err != nil {
		t.Fatal(err)
	}
	if !isTelegramBound("100") {
		t.Fatal("should be bound")
	}
	if err := bindTelegramUser("100", 2); err == nil {
		t.Fatal("chat already bound should fail")
	}
	if err := bindTelegramUser("200", 1); err == nil {
		t.Fatal("customer already bound should fail")
	}
	if err := bindTelegramUser("300", 999); err == nil {
		t.Fatal("missing customer should fail")
	}
}

func TestBatchCWorkOrderNew(t *testing.T) {
	newMigratedTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (7,1,'ABC-1234','TOYOTA')`); err != nil {
		t.Fatal(err)
	}
	owner := model.User{ID: 1, Role: "owner"}
	rec := httptest.NewRecorder()
	NewWorkOrder(rec, httptest.NewRequest("GET", "/work-orders/new", nil), owner)
	if rec.Code != 200 {
		t.Fatalf("new code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	SearchWorkOrderVehiclesAPI(rec, httptest.NewRequest("GET", "/vehicles/search?q=ABC", nil))
	if rec.Code != 200 {
		t.Fatalf("search vehicles code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	QuickVehicle(rec, postReq(t, "/vehicles/quick", url.Values{"plate_number": {"NEW-1"}, "brand_model": {"HONDA"}, "customer_id": {"1"}}))
	_ = rec.Code
}

func TestBatchCInsurances(t *testing.T) {
	newMigratedTestDB(t)
	owner := model.User{ID: 1, Role: "owner"}
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (7,1,'ABC-1234','TOYOTA')`); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	ListVehicleInsurances(rec, httptest.NewRequest("GET", "/vehicles/7/insurances", nil), owner)
	if rec.Code != 200 {
		t.Fatalf("list code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	SearchInsurancesAPI(rec, httptest.NewRequest("GET", "/insurances/search?q=x", nil))
	if rec.Code != 200 {
		t.Fatalf("search code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	ExpiringInsurancesAPI(rec, httptest.NewRequest("GET", "/insurances/expiring", nil))
	if rec.Code != 200 {
		t.Fatalf("expiring code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	ListVehicleInspections(rec, httptest.NewRequest("GET", "/vehicles/7/inspections", nil), owner)
	if rec.Code != 200 {
		t.Fatalf("inspections code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	SearchInspectionsAPI(rec, httptest.NewRequest("GET", "/inspections/search?q=x", nil))
	if rec.Code != 200 {
		t.Fatalf("inspections search code=%d", rec.Code)
	}
}

func TestBatchCSubcontracts(t *testing.T) {
	newMigratedTestDB(t)
	owner := model.User{ID: 1, Role: "owner"}
	if !shouldCreatePayable(1, 100, "received") || shouldCreatePayable(0, 100, "received") || shouldCreatePayable(1, 0, "received") || shouldCreatePayable(1, 100, "cancelled") {
		t.Fatal("shouldCreatePayable")
	}
	if supplierIDArg("5") == nil || supplierIDArg("") != nil || supplierIDArg("abc") != nil {
		t.Fatal("supplierIDArg")
	}
	if subcontractStatusArg("x") == "" {
		t.Fatal("subcontractStatusArg")
	}
	if _, err := db.DB.Exec(`INSERT INTO suppliers (id, name) VALUES (1,'協力廠')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912')`); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	ListSubcontracts(rec, httptest.NewRequest("GET", "/subcontracts", nil), owner)
	if rec.Code != 200 {
		t.Fatalf("list code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	SearchSubcontractsAPI(rec, httptest.NewRequest("GET", "/subcontracts/search?q=x", nil))
	if rec.Code != 200 {
		t.Fatalf("search code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	CreateSubcontract(rec, postReq(t, "/subcontracts", url.Values{"supplier_id": {"1"}, "description": {"外包"}, "cost_amount": {"500"}}))
	_ = rec.Code
	var sid int64
	if err := db.DB.QueryRow(`SELECT id FROM subcontracts ORDER BY id DESC LIMIT 1`).Scan(&sid); err == nil && sid > 0 {
		req := httptest.NewRequest("GET", "/subcontracts/1/edit", nil)
		req.SetPathValue("id", "1")
		rec = httptest.NewRecorder()
		EditSubcontractForm(rec, req, owner)
		_ = rec.Code
		req = postReq(t, "/subcontracts/1/update", url.Values{"description": {"外包改"}, "cost_amount": {"600"}})
		req.SetPathValue("id", "1")
		rec = httptest.NewRecorder()
		UpdateSubcontract(rec, req)
		_ = rec.Code
		req = httptest.NewRequest("POST", "/subcontracts/1/delete", nil)
		req.SetPathValue("id", "1")
		rec = httptest.NewRecorder()
		DeleteSubcontract(rec, req)
		_ = rec.Code
	}
}

func TestBatchCLegacyCRUD(t *testing.T) {
	newMigratedTestDB(t)
	owner := model.User{ID: 1, Role: "owner"}
	if zeroToNil(0) != nil || zeroToNil(5) == nil {
		t.Fatal("zeroToNil")
	}
	if normDateOrEmpty("2026/01/02") == "" {
		t.Fatal("normDateOrEmpty")
	}
	if _, err := insuranceExpiryDate("2026-01-01"); err != nil {
		t.Fatal(err)
	}
	if _, err := insuranceExpiryDate("bad-date"); err == nil {
		t.Fatal("bad date should fail")
	}
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (7,1,'ABC-1234','TOYOTA')`); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	NewVehicleInsuranceForm(rec, httptest.NewRequest("GET", "/vehicles/7/insurances/new", nil), owner)
	if rec.Code != 200 {
		t.Fatalf("form code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	CreateVehicleInsurance(rec, postReq(t, "/vehicles/7/insurances", url.Values{"company": {"富邦"}, "policy_no": {"P1"}, "expiry_date": {"2027-01-01"}}))
	_ = rec.Code
	req := httptest.NewRequest("GET", "/vehicle-insurances/1/edit", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	EditVehicleInsuranceForm(rec, req, owner)
	_ = rec.Code
	req = postReq(t, "/vehicle-insurances/1/update", url.Values{"company": {"新光"}, "expiry_date": {"2028-01-01"}})
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	UpdateVehicleInsurance(rec, req)
	_ = rec.Code
	req = httptest.NewRequest("POST", "/vehicle-insurances/1/delete", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	DeleteVehicleInsurance(rec, req)
	_ = rec.Code

	// inspections
	rec = httptest.NewRecorder()
	NewVehicleInspectionForm(rec, httptest.NewRequest("GET", "/vehicles/7/inspections/new", nil), owner)
	_ = rec.Code
	rec = httptest.NewRecorder()
	CreateVehicleInspection(rec, postReq(t, "/vehicles/7/inspections", url.Values{"result": {"pass"}, "date": {"2026-01-01"}}))
	_ = rec.Code
	req = httptest.NewRequest("GET", "/vehicle-inspections/1/edit", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	EditVehicleInspectionForm(rec, req, owner)
	_ = rec.Code
	req = postReq(t, "/vehicle-inspections/1/update", url.Values{"result": {"fail"}})
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	UpdateVehicleInspection(rec, req)
	_ = rec.Code
	req = httptest.NewRequest("POST", "/vehicle-inspections/1/delete", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	DeleteVehicleInspection(rec, req)
	_ = rec.Code
	_ = strings.Contains("", "")
}

func TestBatchCStaff(t *testing.T) {
	newMigratedTestDB(t)
	owner := model.User{ID: 1, Role: "owner"}
	counter := model.User{ID: 2, Role: "counter"}
	// create validation
	rec := httptest.NewRecorder()
	CreateStaff(rec, postReq(t, "/staff", url.Values{}), owner)
	if rec.Code == 302 {
		t.Fatal("empty should fail")
	}
	// counter cannot create owner
	rec = httptest.NewRecorder()
	CreateStaff(rec, postReq(t, "/staff", url.Values{"username": {"u1"}, "password": {"p1"}, "name": {"N"}, "role": {"owner"}}), counter)
	if rec.Code == 302 {
		t.Fatal("counter creating owner should fail")
	}
	// owner creates technician
	rec = httptest.NewRecorder()
	CreateStaff(rec, postReq(t, "/staff", url.Values{"username": {"tech1"}, "password": {"pw12345"}, "name": {"技師"}, "role": {"technician"}, "staff_kind": {"employee"}}), owner)
	if rec.Code != 302 {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}
	// duplicate username
	rec = httptest.NewRecorder()
	CreateStaff(rec, postReq(t, "/staff", url.Values{"username": {"tech1"}, "password": {"pw12345"}, "name": {"技師"}, "role": {"technician"}}), owner)
	if rec.Code == 302 {
		t.Fatal("duplicate should fail")
	}
	var uid int64
	if err := db.DB.QueryRow(`SELECT id FROM users WHERE username='tech1'`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/staff/1/edit", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	EditStaffForm(rec, req, owner)
	_ = rec.Code
	req = postReq(t, "/staff/1/update", url.Values{"national_id": {"A123"}, "staff_kind": {"dependent"}})
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	UpdateStaff(rec, req)
	_ = rec.Code
	req = httptest.NewRequest("POST", "/staff/1/deactivate", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	DeactivateStaff(rec, req)
	_ = rec.Code
	req = httptest.NewRequest("POST", "/staff/1/reactivate", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	ReactivateStaff(rec, req)
	_ = rec.Code
}

func TestBatchCPrintDocs(t *testing.T) {
	newMigratedTestDB(t)
	owner := model.User{ID: 1, Role: "owner"}
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (7,1,'ABC-1234','TOYOTA')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO work_orders (id, customer_id, vehicle_id, status, total_amount, current_mileage) VALUES (10,1,7,'completed',1000,50000)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO receivables (id, voucher_no, customer_id, work_order_id, pay_total, cash) VALUES (5,'RCV-1',1,10,1000,1000)`); err != nil {
		t.Fatal(err)
	}
	// fill NOT NULL / scanned columns so scanWorkOrder succeeds
	if _, err := db.DB.Exec(`UPDATE work_orders SET approval_status='approved', created_at='2026-01-01' WHERE id=10`); err != nil {
		t.Logf("wo fill: %v", err)
	}
	if _, err := db.DB.Exec(`UPDATE receivables SET pay_date='2026-01-01', detail_total=1000, real_total=1000, discount=0, tax=0, card=0, check_amount=0, transfer_amount=0, voided=0 WHERE id=5`); err != nil {
		t.Fatal(err)
	}
	// helpers
	_ = metaPair("a", "b")
	_ = itemTypeLabelShort("part")
	_ = itemTypeLabelShort("labor")
	_ = itemTypeLabelShort("subcontract")
	_ = itemTypeLabelShort("other")
	if optNoteBlock("") != "" || optNoteBlock("x") == "" {
		t.Fatal("optNoteBlock")
	}
	t.Run("completion", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/print/work-orders/10/completion", nil)
		req.SetPathValue("id", "10")
		rec := httptest.NewRecorder()
		PrintWorkOrderCompletion(rec, req, owner)
		if rec.Code != 200 {
			t.Fatalf("code=%d", rec.Code)
		}
	})
	t.Run("receipt tax", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/print/receipts/5", nil)
		req.SetPathValue("id", "5")
		rec := httptest.NewRecorder()
		PrintReceiptWithTax(rec, req)
		if rec.Code != 200 {
			t.Fatalf("code=%d", rec.Code)
		}
	})
	t.Run("receipt 404", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/print/receipts/999", nil)
		req.SetPathValue("id", "999")
		rec := httptest.NewRecorder()
		PrintReceiptWithTax(rec, req)
		if rec.Code == 200 {
			t.Fatal("404 expected")
		}
	})
	t.Run("ar statement", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/print/customers/1/ar-statement", nil)
		req.SetPathValue("id", "1")
		rec := httptest.NewRecorder()
		PrintARStatement(rec, req, owner)
		if rec.Code != 200 {
			t.Fatalf("code=%d", rec.Code)
		}
	})
	t.Run("ap statement", func(t *testing.T) {
		if _, err := db.DB.Exec(`INSERT INTO suppliers (id, name, phone) VALUES (1,'供應商','02')`); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("GET", "/print/suppliers/1/ap-statement?month=2026-11", nil)
		req.SetPathValue("id", "1")
		rec := httptest.NewRecorder()
		PrintAPStatement(rec, req, owner)
		if rec.Code != 200 {
			t.Fatalf("code=%d", rec.Code)
		}
	})
	t.Run("daily report", func(t *testing.T) {
		rec := httptest.NewRecorder()
		PrintDailyReport(rec, httptest.NewRequest("GET", "/print/daily-report", nil), owner)
		if rec.Code != 200 {
			t.Fatalf("code=%d", rec.Code)
		}
	})
}
