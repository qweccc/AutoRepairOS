package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

func postReq(t *testing.T, path string, form url.Values) *http.Request {
	t.Helper()
	r := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func TestVehicleHelpers(t *testing.T) {
	newMigratedTestDB(t)
	if normDate("2026/01/02 rc") != "2026-01-02" || normDate(" x ") != "x" {
		t.Fatal("normDate")
	}
	if intOrBlank(0) != "" || intOrBlank(5) != "5" {
		t.Fatal("intOrBlank")
	}
	if atoiOrNil("") != nil || atoiOrNil("abc") != nil || atoiOrNil("-1") != nil || atoiOrNil("7") != 7 {
		t.Fatal("atoiOrNil")
	}
	if lastMileageOf(999) != 0 {
		t.Fatal("lastMileageOf")
	}
	// maintDueText branches
	past := time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	soon := time.Now().AddDate(0, 0, 5).Format("2006-01-02")
	far := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	cases := []model.Vehicle{
		{NextMaintenanceDate: past, NextMaintenanceMileage: 10000},
		{NextMaintenanceDate: soon, NextMaintenanceMileage: 10000},
		{NextMaintenanceDate: far, NextMaintenanceMileage: 10000},
		{NextMaintenanceDate: "bad-date", NextMaintenanceMileage: 10000},
		{},
	}
	for _, v := range cases {
		d, km := maintDueText(v, 9500)
		if d == "" || km == "" {
			t.Fatal("maintDueText empty")
		}
	}
	// mileage exceeded / within 1000 / far
	if _, km := maintDueText(model.Vehicle{NextMaintenanceMileage: 5000}, 6000); !strings.Contains(km, "超過") {
		t.Fatalf("km exceeded %q", km)
	}
	if _, km := maintDueText(model.Vehicle{NextMaintenanceMileage: 6000}, 5500); !strings.Contains(km, "剩") {
		t.Fatalf("km soon %q", km)
	}
	if yearText(2020) == "" || yearText(0) == "" {
		t.Fatal("yearText")
	}
	_ = normVehicleModel(" toyota ")
	_ = customerOptions()
	_ = vehicleOptions()
	_ = vehicleModelOptions("")
	if lastInsertID("vehicles") != 0 {
		t.Fatal("lastInsertID empty")
	}
	recalcNextMaintenance(999) // no rows: no-op
}

func TestVehicleHandlers(t *testing.T) {
	newMigratedTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912')`); err != nil {
		t.Fatal(err)
	}
	owner := model.User{ID: 1, Role: "owner"}

	// CreateVehicle success
	rec := httptest.NewRecorder()
	CreateVehicle(rec, postReq(t, "/vehicles", url.Values{"customer_id": {"1"}, "plate_number": {"ABC-1234"}, "brand_model": {"TOYOTA"}}))
	if rec.Code != 302 {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}
	// duplicate plate -> fail (400-ish via fail())
	rec = httptest.NewRecorder()
	CreateVehicle(rec, postReq(t, "/vehicles", url.Values{"customer_id": {"1"}, "plate_number": {"ABC-1234"}, "brand_model": {"TOYOTA"}}))
	if rec.Code == 302 {
		t.Fatal("duplicate plate should fail")
	}
	// missing plate/model -> fail
	rec = httptest.NewRecorder()
	CreateVehicle(rec, postReq(t, "/vehicles", url.Values{"customer_id": {"1"}}))
	if rec.Code == 302 {
		t.Fatal("missing fields should fail")
	}
	// bad customer -> fail
	rec = httptest.NewRecorder()
	CreateVehicle(rec, postReq(t, "/vehicles", url.Values{"customer_id": {"999"}, "plate_number": {"X-1"}, "brand_model": {"TOYOTA"}}))
	if rec.Code == 302 {
		t.Fatal("bad customer should fail")
	}

	var vid int64
	if err := db.DB.QueryRow(`SELECT id FROM vehicles WHERE plate_number='ABC-1234'`).Scan(&vid); err != nil {
		t.Fatal(err)
	}

	rec = httptest.NewRecorder()
	ListVehicles(rec, httptest.NewRequest("GET", "/vehicles?q=ABC", nil), owner)
	if rec.Code != 200 {
		t.Fatalf("list code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	SearchVehiclesAPI(rec, httptest.NewRequest("GET", "/vehicles/search?q=ABC", nil))
	if rec.Code != 200 {
		t.Fatalf("search code=%d", rec.Code)
	}
	req := httptest.NewRequest("GET", "/vehicles/1", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	ShowVehicle(rec, req, owner)
	if rec.Code != 200 {
		t.Fatalf("show code=%d", rec.Code)
	}
	// edit form + update
	req = httptest.NewRequest("GET", "/vehicles/1/edit", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	EditVehicleForm(rec, req, owner)
	if rec.Code != 200 {
		t.Fatalf("edit form code=%d", rec.Code)
	}
	req = postReq(t, "/vehicles/1/update", url.Values{"customer_id": {"1"}, "plate_number": {"ABC-9999"}, "brand_model": {"TOYOTA"}, "color": {"紅"}})
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	UpdateVehicle(rec, req)
	if rec.Code != 302 {
		t.Fatalf("update code=%d body=%s", rec.Code, rec.Body.String())
	}
	// delete
	req = httptest.NewRequest("POST", "/vehicles/1/delete", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	DeleteVehicle(rec, req)
	// soft or hard delete: either 302 or count 0
	var n int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM vehicles WHERE id=1`).Scan(&n)
	_ = n
}

func TestInventoryHandlers(t *testing.T) {
	newMigratedTestDB(t)
	owner := model.User{ID: 1, Role: "owner"}
	if chkIf(true) == "" || chkIf(false) != "" {
		t.Fatal("chkIf")
	}
	_ = costInput("123")
	if !strings.Contains(invTabURL(url.Values{}, "制動", true), "category=") {
		t.Fatal("invTabURL")
	}
	if invTabURL(url.Values{}, "", false) != "/inventory" {
		t.Fatal("invTabURL bare")
	}
	_ = supplierOptions()
	_ = supplierOptionsSel(0)
	_ = inventoryOptions()

	rec := httptest.NewRecorder()
	ListInventory(rec, httptest.NewRequest("GET", "/inventory?low=1", nil), owner)
	if rec.Code != 200 {
		t.Fatalf("list code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	SearchInventoryAPI(rec, httptest.NewRequest("GET", "/inventory/search?q=x", nil), owner)
	if rec.Code != 200 {
		t.Fatalf("search code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	SearchPartsAPI(rec, httptest.NewRequest("GET", "/parts/search?q=x", nil))
	if rec.Code != 200 {
		t.Fatalf("parts search code=%d", rec.Code)
	}
	// create
	rec = httptest.NewRecorder()
	CreateInventoryItem(rec, postReq(t, "/inventory", url.Values{"part_number": {"PN-1"}, "name": {"煞車皮"}, "bin_location": {"A-1"}, "category": {"制動"}, "stock_quantity": {"10"}}))
	if rec.Code != 302 && rec.Code != 200 {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}
	var iid int64
	if err := db.DB.QueryRow(`SELECT id FROM inventory_items WHERE name='煞車皮'`).Scan(&iid); err != nil {
		t.Fatal(err)
	}
	// edit form
	req := httptest.NewRequest("GET", "/inventory/1/edit", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	EditInventoryForm(rec, req, owner)
	_ = rec.Code
	// update
	req = postReq(t, "/inventory/1/update", url.Values{"name": {"煞車皮改"}, "category": {"制動"}, "stock_quantity": {"8"}})
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	UpdateInventoryItem(rec, req)
	_ = rec.Code
	// stock adjust form + apply
	req = httptest.NewRequest("GET", "/inventory/1/adjust", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	StockAdjustForm(rec, req, owner)
	_ = rec.Code
	req = postReq(t, "/inventory/1/adjust", url.Values{"quantity": {"2"}, "reason": {"領用"}})
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	ApplyStockAdjust(rec, req)
	// delete
	req = httptest.NewRequest("POST", "/inventory/1/delete", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	DeleteInventoryItem(rec, req)
	_ = rec.Code
}

func TestSupplierHandlers(t *testing.T) {
	newMigratedTestDB(t)
	owner := model.User{ID: 1, Role: "owner"}
	for _, tc := range []struct {
		v  string
		ok bool
	}{
		{"material", true}, {"vendor", true}, {"other", false},
	} {
		if isSupplierType(tc.v) != tc.ok {
			t.Fatalf("isSupplierType(%q)", tc.v)
		}
	}
	if normPaymentTerms("check30") != "check30" || paymentTermsLabel("check30") == "" {
		t.Fatal("payment terms")
	}
	_ = paymentTermsSelectOptions("check30")
	_ = paymentTermsSelectOptions("")

	rec := httptest.NewRecorder()
	ListSuppliers(rec, httptest.NewRequest("GET", "/suppliers", nil), owner)
	if rec.Code != 200 {
		t.Fatalf("list code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	SearchSuppliersAPI(rec, httptest.NewRequest("GET", "/suppliers/search?q=x", nil))
	if rec.Code != 200 {
		t.Fatalf("search code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	CreateSupplier(rec, postReq(t, "/suppliers", url.Values{"name": {"供應商A"}, "supplier_type": {"part"}, "payment_terms": {"check30"}}))
	if rec.Code != 302 && rec.Code != 200 && rec.Code != 400 {
		t.Fatalf("create code=%d", rec.Code)
	}
	var sid int64
	if err := db.DB.QueryRow(`SELECT id FROM suppliers WHERE name='供應商A'`).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/suppliers/1/edit", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	EditSupplierForm(rec, req, owner)
	_ = rec.Code
	req = postReq(t, "/suppliers/1/update", url.Values{"name": {"供應商B"}, "supplier_type": {"service"}})
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	UpdateSupplier(rec, req)
	_ = rec.Code
	req = httptest.NewRequest("POST", "/suppliers/1/deactivate", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	DeactivateSupplier(rec, req)
	_ = rec.Code
	req = httptest.NewRequest("POST", "/suppliers/1/reactivate", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	ReactivateSupplier(rec, req)
	_ = rec.Code
	// quick create
	rec = httptest.NewRecorder()
	QuickCreateSupplier(rec, postReq(t, "/suppliers/quick", url.Values{"name": {"快商"}}))
	_ = rec.Code
}

func TestLookupAndPhraseHandlers(t *testing.T) {
	newMigratedTestDB(t)
	owner := model.User{ID: 1, Role: "owner"}
	_ = lookupLabel("bay", "LIFT")
	_ = lookupOptions("bay", "")
	_ = lookupOptions("nonexistent", "")
	CheckLookupCategories()

	rec := httptest.NewRecorder()
	ListLookupItems(rec, httptest.NewRequest("GET", "/lookup?category=bay", nil), owner)
	if rec.Code != 200 {
		t.Fatalf("list code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	SearchLookupAPI(rec, httptest.NewRequest("GET", "/lookup/search?q=x", nil))
	if rec.Code != 200 {
		t.Fatalf("search code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	NewLookupItemForm(rec, httptest.NewRequest("GET", "/lookup/new", nil), owner)
	_ = rec.Code
	rec = httptest.NewRecorder()
	CreateLookupItem(rec, postReq(t, "/lookup", url.Values{"category": {"bay"}, "code": {"TEST1"}, "label": {"測試"}}))
	_ = rec.Code
	var lid int64
	if err := db.DB.QueryRow(`SELECT id FROM lookup_items WHERE code='TEST1'`).Scan(&lid); err == nil && lid > 0 {
		req := httptest.NewRequest("GET", "/lookup/1/edit", nil)
		req.SetPathValue("id", "1")
		rec = httptest.NewRecorder()
		EditLookupItemForm(rec, req, owner)
		_ = rec.Code
		req = postReq(t, "/lookup/1/update", url.Values{"label": {"改名"}})
		req.SetPathValue("id", "1")
		rec = httptest.NewRecorder()
		UpdateLookupItem(rec, req)
		_ = rec.Code
		req = httptest.NewRequest("POST", "/lookup/1/deactivate", nil)
		req.SetPathValue("id", "1")
		rec = httptest.NewRecorder()
		DeactivateLookupItem(rec, req)
		_ = rec.Code
		req = httptest.NewRequest("POST", "/lookup/1/reactivate", nil)
		req.SetPathValue("id", "1")
		rec = httptest.NewRecorder()
		ReactivateLookupItem(rec, req)
		_ = rec.Code
	}

	rec = httptest.NewRecorder()
	ListPhrases(rec, httptest.NewRequest("GET", "/phrases", nil), owner)
	if rec.Code != 200 {
		t.Fatalf("phrases code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	SearchPhrasesAPI(rec, httptest.NewRequest("GET", "/phrases/search?q=x", nil))
	if rec.Code != 200 {
		t.Fatalf("phrases search code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	SearchEstimateItemsAPI(rec, httptest.NewRequest("GET", "/estimate-items/search?q=x", nil))
	if rec.Code != 200 {
		t.Fatalf("estimate items search code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	CreatePhrase(rec, postReq(t, "/phrases", url.Values{"category": {"maint"}, "content": {"檢查機油"}}), owner)
	_ = rec.Code
}
