package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Phase A5: Create/Update 輸入驗證 fail() 已轉 formError —
// 斷言 400 + body 含 msg-err + 返回表單重填 + 預期訊息。

func TestPhaseA5FormErrorValidation(t *testing.T) {
	d := newMigratedTestDB(t)
	if _, err := d.Exec(`INSERT INTO customers (id, name, phone) VALUES (1, '測試客', '0900000000')`); err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	type tc struct {
		name    string
		path    string
		id      string // PathValue("id")，空 = 不設定
		form    url.Values
		handler func(http.ResponseWriter, *http.Request)
		wantMsg string
	}
	cases := []tc{
		{
			name:    "CreateVehicle 缺客戶",
			path:    "/vehicles",
			form:    url.Values{},
			handler: CreateVehicle,
			wantMsg: "請輸入客戶",
		},
		{
			name:    "CreateVehicle 空車牌車型",
			path:    "/vehicles",
			form:    url.Values{"customer_id": {"1"}},
			handler: CreateVehicle,
			wantMsg: "車牌、車型為必填",
		},
		{
			name:    "UpdateVehicle 空車牌車型",
			path:    "/vehicles/1",
			id:      "1",
			form:    url.Values{"customer_id": {"1"}},
			handler: UpdateVehicle,
			wantMsg: "車牌、車型為必填",
		},
		{
			name:    "UpdateVehicle 缺客戶",
			path:    "/vehicles/1",
			id:      "1",
			form:    url.Values{"plate_number": {"ABC-1234"}, "brand_model": {"Toyota"}},
			handler: UpdateVehicle,
			wantMsg: "請輸入客戶",
		},
		{
			name:    "CreateInsuranceCompany 空白",
			path:    "/insurance-companies",
			form:    url.Values{},
			handler: CreateInsuranceCompany,
			wantMsg: "簡稱與公司名稱為必填",
		},
		{
			name:    "UpdateInsuranceCompany 空白",
			path:    "/insurance-companies/1",
			id:      "1",
			form:    url.Values{},
			handler: UpdateInsuranceCompany,
			wantMsg: "簡稱與公司名稱為必填",
		},
		{
			name:    "CreateEquipment 空名",
			path:    "/equipment",
			form:    url.Values{},
			handler: CreateEquipment,
			wantMsg: "設備名稱為必填",
		},
		{
			name:    "UpdateEquipment 空名",
			path:    "/equipment/1",
			id:      "1",
			form:    url.Values{},
			handler: UpdateEquipment,
			wantMsg: "設備名稱為必填",
		},
		{
			name:    "CreateEquipmentMaintenance 空日期",
			path:    "/equipment/1/maintenance",
			id:      "1",
			form:    url.Values{},
			handler: CreateEquipmentMaintenance,
			wantMsg: "保養日期為必填",
		},
		{
			name:    "UpdateWorkOrderStatus 無效狀態",
			path:    "/work-orders/1/status",
			id:      "1",
			form:    url.Values{"status": {"bogus"}},
			handler: UpdateWorkOrderStatus,
			wantMsg: "無效狀態",
		},
		{
			name:    "UpdateWorkOrderApproval 無效核准",
			path:    "/work-orders/1/approval",
			id:      "1",
			form:    url.Values{"approval_status": {"bogus"}},
			handler: UpdateWorkOrderApproval,
			wantMsg: "無效核准狀態",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := postReq(t, c.path, c.form)
			if c.id != "" {
				req.SetPathValue("id", c.id)
			}
			rec := httptest.NewRecorder()
			c.handler(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if !strings.Contains(body, "msg-err") {
				t.Errorf("body missing msg-err: %s", body)
			}
			if !strings.Contains(body, "返回表單重填") {
				t.Errorf("body missing 返回表單重填 link: %s", body)
			}
			if !strings.Contains(body, c.wantMsg) {
				t.Errorf("body missing %q: %s", c.wantMsg, body)
			}
		})
	}
}
