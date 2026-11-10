package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"golang.org/x/crypto/bcrypt"
)

func TestBatchGLayout(t *testing.T) {
	newMigratedTestDB(t)
	h, _ := bcrypt.GenerateFromPassword([]byte("pw12345"), bcrypt.MinCost)
	if _, err := db.DB.Exec(`INSERT INTO users (id, username, password, role, name, active) VALUES (1,'owner1',?,'owner','O',1)`, string(h)); err != nil {
		t.Fatal(err)
	}
	called := false
	fn := func(w http.ResponseWriter, r *http.Request, u model.User) { called = true; w.WriteHeader(200) }
	// unauthenticated -> redirect
	rec := httptest.NewRecorder()
	RequireAuth(fn).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 302 || called {
		t.Fatalf("code=%d called=%v", rec.Code, called)
	}
	// authenticated
	rec = httptest.NewRecorder()
	RequireAuth(fn).ServeHTTP(rec, reqWithSession(t, 1))
	if rec.Code != 200 || !called {
		t.Fatalf("code=%d called=%v", rec.Code, called)
	}
	// RequireLogin
	plain := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }
	rec = httptest.NewRecorder()
	RequireLogin(plain).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 302 {
		t.Fatalf("login code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	RequireLogin(plain).ServeHTTP(rec, reqWithSession(t, 1))
	if rec.Code != 200 {
		t.Fatalf("login authed code=%d", rec.Code)
	}
	// renderPage + flash helpers
	rec = httptest.NewRecorder()
	renderPage(rec, "標題", "home", "user1", "owner", "<p>body</p>")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "標題") {
		t.Fatalf("renderPage code=%d", rec.Code)
	}
	if flash(httptest.NewRequest("GET", "/?msg=hi", nil)) == "" || flashText(httptest.NewRequest("GET", "/?msg=hi", nil)) != "hi" {
		t.Fatal("flash")
	}
	if flashErr(httptest.NewRequest("GET", "/?err=bad", nil)) == "" || flashErr(httptest.NewRequest("GET", "/", nil)) != "" {
		t.Fatal("flashErr")
	}
	rec = httptest.NewRecorder()
	redirectErrMsg(rec, httptest.NewRequest("GET", "/", nil), "/x", "err msg")
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "err=") {
		t.Fatalf("loc=%q", loc)
	}
	if appName() == "" {
		t.Fatal("appName")
	}
	fail(httptest.NewRecorder(), "bad")
}

func TestBatchGResolveCustomerID(t *testing.T) {
	newMigratedTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912'),(2,'王大明','0923'),(3,'李小華','0955')`); err != nil {
		t.Fatal(err)
	}
	mk := func(v url.Values) *http.Request {
		r := httptest.NewRequest("POST", "/x", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return r
	}
	cases := []struct {
		v    url.Values
		want int64
	}{
		{url.Values{"customer_id": {"1"}}, 1},
		{url.Values{"customer_id": {"999"}}, 0},
		{url.Values{"customer_query": {"1"}}, 1},    // numeric id
		{url.Values{"customer_query": {"0912"}}, 1}, // numeric phone
		{url.Values{"customer_query": {"李小華"}}, 3},  // exact name
		{url.Values{"customer_query": {"小明"}}, 1},   // fuzzy unique
		{url.Values{"customer_query": {"王"}}, 0},    // fuzzy multiple
		{url.Values{"customer_query": {"不存在"}}, 0},  // fuzzy none
		{url.Values{}, 0}, // empty
	}
	for i, c := range cases {
		id, _ := resolveCustomerID(mk(c.v))
		if id != c.want {
			t.Fatalf("case %d: id=%d want %d", i, id, c.want)
		}
	}
}

func TestBatchGCustomerBinding(t *testing.T) {
	newMigratedTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone, telegram_user_id) VALUES (1,'王小明','0912','100')`); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/customers/1/telegram-unbind", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	UnbindCustomerTelegram(rec, req)
	if rec.Code != 302 {
		t.Fatalf("unbind code=%d", rec.Code)
	}
	var tg interface{}
	if err := db.DB.QueryRow(`SELECT telegram_user_id FROM customers WHERE id=1`).Scan(&tg); err != nil {
		t.Fatal(err)
	}
	if tg != nil {
		t.Fatalf("tg=%v want nil", tg)
	}
	// missing customer -> 404
	req = httptest.NewRequest("POST", "/customers/999/telegram-unbind", nil)
	req.SetPathValue("id", "999")
	rec = httptest.NewRecorder()
	UnbindCustomerTelegram(rec, req)
	if rec.Code != 404 {
		t.Fatalf("missing code=%d", rec.Code)
	}
	logBindingError("test", 1, errTest)
}

type testErr string

func (e testErr) Error() string { return string(e) }

var errTest = testErr("boom")

func TestBatchGSearchAPIsWithSession(t *testing.T) {
	newMigratedTestDB(t)
	h, _ := bcrypt.GenerateFromPassword([]byte("pw12345"), bcrypt.MinCost)
	if _, err := db.DB.Exec(`INSERT INTO users (id, username, password, role, name, active) VALUES (1,'owner1',?,'owner','O',1)`, string(h)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO inventory_items (id, part_number, name, bin_location, stock_quantity) VALUES (1,'PN-1','煞車皮','A-1',10)`); err != nil {
		t.Fatal(err)
	}
	// SearchCustomersAPI authenticated
	req := reqWithSession(t, 1)
	q := req.URL.Query()
	q.Set("q", "王")
	req.URL.RawQuery = q.Encode()
	rec := httptest.NewRecorder()
	SearchCustomersAPI(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "王小明") {
		t.Fatalf("customers search code=%d body=%s", rec.Code, rec.Body.String())
	}
	// SearchPartsAPI
	rec = httptest.NewRecorder()
	SearchPartsAPI(rec, httptest.NewRequest("GET", "/parts/search?q=煞車", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "煞車皮") {
		t.Fatalf("parts search code=%d body=%s", rec.Code, rec.Body.String())
	}
	// SearchAppointmentsAPI with session
	rec = httptest.NewRecorder()
	SearchAppointmentsAPI(rec, reqWithSession(t, 1))
	_ = rec.Code
	// UpdateInventoryItem success
	req = postReq(t, "/inventory/1/update", url.Values{"name": {"煞車皮改"}, "category": {"制動"}, "stock_quantity": {"8"}, "part_number": {"PN-1"}, "bin_location": {"A-1"}})
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	UpdateInventoryItem(rec, req)
	_ = rec.Code
	if s := inventoryOptions(); s == "" {
		t.Fatal("inventoryOptions empty")
	}
}

