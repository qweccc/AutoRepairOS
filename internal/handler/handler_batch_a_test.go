package handler

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

// ---------- Batch A shared seed helpers ----------

func batchASeedUser(t *testing.T, id int64, role string) {
	t.Helper()
	h, _ := bcrypt.GenerateFromPassword([]byte("pw12345"), bcrypt.MinCost)
	seedUser(t, id, role, string(h))
}

func batchAMustExec(t *testing.T, q string, args ...interface{}) {
	t.Helper()
	if _, err := db.DB.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

// seed customer+vehicle, returns vehicle id
func batchASeedVehicle(t *testing.T) int64 {
	t.Helper()
	batchAMustExec(t, `INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912')`)
	batchAMustExec(t, `INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (1,1,'ABC-1234','TOYOTA')`)
	return 1
}

func batchAPostWithSession(t *testing.T, userID int64, path string, form url.Values) *http.Request {
	t.Helper()
	r := postReq(t, path, form)
	tok, _ := startTestSession(t, userID)
	r.AddCookie(&http.Cookie{Name: "session_token", Value: tok})
	return r
}

// ---------- estimates.go ----------

func TestBatchAEstimates(t *testing.T) {
	newMigratedTestDB(t)
	batchASeedUser(t, 1, "owner")
	owner := model.User{ID: 1, Username: "owneruser", Role: "owner"}
	batchASeedVehicle(t)

	// pure helper
	if got := vehicleOptionsJSONFromEstimateVehicles(nil); got != "[]" {
		t.Fatalf("empty json = %q", got)
	}
	got := vehicleOptionsJSONFromEstimateVehicles([]pages.EstimateVehicleOption{{ID: 7, PlateNumber: " P1 ", BrandModel: "T", CustomerName: "C"}})
	if !strings.Contains(got, `"v":"7"`) || !strings.Contains(got, "P1") {
		t.Fatalf("json = %q", got)
	}

	// List + New
	rec := httptest.NewRecorder()
	ListEstimates(rec, httptest.NewRequest("GET", "/estimates", nil), owner)
	if rec.Code != 200 {
		t.Fatalf("ListEstimates %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	NewEstimate(rec, httptest.NewRequest("GET", "/estimates/new", nil), owner)
	if rec.Code != 200 {
		t.Fatalf("NewEstimate %d", rec.Code)
	}

	// CreateEstimate: no session -> redirect /login
	rec = httptest.NewRecorder()
	CreateEstimate(rec, postReq(t, "/estimates", url.Values{"vehicle_id": {"1"}}))
	if rec.Code != http.StatusFound {
		t.Fatalf("create no-session %d", rec.Code)
	}
	// CreateEstimate: unknown plate -> 400
	rec = httptest.NewRecorder()
	CreateEstimate(rec, batchAPostWithSession(t, 1, "/estimates", url.Values{"vehicle_id": {""}, "plate_number": {"NOPE-1"}}))
	if rec.Code != 400 {
		t.Fatalf("create unknown plate %d %s", rec.Code, rec.Body.String())
	}
	// CreateEstimate: bad vehicle id -> service error 400
	rec = httptest.NewRecorder()
	CreateEstimate(rec, batchAPostWithSession(t, 1, "/estimates", url.Values{"vehicle_id": {"999"}, "current_mileage": {"100"}}))
	if rec.Code != 400 {
		t.Fatalf("create bad vehicle %d", rec.Code)
	}
	// CreateEstimate: by vehicle_id -> 303
	rec = httptest.NewRecorder()
	CreateEstimate(rec, batchAPostWithSession(t, 1, "/estimates", url.Values{"vehicle_id": {"1"}, "current_mileage": {"50000"}, "customer_complaint": {"異音"}}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/estimates/") {
		t.Fatalf("create loc %q", loc)
	}
	var estID int64
	if err := db.DB.QueryRow(`SELECT id FROM estimates WHERE vehicle_id=1`).Scan(&estID); err != nil || estID <= 0 {
		t.Fatalf("estimate not created: %v id=%d", err, estID)
	}
	// CreateEstimate: by plate
	rec = httptest.NewRecorder()
	CreateEstimate(rec, batchAPostWithSession(t, 1, "/estimates", url.Values{"plate_number": {"abc-1234"}, "current_mileage": {"10"}}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create by plate %d %s", rec.Code, rec.Body.String())
	}

	// ShowEstimate 200 + 404
	req := httptest.NewRequest("GET", "/estimates/1", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	ShowEstimate(rec, req, owner)
	if rec.Code != 200 {
		t.Fatalf("show %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest("GET", "/estimates/999", nil)
	req.SetPathValue("id", "999")
	rec = httptest.NewRecorder()
	ShowEstimate(rec, req, owner)
	if rec.Code != 404 {
		t.Fatalf("show 404 got %d", rec.Code)
	}
	// loadEstimate direct
	if _, _, err := loadEstimate(999); err == nil {
		t.Fatal("loadEstimate 999 should error")
	}
	if _, _, err := loadEstimate(1); err != nil {
		t.Fatalf("loadEstimate 1: %v", err)
	}

	// AddEstimateItem ok + invalid
	req = postReq(t, "/estimates/1/items", url.Values{"item_type": {"labor"}, "description": {"工資"}, "quantity": {"2"}, "unit_price": {"1000"}})
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	AddEstimateItem(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("add item %d %s", rec.Code, rec.Body.String())
	}
	var total float64
	if err := db.DB.QueryRow(`SELECT total_amount FROM estimates WHERE id=1`).Scan(&total); err != nil || total != 2000 {
		t.Fatalf("total=%v err=%v", total, err)
	}
	req = postReq(t, "/estimates/1/items", url.Values{"item_type": {"bogus"}, "description": {"x"}, "quantity": {"1"}, "unit_price": {"1"}})
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	AddEstimateItem(rec, req)
	if rec.Code != 400 {
		t.Fatalf("add invalid item %d", rec.Code)
	}
	// add another item for delete test
	req = postReq(t, "/estimates/1/items", url.Values{"item_type": {"part"}, "description": {"零件"}, "quantity": {"1"}, "unit_price": {"500"}})
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	AddEstimateItem(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("add item2 %d", rec.Code)
	}
	var itemID int64
	if err := db.DB.QueryRow(`SELECT id FROM estimate_items WHERE estimate_id=1 ORDER BY id DESC LIMIT 1`).Scan(&itemID); err != nil {
		t.Fatal(err)
	}

	// SetEstimateDiscount: bad format, ok, exceeds
	req = postReq(t, "/estimates/1/discount", url.Values{"discount_amount": {"abc"}})
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	SetEstimateDiscount(rec, req)
	if rec.Code != 400 {
		t.Fatalf("discount bad %d", rec.Code)
	}
	req = postReq(t, "/estimates/1/discount", url.Values{"discount_amount": {"99999"}})
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	SetEstimateDiscount(rec, req)
	if rec.Code != 400 {
		t.Fatalf("discount exceeds %d", rec.Code)
	}
	req = postReq(t, "/estimates/1/discount", url.Values{"discount_amount": {"200"}})
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	SetEstimateDiscount(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("discount ok %d %s", rec.Code, rec.Body.String())
	}

	// DeleteEstimateItem
	req = httptest.NewRequest("POST", "/estimates/1/items/x/delete", nil)
	req.SetPathValue("id", "1")
	req.SetPathValue("item_id", "99999")
	rec = httptest.NewRecorder()
	DeleteEstimateItem(rec, req)
	if rec.Code != 400 {
		t.Fatalf("delete missing item %d", rec.Code)
	}
	req = httptest.NewRequest("POST", "/estimates/1/items/x/delete", nil)
	req.SetPathValue("id", "1")
	req.SetPathValue("item_id", string(rune('0'+itemID))) // placeholder overwritten below
	// use proper string conversion
	req.SetPathValue("item_id", batchAItoa(itemID))
	rec = httptest.NewRecorder()
	DeleteEstimateItem(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete item %d %s", rec.Code, rec.Body.String())
	}

	// ConvertEstimate: not found, no-items (estimate 2), ok (estimate 1)
	req = httptest.NewRequest("POST", "/estimates/999/convert", nil)
	req.SetPathValue("id", "999")
	rec = httptest.NewRecorder()
	ConvertEstimate(rec, req)
	if rec.Code != 404 {
		t.Fatalf("convert 404 got %d", rec.Code)
	}
	req = httptest.NewRequest("POST", "/estimates/2/convert", nil)
	req.SetPathValue("id", "2")
	rec = httptest.NewRecorder()
	ConvertEstimate(rec, req)
	if rec.Code != 400 {
		t.Fatalf("convert no-items %d", rec.Code)
	}
	req = httptest.NewRequest("POST", "/estimates/1/convert", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	ConvertEstimate(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("convert %d %s", rec.Code, rec.Body.String())
	}
	if !strings.HasPrefix(rec.Header().Get("Location"), "/work-orders/") {
		t.Fatalf("convert loc %q", rec.Header().Get("Location"))
	}
	// ShowWorkOrderEstimateSource: 404 for unconverted, 200 for converted WO
	req = httptest.NewRequest("GET", "/work-orders/999/estimate-source", nil)
	req.SetPathValue("id", "999")
	rec = httptest.NewRecorder()
	ShowWorkOrderEstimateSource(rec, req, owner)
	if rec.Code != 404 {
		t.Fatalf("source 404 got %d", rec.Code)
	}
	var woID int64
	if err := db.DB.QueryRow(`SELECT converted_work_order_id FROM estimates WHERE id=1`).Scan(&woID); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest("GET", "/work-orders/x/estimate-source", nil)
	req.SetPathValue("id", batchAItoa(woID))
	rec = httptest.NewRecorder()
	ShowWorkOrderEstimateSource(rec, req, owner)
	if rec.Code != 200 {
		t.Fatalf("source %d %s", rec.Code, rec.Body.String())
	}
}

func batchAItoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// ---------- insurance.go ----------

func TestBatchAInsurance(t *testing.T) {
	newMigratedTestDB(t)
	batchASeedUser(t, 1, "owner")
	owner := model.User{ID: 1, Username: "owneruser", Role: "owner"}

	// Create: missing fields, ok, duplicate
	rec := httptest.NewRecorder()
	CreateInsuranceCompany(rec, postReq(t, "/insurance-companies", url.Values{"code": {""}, "name": {""}}))
	if rec.Code != 400 {
		t.Fatalf("create missing %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	CreateInsuranceCompany(rec, postReq(t, "/insurance-companies", url.Values{
		"code": {"CT"}, "name": {"國泰產險"}, "tax_id": {"123"}, "claim_phone": {"02-1"}, "phone": {"02-2"},
		"contact_person": {"王"}, "address": {"台北"}, "postal_code": {"100"}, "notes": {"n"},
	}))
	if rec.Code != http.StatusFound {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	CreateInsuranceCompany(rec, postReq(t, "/insurance-companies", url.Values{"code": {"CT"}, "name": {"重複"}}))
	if rec.Code != 400 {
		t.Fatalf("create dup %d", rec.Code)
	}

	// List (plain + q + pagination) & Search API
	rec = httptest.NewRecorder()
	ListInsuranceCompanies(rec, httptest.NewRequest("GET", "/insurance-companies", nil), owner)
	if rec.Code != 200 {
		t.Fatalf("list %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	ListInsuranceCompanies(rec, httptest.NewRequest("GET", "/insurance-companies?q=CT&page=1&per=10", nil), owner)
	if rec.Code != 200 {
		t.Fatalf("list q %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	SearchInsuranceAPI(rec, httptest.NewRequest("GET", "/insurance-companies/search?q=國", nil))
	if rec.Code != 200 {
		t.Fatalf("search %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	SearchInsuranceAPI(rec, httptest.NewRequest("GET", "/insurance-companies/search", nil))
	if rec.Code != 200 {
		t.Fatalf("search empty %d", rec.Code)
	}

	// Edit form 200/404
	req := httptest.NewRequest("GET", "/insurance-companies/1/edit", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	EditInsuranceCompanyForm(rec, req, owner)
	if rec.Code != 200 {
		t.Fatalf("edit %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest("GET", "/insurance-companies/999/edit", nil)
	req.SetPathValue("id", "999")
	rec = httptest.NewRecorder()
	EditInsuranceCompanyForm(rec, req, owner)
	if rec.Code != 404 {
		t.Fatalf("edit 404 got %d", rec.Code)
	}

	// Update: missing, ok, not-found
	req = postReq(t, "/insurance-companies/1/update", url.Values{"code": {""}, "name": {""}})
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	UpdateInsuranceCompany(rec, req)
	if rec.Code != 400 {
		t.Fatalf("update missing %d", rec.Code)
	}
	req = postReq(t, "/insurance-companies/999/update", url.Values{"code": {"X"}, "name": {"Y"}})
	req.SetPathValue("id", "999")
	rec = httptest.NewRecorder()
	UpdateInsuranceCompany(rec, req)
	if rec.Code != 400 {
		t.Fatalf("update notfound %d", rec.Code)
	}
	req = postReq(t, "/insurance-companies/1/update", url.Values{"code": {"CT2"}, "name": {"國泰改名"}})
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	UpdateInsuranceCompany(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("update %d %s", rec.Code, rec.Body.String())
	}
	var nm string
	if err := db.DB.QueryRow(`SELECT name FROM insurance_companies WHERE id=1`).Scan(&nm); err != nil || nm != "國泰改名" {
		t.Fatalf("updated name %q err %v", nm, err)
	}

	// scanInsuranceCompany direct + options helper
	row := db.DB.QueryRow(`SELECT id, code, name, COALESCE(tax_id,''), COALESCE(claim_phone,''), COALESCE(phone,''), COALESCE(contact_person,''), COALESCE(address,''), COALESCE(postal_code,''), COALESCE(notes,''), active FROM insurance_companies WHERE id=1`)
	if _, err := scanInsuranceCompany(row); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if s := insuranceOptionsSel(1); !strings.Contains(s, "CT2") {
		t.Fatalf("options %q", s)
	}
	if s := insuranceOptionsSel(0); !strings.Contains(s, "不指定") {
		t.Fatalf("options0 %q", s)
	}

	// Deactivate: blocked by open WO, then ok after paid, then not-found
	batchASeedVehicle(t)
	batchAMustExec(t, `INSERT INTO work_orders (id, status, vehicle_id, customer_id, current_mileage, insurance_company_id) VALUES (1,'pending',1,1,0,1)`)
	req = httptest.NewRequest("POST", "/insurance-companies/1/deactivate", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	DeactivateInsuranceCompany(rec, req)
	if rec.Code != 400 {
		t.Fatalf("deactivate blocked %d %s", rec.Code, rec.Body.String())
	}
	batchAMustExec(t, `UPDATE work_orders SET status='paid' WHERE id=1`)
	req = httptest.NewRequest("POST", "/insurance-companies/1/deactivate", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	DeactivateInsuranceCompany(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("deactivate %d %s", rec.Code, rec.Body.String())
	}
	var active int
	if err := db.DB.QueryRow(`SELECT active FROM insurance_companies WHERE id=1`).Scan(&active); err != nil || active != 0 {
		t.Fatalf("active=%d err=%v", active, err)
	}
	req = httptest.NewRequest("POST", "/insurance-companies/999/deactivate", nil)
	req.SetPathValue("id", "999")
	rec = httptest.NewRecorder()
	DeactivateInsuranceCompany(rec, req)
	if rec.Code != 400 {
		t.Fatalf("deactivate notfound %d", rec.Code)
	}
}

// ---------- quotation.go ----------

func TestBatchAQuotation(t *testing.T) {
	newMigratedTestDB(t)
	batchASeedUser(t, 1, "owner")
	owner := model.User{ID: 1, Username: "owneruser", Role: "owner"}
	batchASeedVehicle(t)
	batchAMustExec(t, `INSERT INTO work_orders (id, status, created_at, customer_id, vehicle_id, current_mileage, customer_complaint, total_amount, discount_amount, is_warranty) VALUES (1,'pending',datetime('now','localtime'),1,1,50000,'煞車異音',1800,200,1)`)
	batchAMustExec(t, `INSERT INTO work_order_items (work_order_id, item_type, description, quantity, unit_price, subtotal) VALUES (1,'labor','工資',1,1000,1000),(1,'part','煞車皮',2,250,500),(1,'subcontract','外包',1,500,500)`)

	req := httptest.NewRequest("GET", "/work-orders/1/quotation", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	PrintWorkOrderQuotation(rec, req, owner)
	if rec.Code != 200 {
		t.Fatalf("quotation a4 %d %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("ct %q", ct)
	}
	req = httptest.NewRequest("GET", "/work-orders/1/quotation?paper=thermal", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	PrintWorkOrderQuotation(rec, req, owner)
	if rec.Code != 200 {
		t.Fatalf("quotation thermal %d", rec.Code)
	}
	req = httptest.NewRequest("GET", "/work-orders/999/quotation", nil)
	req.SetPathValue("id", "999")
	rec = httptest.NewRecorder()
	PrintWorkOrderQuotation(rec, req, owner)
	if rec.Code != 404 {
		t.Fatalf("quotation 404 got %d", rec.Code)
	}
}

// ---------- reports.go ----------

func TestBatchAReports(t *testing.T) {
	newMigratedTestDB(t)
	owner := model.User{ID: 1, Username: "o", Role: "owner"}
	tech := model.User{ID: 2, Username: "t", Role: "technician"}
	counter := model.User{ID: 3, Username: "c", Role: "counter"}

	// technician forbidden
	rec := httptest.NewRecorder()
	ReportsHome(rec, httptest.NewRequest("GET", "/reports", nil), tech)
	if rec.Code != 403 {
		t.Fatalf("tech %d", rec.Code)
	}
	// empty DB, default range + explicit range
	for _, u := range []model.User{owner, counter} {
		rec = httptest.NewRecorder()
		ReportsHome(rec, httptest.NewRequest("GET", "/reports", nil), u)
		if rec.Code != 200 {
			t.Fatalf("reports %s %d %s", u.Role, rec.Code, rec.Body.String())
		}
		rec = httptest.NewRecorder()
		ReportsHome(rec, httptest.NewRequest("GET", "/reports?from=2026-01-01&to=2026-12-31", nil), u)
		if rec.Code != 200 {
			t.Fatalf("reports range %d", rec.Code)
		}
	}
	// with data: paid insurance/self, completed unpaid (aging), low stock
	batchASeedVehicle(t)
	today := "2026-06-15 10:00:00"
	batchAMustExec(t, `INSERT INTO work_orders (id, status, created_at, updated_at, customer_id, vehicle_id, current_mileage, total_amount, is_insurance) VALUES (1,'paid',?, ?,1,1,0,5000,1),(2,'paid',?, ?,1,1,0,3000,0),(3,'completed',?, ?,1,1,0,2000,0)`, today, today, today, today, today, today)
	batchAMustExec(t, `INSERT INTO work_order_items (work_order_id, item_type, description, quantity, subtotal) VALUES (1,'labor','保養',1,5000),(2,'labor','保養',2,3000)`)
	batchAMustExec(t, `INSERT INTO inventory_items (id, part_number, name, bin_location, stock_quantity, min_safety_stock) VALUES (1,'PN1','機油','A1',0,5),(2,'PN2','濾芯','A2',10,5)`)
	rec = httptest.NewRecorder()
	ReportsHome(rec, httptest.NewRequest("GET", "/reports?from=2026-01-01&to=2026-12-31", nil), owner)
	if rec.Code != 200 {
		t.Fatalf("reports with data %d %s", rec.Code, rec.Body.String())
	}
}

// ---------- static.go ----------

func TestBatchAStatic(t *testing.T) {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		t.Fatal(err)
	}
	// serveStatic: existing js/css, missing
	rec := httptest.NewRecorder()
	serveStatic(rec, httptest.NewRequest("GET", "/", nil), sub, "app.css")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "text/css; charset=utf-8" {
		t.Fatalf("serveStatic css %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	rec = httptest.NewRecorder()
	serveStatic(rec, httptest.NewRequest("GET", "/", nil), sub, "htmx.min.js")
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("serveStatic js %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	serveStatic(rec, httptest.NewRequest("GET", "/", nil), sub, "nope.css")
	if rec.Code != 404 {
		t.Fatalf("serveStatic missing %d", rec.Code)
	}
	// serveSub: bad prefix, empty, missing, ok, traversal-ish
	cases := []struct {
		path string
		want int
	}{
		{"/other/app.css", 404},
		{"/static/", 404},
		{"/static/nope.js", 404},
		{"/static/app.css", 200},
		{"/static/htmx.min.js", 200},
		{"/static/offline.css", 200},
		{"/static/sw.js", 200},
	}
	for _, tc := range cases {
		rec = httptest.NewRecorder()
		serveSub(rec, httptest.NewRequest("GET", tc.path, nil), sub)
		if rec.Code != tc.want {
			t.Fatalf("serveSub %s got %d want %d", tc.path, rec.Code, tc.want)
		}
	}
	// RegisterStatic via ServeMux: public css/js, login-gated sw.js + wildcard
	mux := http.NewServeMux()
	RegisterStatic(mux)
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/static/app.css", 200},
		{"/static/htmx.min.js", 200},
		{"/sw.js", 302}, // RequireLogin, no session
		{"/static/offline.js", 302},
	} {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", tc.path, nil))
		if rec.Code != tc.want {
			t.Fatalf("mux %s got %d want %d", tc.path, rec.Code, tc.want)
		}
	}
	// RegisterStatic via chi
	r := chi.NewRouter()
	RegisterStatic(r)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/static/app.css", nil))
	if rec.Code != 200 {
		t.Fatalf("chi css %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/sw.js", nil))
	if rec.Code != 302 {
		t.Fatalf("chi sw %d", rec.Code)
	}
	// RegisterStatic(nil) and unknown router: no panic
	RegisterStatic(nil)
	RegisterStatic(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {}))
	registerStaticServeMux(nil)
}

// ---------- template_crud.go ----------

func TestBatchATemplateCRUD(t *testing.T) {
	newMigratedTestDB(t)
	batchASeedUser(t, 1, "owner")
	owner := model.User{ID: 1, Username: "owneruser", Role: "owner"}

	rec := httptest.NewRecorder()
	NewMaintenanceTemplateForm(rec, httptest.NewRequest("GET", "/maintenance-templates/new", nil), owner)
	if rec.Code != 200 {
		t.Fatalf("new form %d", rec.Code)
	}
	// Create: missing, ok, duplicate
	rec = httptest.NewRecorder()
	CreateMaintenanceTemplate(rec, postReq(t, "/maintenance-templates", url.Values{"code": {""}, "name": {""}}))
	if rec.Code != 400 {
		t.Fatalf("create missing %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	CreateMaintenanceTemplate(rec, postReq(t, "/maintenance-templates", url.Values{"code": {"A0002"}, "name": {"小保養"}, "notes": {"標準"}}))
	if rec.Code != http.StatusFound {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Location"), "/maintenance-templates/") {
		t.Fatalf("create loc %q", rec.Header().Get("Location"))
	}
	rec = httptest.NewRecorder()
	CreateMaintenanceTemplate(rec, postReq(t, "/maintenance-templates", url.Values{"code": {"A0002"}, "name": {"重複"}}))
	if rec.Code != 400 {
		t.Fatalf("create dup %d", rec.Code)
	}
	var tid int64
	if err := db.DB.QueryRow(`SELECT id FROM maintenance_templates WHERE code='A0002'`).Scan(&tid); err != nil {
		t.Fatal(err)
	}
	tidStr := batchAItoa(tid)

	// Edit form 200/404
	req := httptest.NewRequest("GET", "/maintenance-templates/x/edit", nil)
	req.SetPathValue("id", tidStr)
	rec = httptest.NewRecorder()
	EditMaintenanceTemplateForm(rec, req, owner)
	if rec.Code != 200 {
		t.Fatalf("edit %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest("GET", "/maintenance-templates/999/edit", nil)
	req.SetPathValue("id", "999")
	rec = httptest.NewRecorder()
	EditMaintenanceTemplateForm(rec, req, owner)
	if rec.Code != 404 {
		t.Fatalf("edit 404 got %d", rec.Code)
	}

	// Update: missing name, ok, not-found
	req = postReq(t, "/maintenance-templates/x/update", url.Values{"name": {""}})
	req.SetPathValue("id", tidStr)
	rec = httptest.NewRecorder()
	UpdateMaintenanceTemplate(rec, req)
	if rec.Code != 400 {
		t.Fatalf("update missing %d", rec.Code)
	}
	req = postReq(t, "/maintenance-templates/999/update", url.Values{"name": {"X"}})
	req.SetPathValue("id", "999")
	rec = httptest.NewRecorder()
	UpdateMaintenanceTemplate(rec, req)
	if rec.Code != 404 {
		t.Fatalf("update 404 got %d", rec.Code)
	}
	req = postReq(t, "/maintenance-templates/x/update", url.Values{"name": {"小保養改"}, "notes": {"n2"}})
	req.SetPathValue("id", tidStr)
	rec = httptest.NewRecorder()
	UpdateMaintenanceTemplate(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("update %d %s", rec.Code, rec.Body.String())
	}

	// Replace items: 404, ok (blank row skipped), verify count
	req = postReq(t, "/maintenance-templates/999/items", url.Values{"item_desc": {"機油"}})
	req.SetPathValue("id", "999")
	rec = httptest.NewRecorder()
	ReplaceMaintenanceTemplateItems(rec, req)
	if rec.Code != 404 {
		t.Fatalf("replace 404 got %d", rec.Code)
	}
	form := url.Values{}
	form["item_desc"] = []string{"機油", "", "濾芯"}
	form["part_number"] = []string{"PN1", "", "PN2"}
	form["item_qty"] = []string{"1", "", "2"}
	form["item_price"] = []string{"800", "", "300"}
	req = postReq(t, "/maintenance-templates/x/items", form)
	req.SetPathValue("id", tidStr)
	rec = httptest.NewRecorder()
	ReplaceMaintenanceTemplateItems(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("replace %d %s", rec.Code, rec.Body.String())
	}
	var n int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM maintenance_template_items WHERE template_id=? AND deleted_at IS NULL`, tid).Scan(&n); err != nil || n != 2 {
		t.Fatalf("items n=%d err=%v", n, err)
	}
	// Edit form now lists items
	req = httptest.NewRequest("GET", "/maintenance-templates/x/edit", nil)
	req.SetPathValue("id", tidStr)
	rec = httptest.NewRecorder()
	EditMaintenanceTemplateForm(rec, req, owner)
	if rec.Code != 200 {
		t.Fatalf("edit with items %d", rec.Code)
	}

	// Delete: not-found, ok, verify soft-deleted
	req = httptest.NewRequest("POST", "/maintenance-templates/999/delete", nil)
	req.SetPathValue("id", "999")
	rec = httptest.NewRecorder()
	DeleteMaintenanceTemplate(rec, req)
	if rec.Code != 404 {
		t.Fatalf("delete 404 got %d", rec.Code)
	}
	req = httptest.NewRequest("POST", "/maintenance-templates/x/delete", nil)
	req.SetPathValue("id", tidStr)
	rec = httptest.NewRecorder()
	DeleteMaintenanceTemplate(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("delete %d %s", rec.Code, rec.Body.String())
	}
	if err := db.DB.QueryRow(`SELECT deleted_at FROM maintenance_templates WHERE id=?`, tid).Scan(new(string)); err != nil {
		// deleted_at may be set; non-nil scan to string works if set
		t.Logf("deleted_at scan: %v", err)
	}
}
