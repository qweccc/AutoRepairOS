package handler

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

func setupInsurancePolicyTestDB(t *testing.T) {
	t.Helper()
	d, err := openTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	db.DB = d
	stmts := []string{
		`CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT, deleted_at TEXT)`,
		`CREATE VIEW active_customers AS SELECT * FROM customers WHERE deleted_at IS NULL`,
		`CREATE TABLE vehicles (id INTEGER PRIMARY KEY, customer_id INTEGER, plate_number TEXT, deleted_at TEXT)`,
		`CREATE VIEW active_vehicles AS SELECT * FROM vehicles WHERE deleted_at IS NULL`,
		`CREATE TABLE insurance_companies (id INTEGER PRIMARY KEY, name TEXT, active INTEGER DEFAULT 1)`,
		`CREATE TABLE vehicle_insurances (
			id INTEGER PRIMARY KEY AUTOINCREMENT, vehicle_id INTEGER NOT NULL, insurance_company_id INTEGER,
			company_no TEXT DEFAULT '', insurance_class TEXT DEFAULT '', insurance_class_name TEXT DEFAULT '',
			license_date TEXT DEFAULT '', card_no TEXT DEFAULT '', amount_raw TEXT DEFAULT '', notes TEXT DEFAULT '',
			policy_number TEXT NOT NULL DEFAULT '', expiry_date TEXT NOT NULL DEFAULT '', deleted_at TEXT)`,
		`CREATE VIEW active_vehicle_insurances AS SELECT * FROM vehicle_insurances WHERE deleted_at IS NULL`,
		`INSERT INTO customers (id,name) VALUES (1,'王小明')`,
		`INSERT INTO vehicles (id,customer_id,plate_number) VALUES (1,1,'ABC-1234')`,
	}
	for _, stmt := range stmts {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatalf("test schema setup failed: %v", err)
		}
	}
}