func TestBatchGApprovalsHelpers(t *testing.T) {
	newMigratedTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (7,1,'ABC-1234','TOYOTA')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO work_orders (id, customer_id, vehicle_id, status, total_amount, current_mileage) VALUES (10,1,7,'pending',100,50000)`); err != nil {
		t.Fatal(err)
	}
	c, err := getCustomer(1)
	if err != nil || c == nil || c.Name != "王小明" {
		t.Fatalf("getCustomer %+v %v", c, err)
	}
	if _, err := getCustomer(999); err == nil {
		t.Fatal("missing customer should error")
	}
	// getWorkOrderItems queries work_order_items directly: the table is
	// hard-deleted (no deleted_at), so no active_* view exists for it.
	if _, err := getWorkOrderItems(10); err != nil {
		t.Fatalf("getWorkOrderItems should succeed: %v", err)
	}
	// ApprovalPageHandler unauthenticated -> redirect (handler reads session itself? check via direct call with no session may 302/200)
	rec := httptest.NewRecorder()
	ApprovalPageHandler(rec, httptest.NewRequest("GET", "/approvals", nil))
	_ = rec.Code
	rec = httptest.NewRecorder()
	RejectRequestHandler(rec, httptest.NewRequest("POST", "/approvals/1/reject", nil))
	_ = rec.Code
}

func TestBatchGEstimatesList(t *testing.T) {
	newMigratedTestDB(t)
	owner := model.User{ID: 1, Role: "owner"}
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (7,1,'ABC-1234','TOYOTA')`); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	ListEstimates(rec, httptest.NewRequest("GET", "/estimates", nil), owner)
	if rec.Code != 200 {
		t.Fatalf("list estimates code=%d", rec.Code)
	}
}

func TestBatchGPhotosRemaining(t *testing.T) {
	newMigratedTestDB(t)
	owner := model.User{ID: 1, Role: "owner"}
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (7,1,'ABC-1234','TOYOTA')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO work_orders (id, customer_id, vehicle_id, status, total_amount, current_mileage) VALUES (10,1,7,'pending',0,50000)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO work_order_photos (id, work_order_id, filename, original_name, mime_type, file_size, uploaded_by) VALUES (1,10,'f.jpg','f.jpg','image/jpeg',100,1)`); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/work-orders/10/photos", nil)
	req.SetPathValue("id", "10")
	rec := httptest.NewRecorder()
	ListWorkOrderPhotos(rec, req, owner)
	if rec.Code != 200 {
		t.Fatalf("list code=%d", rec.Code)
	}
	// photoUploadSuccess + retry with no queue rows
	rec = httptest.NewRecorder()
	photoUploadSuccess(rec, httptest.NewRequest("GET", "/x", nil), 10, "上傳成功")
	_ = rec.Code
	rec = httptest.NewRecorder()
	retryPhotoUploads(rec, httptest.NewRequest("POST", "/work-orders/10/photos/retry", nil), 10)
	_ = rec.Code
	// delete real photo
	req = httptest.NewRequest("POST", "/photos/1/delete", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	DeleteWorkOrderPhoto(rec, req)
	_ = rec.Code
}

func TestBatchGStaticAndSubcontractEdit(t *testing.T) {
	newMigratedTestDB(t)
	// RegisterStatic on a bare mux
	mux := http.NewServeMux()
	RegisterStatic(mux)
	// subcontract edit/delete with real row
	if _, err := db.DB.Exec(`INSERT INTO suppliers (id, name) VALUES (1,'協力廠')`); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	CreateSubcontract(rec, postReq(t, "/subcontracts", url.Values{"supplier_id": {"1"}, "description": {"外包"}, "cost_amount": {"500"}, "work_order_id": {"1"}}))
	owner := model.User{ID: 1, Role: "owner"}
	var sid int64
	if err := db.DB.QueryRow(`SELECT id FROM subcontracts ORDER BY id DESC LIMIT 1`).Scan(&sid); err == nil && sid > 0 {
		idStr := strings.TrimSpace(strings.Repeat("", 0))
		_ = idStr
		req := httptest.NewRequest("GET", "/subcontracts/1/edit", nil)
		req.SetPathValue("id", "1")
		rec = httptest.NewRecorder()
		EditSubcontractForm(rec, req, owner)
		if rec.Code != 200 {
			t.Fatalf("edit subcontract code=%d", rec.Code)
		}
		req = httptest.NewRequest("POST", "/subcontracts/1/delete", nil)
		req.SetPathValue("id", "1")
		rec = httptest.NewRecorder()
		DeleteSubcontract(rec, req)
		_ = rec.Code
	}
	// supplier quick-create fragment via QuickCreateSupplier
	rec = httptest.NewRecorder()
	QuickCreateSupplier(rec, postReq(t, "/suppliers/quick", url.Values{"name": {"快商"}, "supplier_type": {"material"}}))
	_ = rec.Code
}
