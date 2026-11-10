package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// Phase A3: Create/Update 輸入驗證失敗必須回 formError 頁面（400 + msg-err
// + 返回表單重填），而非 fail() 的裸文字。
// 業務規則／DB 錯誤維持 fail()，見 TestPhaseA3RetainedFailErrors。
func TestPhaseA3ValidationFormErrors(t *testing.T) {
	newMigratedTestDB(t)

	// CreateWorkOrder 後段驗證（保險／理賠）需先通過車輛存在檢查
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (1,1,'ABC-1234','TOYOTA')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO maintenance_templates (id, code, name) VALUES (1,'A0001','小保養')`); err != nil {
		t.Fatal(err)
	}

	user := model.User{ID: 1, Username: "owneruser", Role: "owner"}

	assertFormError := func(name string, rec *httptest.ResponseRecorder, wantMsg string) {
		t.Helper()
		body := rec.Body.String()
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: code=%d, want 400, body=%s", name, rec.Code, body)
		}
		for _, want := range []string{wantMsg, "msg-err", "返回表單重填"} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s: body missing %q, body=%s", name, want, body)
			}
		}
	}

	cases := []struct {
		name string
		call func() *httptest.ResponseRecorder
		msg  string
	}{
		{"AddWorkOrderItem 數量為零", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			req := postReq(t, "/work-orders/1/items", url.Values{"quantity": {"0"}, "description": {"檢查"}})
			req.SetPathValue("id", "1")
			AddWorkOrderItem(rec, req, user)
			return rec
		}, "數量必須大於 0"},
		{"AddWorkOrderItem 缺描述", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			req := postReq(t, "/work-orders/1/items", url.Values{"quantity": {"1"}})
			req.SetPathValue("id", "1")
			AddWorkOrderItem(rec, req, user)
			return rec
		}, "描述為必填"},
		{"CreateMaintenanceTemplate 缺代碼名稱", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			CreateMaintenanceTemplate(rec, postReq(t, "/maintenance-templates", url.Values{"code": {"X1"}}))
			return rec
		}, "代碼與名稱為必填"},
		{"UpdateMaintenanceTemplate 缺名稱", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			req := postReq(t, "/maintenance-templates/1", url.Values{"name": {"  "}})
			req.SetPathValue("id", "1")
			UpdateMaintenanceTemplate(rec, req)
			return rec
		}, "套餐名稱為必填"},
		{"ReplaceMaintenanceTemplateItems 表單解析失敗", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/maintenance-templates/1/items", strings.NewReader("x=%zz"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.SetPathValue("id", "1")
			ReplaceMaintenanceTemplateItems(rec, req)
			return rec
		}, "表單解析失敗"},
		// CreateWorkOrder validation is same-screen re-rendered (Phase B):
		// verified separately below, not via the formError table.
		{"CreateWorkOrder 勾保險未選公司", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			CreateWorkOrder(rec, postReq(t, "/work-orders", url.Values{
				"vehicle_id": {"1"}, "current_mileage": {"1000"}, "is_insurance": {"1"},
			}))
			return rec
		}, "請選擇保險公司"},
		{"CreateWorkOrder 理賠日期格式錯誤", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			CreateWorkOrder(rec, postReq(t, "/work-orders", url.Values{
				"vehicle_id": {"1"}, "current_mileage": {"1000"}, "claim_date": {"2026-13-99"},
			}))
			return rec
		}, "理賠日期格式錯誤"},
		{"CreateWorkOrder 理賠金額為負", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			CreateWorkOrder(rec, postReq(t, "/work-orders", url.Values{
				"vehicle_id": {"1"}, "current_mileage": {"1000"}, "claim_amount": {"-5"},
			}))
			return rec
		}, "理賠金額必須為不小於 0 的數字"},
		{"UpdateWorkOrderClaim 理賠日期格式錯誤", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			req := postReq(t, "/work-orders/1/claim", url.Values{"claim_date": {"2026-13-99"}})
			req.SetPathValue("id", "1")
			UpdateWorkOrderClaim(rec, req)
			return rec
		}, "理賠日期格式錯誤"},
		{"UpdateWorkOrderClaim 理賠金額非法", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			req := postReq(t, "/work-orders/1/claim", url.Values{"claim_amount": {"abc"}})
			req.SetPathValue("id", "1")
			UpdateWorkOrderClaim(rec, req)
			return rec
		}, "理賠金額必須為不小於 0 的數字"},
	}

	for _, tc := range cases {
		rec := tc.call()
		assertFormError(tc.name, rec, tc.msg)
	}

	// Phase B: CreateWorkOrder re-renders the templ form in place.
	for _, tc := range []struct {
		name string
		form url.Values
		want string
	}{
		{"missing vehicle", url.Values{"current_mileage": {"1000"}}, "請選擇車輛"},
		{"missing mileage", url.Values{"vehicle_id": {"1"}}, "請填寫進廠里程"},
	} {
		rec := httptest.NewRecorder()
		CreateWorkOrder(rec, postReq(t, "/work-orders", tc.form))
		if rec.Code != 400 {
			t.Fatalf("%s: code=%d want 400", tc.name, rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, tc.want) || !strings.Contains(body, `id="woForm"`) {
			t.Fatalf("%s: form not re-rendered: %.200s", tc.name, body)
		}
	}
}

// 保留 fail() 的業務規則／DB 錯誤：已結帳鎖定、查無關聯資料、庫存、DB 失敗。
func TestPhaseA3RetainedFailErrors(t *testing.T) {
	newMigratedTestDB(t)

	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (1,1,'ABC-1234','TOYOTA')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO work_orders (id, customer_id, vehicle_id, status, total_amount, current_mileage) VALUES (1,1,1,'paid',0,50000)`); err != nil {
		t.Fatal(err)
	}

	user := model.User{ID: 1, Username: "owneruser", Role: "owner"}

	assertPlainFail := func(name string, rec *httptest.ResponseRecorder, wantMsg string) {
		t.Helper()
		body := rec.Body.String()
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: code=%d, want 400, body=%s", name, rec.Code, body)
		}
		if !strings.Contains(body, wantMsg) {
			t.Fatalf("%s: body missing %q, body=%s", name, wantMsg, body)
		}
		if strings.Contains(body, "msg-err") || strings.Contains(body, "返回表單重填") {
			t.Fatalf("%s: retained fail() must stay plain text, body=%s", name, body)
		}
	}

	// 已結帳鎖定（paid-lock）：AddWorkOrderItem / DeleteWorkOrderItem / UpdateWorkOrderClaim
	rec := httptest.NewRecorder()
	req := postReq(t, "/work-orders/1/items", url.Values{"quantity": {"1"}, "description": {"檢查"}})
	req.SetPathValue("id", "1")
	AddWorkOrderItem(rec, req, user)
	assertPlainFail("AddWorkOrderItem 已結帳鎖定", rec, "此工單已結帳")

	rec = httptest.NewRecorder()
	req = postReq(t, "/work-orders/1/claim", url.Values{"claim_amount": {"100"}})
	req.SetPathValue("id", "1")
	UpdateWorkOrderClaim(rec, req)
	assertPlainFail("UpdateWorkOrderClaim 已結帳鎖定", rec, "此工單已結帳")

	// 查無車輛（DB 關聯查無）：CreateWorkOrder
	rec = httptest.NewRecorder()
	CreateWorkOrder(rec, postReq(t, "/work-orders", url.Values{"vehicle_id": {"999"}, "current_mileage": {"1000"}}))
	assertPlainFail("CreateWorkOrder 查無車輛", rec, "找不到該車輛")

	// 查無零件（DB 關聯查無）：AddWorkOrderItem（用未結帳工單 id=2 避免鎖定擋下）
	if _, err := db.DB.Exec(`INSERT INTO work_orders (id, customer_id, vehicle_id, status, total_amount, current_mileage) VALUES (2,1,1,'pending',0,50000)`); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/work-orders/2/items", url.Values{
		"quantity": {"1"}, "description": {"x"}, "item_type": {"part"}, "inventory_item_id": {"999"},
	})
	req.SetPathValue("id", "2")
	AddWorkOrderItem(rec, req, user)
	assertPlainFail("AddWorkOrderItem 查無零件", rec, "找不到該零件")
}