func TestVehicleInsurancePolicyCRUDAndExpiringList(t *testing.T) {
	setupInsurancePolicyTestDB(t)

	form := url.Values{
		"vehicle_id":    {"1"},
		"policy_number": {"POLICY-100"},
		"expiry_date":   {"2099-02-03"},
	}
	req := httptest.NewRequest("POST", "/insurances", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	CreateVehicleInsurance(rec, req)
	if rec.Code != 302 {
		t.Fatalf("create status = %d, want 302; body=%s", rec.Code, rec.Body.String())
	}
	if _, err := db.DB.Exec(`UPDATE vehicle_insurances SET expiry_date=date('now','localtime','+12 days') WHERE id=1`); err != nil {
		t.Fatal(err)
	}

	update := url.Values{"policy_number": {"POLICY-200"}, "expiry_date": {"2099-04-05"}}
	req = httptest.NewRequest("POST", "/insurances/1", strings.NewReader(update.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	UpdateVehicleInsurance(rec, req)
	if rec.Code != 302 {
		t.Fatalf("update status = %d, want 302; body=%s", rec.Code, rec.Body.String())
	}
	var policy, expiry string
	if err := db.DB.QueryRow("SELECT policy_number, expiry_date FROM vehicle_insurances WHERE id=1").Scan(&policy, &expiry); err != nil {
		t.Fatal(err)
	}
	if policy != "POLICY-200" || expiry != "2099-04-05" {
		t.Fatalf("stored policy fields = (%q, %q), want updated fields", policy, expiry)
	}
	editReq := httptest.NewRequest("GET", "/insurances/1/edit", nil)
	editReq.SetPathValue("id", "1")
	editRec := httptest.NewRecorder()
	EditVehicleInsuranceForm(editRec, editReq, model.User{Username: "tester", Role: "owner"})
	if editRec.Code != 200 || !strings.Contains(editRec.Body.String(), `value="POLICY-200"`) ||
		!strings.Contains(editRec.Body.String(), `value="2099-04-05"`) {
		t.Fatalf("edit form did not render the new policy values: status=%d", editRec.Code)
	}
	searchReq := httptest.NewRequest("GET", "/api/insurances/search?q=POLICY-200", nil)
	searchRec := httptest.NewRecorder()
	SearchInsurancesAPI(searchRec, searchReq)
	if searchRec.Code != 200 || !strings.Contains(searchRec.Body.String(), "POLICY-200") {
		t.Fatalf("policy search did not return policy number: status=%d body=%s", searchRec.Code, searchRec.Body.String())
	}

	if _, err := db.DB.Exec(`UPDATE vehicle_insurances SET expiry_date=date('now','localtime','+12 days') WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest("GET", "/api/insurances/expiring", nil)
	rec = httptest.NewRecorder()
	ExpiringInsurancesAPI(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expiring list status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var entries []struct {
		PolicyNumber string `json:"policy_number"`
		ExpiryDate   string `json:"expiry_date"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].PolicyNumber != "POLICY-200" || entries[0].ExpiryDate == "" {
		t.Fatalf("expiring policies = %+v, want policy and expiry date", entries)
	}
}

func TestUpdateWorkOrderClaimFields(t *testing.T) {
	d, err := openTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	db.DB = d
	if _, err := d.Exec(`CREATE TABLE work_orders (
		id INTEGER PRIMARY KEY, status TEXT, claim_amount REAL NOT NULL DEFAULT 0,
		claim_date TEXT NOT NULL DEFAULT '', claim_note TEXT NOT NULL DEFAULT '',
		updated_at TEXT DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO work_orders (id,status) VALUES (1,'pending')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE VIEW active_work_orders AS SELECT * FROM work_orders`); err != nil {
		t.Fatal(err)
	}

	form := url.Values{"claim_date": {"2026-10-01"}, "claim_amount": {"12345.67"}, "claim_note": {"核定金額"}}
	req := httptest.NewRequest("POST", "/work-orders/1/claim", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	UpdateWorkOrderClaim(rec, req)
	if rec.Code != 302 {
		t.Fatalf("claim update status = %d, want 302; body=%s", rec.Code, rec.Body.String())
	}
	var claimDate, claimNote string
	var claimAmount float64
	if err := d.QueryRow("SELECT claim_date, claim_amount, claim_note FROM work_orders WHERE id=1").
		Scan(&claimDate, &claimAmount, &claimNote); err != nil {
		t.Fatal(err)
	}
	if claimDate != "2026-10-01" || claimAmount != 12345.67 || claimNote != "核定金額" {
		t.Fatalf("stored claim fields = (%q, %v, %q)", claimDate, claimAmount, claimNote)
	}

	clear := url.Values{"claim_amount": {""}}
	req = httptest.NewRequest("POST", "/work-orders/1/claim", strings.NewReader(clear.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	UpdateWorkOrderClaim(rec, req)
	if rec.Code != 302 {
		t.Fatalf("clearing claim fields status = %d, want 302; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateWorkOrderWithClaimFields(t *testing.T) {
	d, err := openTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	db.DB = d
	stmts := []string{
		`CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT, deleted_at TEXT)`,
		`CREATE VIEW active_customers AS SELECT * FROM customers WHERE deleted_at IS NULL`,
		`CREATE TABLE vehicles (id INTEGER PRIMARY KEY, customer_id INTEGER, deleted_at TEXT)`,
		`CREATE VIEW active_vehicles AS SELECT * FROM vehicles WHERE deleted_at IS NULL`,
		`CREATE TABLE insurance_companies (id INTEGER PRIMARY KEY, active INTEGER DEFAULT 1)`,
		`CREATE TABLE work_orders (
			id INTEGER PRIMARY KEY AUTOINCREMENT, vehicle_id INTEGER, customer_id INTEGER, technician_id INTEGER,
			status TEXT, current_mileage INTEGER, customer_complaint TEXT, approval_status TEXT,
			total_amount REAL DEFAULT 0, sop_completed INTEGER DEFAULT 0, insurance_company_id INTEGER,
			claim_no TEXT, is_insurance INTEGER DEFAULT 0, accident_date TEXT, deductible_amount REAL DEFAULT 0,
			claim_amount REAL NOT NULL DEFAULT 0, claim_date TEXT NOT NULL DEFAULT '', claim_note TEXT NOT NULL DEFAULT '')`,
		`INSERT INTO customers (id,name) VALUES (1,'王小明')`,
		`INSERT INTO vehicles (id,customer_id) VALUES (1,1)`,
	}
	for _, stmt := range stmts {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatalf("test schema setup failed: %v", err)
		}
	}

	form := url.Values{
		"vehicle_id": {"1"}, "current_mileage": {"12000"},
		"claim_date": {"2026-10-02"}, "claim_amount": {"25000"}, "claim_note": {"修理費用核定"},
	}
	req := httptest.NewRequest("POST", "/work-orders", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	CreateWorkOrder(rec, req)
	if rec.Code != 302 {
		t.Fatalf("work order create status = %d, want 302; body=%s", rec.Code, rec.Body.String())
	}
	var claimDate, claimNote string
	var claimAmount float64
	if err := d.QueryRow("SELECT claim_date, claim_amount, claim_note FROM work_orders WHERE id=1").
		Scan(&claimDate, &claimAmount, &claimNote); err != nil {
		t.Fatal(err)
	}
	if claimDate != "2026-10-02" || claimAmount != 25000 || claimNote != "修理費用核定" {
		t.Fatalf("created claim fields = (%q, %v, %q)", claimDate, claimAmount, claimNote)
	}
}
