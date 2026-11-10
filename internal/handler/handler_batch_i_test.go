package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/integration/vision"
)

func TestBatchIFormErrorRendering(t *testing.T) {
	newMigratedTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912')`); err != nil {
		t.Fatal(err)
	}
	// Customer create is same-screen re-rendered (not the formError shell):
	// error + refilled input appear on the list page itself.
	rec := httptest.NewRecorder()
	CreateCustomer(rec, postReq(t, "/customers", url.Values{"name": {"王小明"}, "address": {"台北市"}, "notes": {"VIP"}}))
	if rec.Code != 400 {
		t.Fatalf("customer create code=%d want 400", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "姓名與電話為必填") || !strings.Contains(body, "王小明") || !strings.Contains(body, "台北市") || !strings.Contains(body, "VIP") {
		t.Fatalf("customer error/input not preserved: %.300s", body)
	}
	cases := []struct {
		name string
		call func(w http.ResponseWriter, r *http.Request)
		path string
		form url.Values
		want string
	}{
		{"vehicle missing plate", func(w http.ResponseWriter, r *http.Request) { CreateVehicle(w, r) }, "/vehicles", url.Values{"customer_id": {"1"}}, "車牌、車型為必填"},
	}
	// Inventory create is same-screen re-rendered (list page + create form).
	rec = httptest.NewRecorder()
	CreateInventoryItem(rec, postReq(t, "/inventory", url.Values{"name": {"煞車皮"}, "stock_quantity": {"9"}}))
	if rec.Code != 400 {
		t.Fatalf("inventory create code=%d want 400", rec.Code)
	}
	body = rec.Body.String()
	if !strings.Contains(body, "料號、名稱、庫位為必填") || !strings.Contains(body, "煞車皮") || !strings.Contains(body, "9") {
		t.Fatalf("inventory error/input not preserved: %.300s", body)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", c.path, strings.NewReader(c.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			c.call(rec, req)
			if rec.Code != 400 {
				t.Fatalf("code=%d want 400", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, c.want) {
				t.Fatalf("body missing error %q: %.200s", c.want, body)
			}
			if !strings.Contains(body, "msg-err") || !strings.Contains(body, "返回表單重填") {
				t.Fatalf("body missing form error rendering: %.200s", body)
			}
		})
	}
	t.Run("htmx gets fragment only", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/vehicles", strings.NewReader(url.Values{"customer_id": {"1"}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		CreateVehicle(rec, req)
		if rec.Code != 400 {
			t.Fatalf("code=%d want 400", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "車牌、車型為必填") || strings.Contains(body, "返回表單重填") || strings.Contains(body, "<html") {
			t.Fatalf("hx body should be fragment only: %.200s", body)
		}
	})
}

func TestBatchISupplierOptionsSearch(t *testing.T) {
	newMigratedTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO suppliers (id, name) VALUES (1,'富邦產險'),(2,'新光產險'),(3,'南山人壽')`); err != nil {
		t.Fatal(err)
	}
	// no q: all
	rec := httptest.NewRecorder()
	SupplierOptionsAPI(rec, httptest.NewRequest("GET", "/api/supplier-options", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "南山人壽") {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	// q filters
	rec = httptest.NewRecorder()
	SupplierOptionsAPI(rec, httptest.NewRequest("GET", "/api/supplier-options?q=富邦", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "富邦產險") || strings.Contains(rec.Body.String(), "南山人壽") {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	// HX fragment with q
	req := httptest.NewRequest("GET", "/api/supplier-options?q=新光", nil)
	req.Header.Set("HX-Request", "true")
	rec = httptest.NewRecorder()
	SupplierOptionsAPI(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "新光產險") {
		t.Fatalf("hx code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestBatchIDecodeVisionImageBranches(t *testing.T) {
	// visionClient is nil in handler tests unless set: 503 path via helper
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/vision/plate", strings.NewReader(`{"image":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	RecognizePlate(rec, req)
	if rec.Code != 503 {
		t.Fatalf("nil client code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	RecognizeInvoice(rec, req)
	if rec.Code != 503 {
		t.Fatalf("nil client invoice code=%d", rec.Code)
	}
	// decode helper direct: missing image with client nil already covered; bad JSON needs client.
	// Set a client pointing nowhere: decode happens before recognize call.
	old := visionClient
	defer func() { visionClient = old }()
	visionClient = vision.NewClient("local", "", "", "", "http://127.0.0.1:1", false, 0, "m", "", "", "")
	rec = httptest.NewRecorder()
	bad := httptest.NewRequest("POST", "/vision/plate", strings.NewReader(`not-json`))
	bad.Header.Set("Content-Type", "application/json")
	RecognizeInvoice(rec, bad)
	if rec.Code != 400 {
		t.Fatalf("bad json code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	empty := httptest.NewRequest("POST", "/vision/plate", strings.NewReader(`{"image":""}`))
	empty.Header.Set("Content-Type", "application/json")
	RecognizePlate(rec, empty)
	if rec.Code != 400 {
		t.Fatalf("empty image code=%d", rec.Code)
	}
}
