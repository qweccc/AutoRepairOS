package handler

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

func TestPhaseA2ValidationFormErrors(t *testing.T) {
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
	if _, err := db.DB.Exec(`INSERT INTO suppliers (id, name) VALUES (1,'協力廠')`); err != nil {
		t.Fatal(err)
	}
	check := func(name string, rec *httptest.ResponseRecorder, want string) {
		t.Helper()
		if rec.Code != 400 {
			t.Fatalf("%s: code=%d want 400", name, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), want) || !strings.Contains(rec.Body.String(), "msg-err") || !strings.Contains(rec.Body.String(), "返回表單重填") {
			t.Fatalf("%s: body missing form error for %q: %.200s", name, want, rec.Body.String())
		}
	}

	// subcontract: missing work order id
	rec := httptest.NewRecorder()
	CreateSubcontract(rec, postReq(t, "/subcontracts", url.Values{"description": {"x"}}))
	check("subcontract no WO", rec, "工單 ID 為必填")
	// subcontract: WO missing
	rec = httptest.NewRecorder()
	CreateSubcontract(rec, postReq(t, "/subcontracts", url.Values{"work_order_id": {"999"}, "description": {"x"}}))
	check("subcontract bad WO", rec, "工單不存在")
	// subcontract: missing description
	rec = httptest.NewRecorder()
	CreateSubcontract(rec, postReq(t, "/subcontracts", url.Values{"work_order_id": {"10"}}))
	check("subcontract no desc", rec, "加工內容為必填")
	// subcontract update: missing fields
	req := postReq(t, "/subcontracts/1/update", url.Values{})
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	UpdateSubcontract(rec, req)
	check("subcontract update empty", rec, "工單 ID 與加工內容為必填")

	// photo markup: missing marker type (JSON body, photo_id path value)
	jreq := httptest.NewRequest("POST", "/work-orders/10/photos/1/annotations", strings.NewReader(`{"x1":1,"y1":2}`))
	jreq.Header.Set("Content-Type", "application/json")
	jreq.SetPathValue("photo_id", "1")
	rec = httptest.NewRecorder()
	AddPhotoAnnotation(rec, jreq, owner)
	check("photo markup no type", rec, "標記類型為必填")
	// walkaround: missing mark type (JSON body)
	jreq2 := httptest.NewRequest("POST", "/work-orders/10/walkaround", strings.NewReader(`{"x":1,"y":2}`))
	jreq2.Header.Set("Content-Type", "application/json")
	jreq2.SetPathValue("id", "10")
	rec = httptest.NewRecorder()
	AddWalkaroundMark(rec, jreq2, owner)
	check("walkaround no type", rec, "舊傷類型為必填")

	// estimate discount format
	req = postReq(t, "/estimates/1/discount", url.Values{"discount_amount": {"abc"}})
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	SetEstimateDiscount(rec, req)
	check("estimate discount format", rec, "折扣金額格式錯誤")
}
