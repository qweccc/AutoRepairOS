package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Phase A1: Create/Update 輸入驗證失敗必須回 formError 頁面（400 + msg-err
// + 返回表單重填），而非 fail() 的裸文字。
func TestPhaseA1ValidationFormErrors(t *testing.T) {
	newMigratedTestDB(t)

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
		{"CreateSOPTemplate 缺名稱", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			CreateSOPTemplate(rec, postReq(t, "/sop-templates", url.Values{}))
			return rec
		}, "模板名稱為必填"},
		{"CreateSOPTemplate 空白名稱", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			CreateSOPTemplate(rec, postReq(t, "/sop-templates", url.Values{"name": {"  "}}))
			return rec
		}, "模板名稱為必填"},
		{"UpdateSOPTemplate 缺名稱", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			req := postReq(t, "/sop-templates/x", url.Values{})
			req.SetPathValue("id", "1")
			UpdateSOPTemplate(rec, req)
			return rec
		}, "模板名稱為必填"},
		{"CreateSOPItem 無效模板ID", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			req := postReq(t, "/sop-templates/0/items", url.Values{"description": {"檢查"}})
			req.SetPathValue("template_id", "0")
			CreateSOPItem(rec, req)
			return rec
		}, "無效的模板 ID"},
		{"CreateSOPItem 缺描述", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			req := postReq(t, "/sop-templates/1/items", url.Values{})
			req.SetPathValue("template_id", "1")
			CreateSOPItem(rec, req)
			return rec
		}, "檢查項目描述為必填"},
	}

	for _, tc := range cases {
		rec := tc.call()
		assertFormError(tc.name, rec, tc.msg)
	}

	// CreatePurchaseOrder validation re-renders the templ form itself
	// (same screen, supplier/date preserved via existing page params).
	for _, tc := range []struct {
		name string
		form url.Values
	}{
		{"missing supplier", url.Values{}},
		{"invalid supplier", url.Values{"supplier_id": {"abc"}}},
	} {
		rec := httptest.NewRecorder()
		CreatePurchaseOrder(rec, postReq(t, "/purchase-orders", tc.form))
		if rec.Code != 400 {
			t.Fatalf("%s: code=%d want 400", tc.name, rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "請選擇供應商") || !strings.Contains(body, "newPurchaseOrderForm") {
			t.Fatalf("%s: form not re-rendered: %.200s", tc.name, body)
		}
	}
}
