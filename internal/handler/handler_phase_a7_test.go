package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"autorepairos/internal/model"
)

// TestPhaseA7FormError 驗證 Phase A7 轉換的輸入驗證分支回 formError 頁面
// （400 + msg-err + 返回表單重填），而非裸 fail 文字。
func TestPhaseA7FormError(t *testing.T) {
	newMigratedTestDB(t)
	owner := model.User{ID: 1, Role: "owner"}

	type tc struct {
		name string
		call func(w http.ResponseWriter, r *http.Request)
		msg  string
	}
	mk := func(path, id string, form url.Values) *http.Request {
		r := postReq(t, path, form)
		if id != "" {
			r.SetPathValue("id", id)
		}
		return r
	}
	cases := []tc{
		{"CreateVehicleMileage 負里程", func(w http.ResponseWriter, r *http.Request) {
			CreateVehicleMileage(w, r)
		}, "請填寫有效里程（不可為負數）"},
		{"RecordAPPayment 金額格式", func(w http.ResponseWriter, r *http.Request) {
			RecordAPPayment(w, r)
		}, "付款金額格式錯誤"},
		{"CreateLookupItem 必填", func(w http.ResponseWriter, r *http.Request) {
			CreateLookupItem(w, r)
		}, "分類、代碼、名稱為必填"},
		{"UpdateLookupItem 必填", func(w http.ResponseWriter, r *http.Request) {
			UpdateLookupItem(w, r)
		}, "代碼、名稱為必填"},
		{"CreateSupplier 名稱必填", func(w http.ResponseWriter, r *http.Request) {
			CreateSupplier(w, r)
		}, "名稱為必填"},
		{"UpdateSupplier 名稱必填", func(w http.ResponseWriter, r *http.Request) {
			UpdateSupplier(w, r)
		}, "名稱為必填"},
		{"CreatePhrase 內容必填", func(w http.ResponseWriter, r *http.Request) {
			CreatePhrase(w, r, owner)
		}, "片語內容為必填"},
		{"UpdatePhrase 內容必填", func(w http.ResponseWriter, r *http.Request) {
			UpdatePhrase(w, r, owner)
		}, "片語內容為必填"},
	}
	reqs := map[string]*http.Request{
		"CreateVehicleMileage 負里程": mk("/vehicles/7/mileage", "7", url.Values{"mileage": {"-5"}}),
		"RecordAPPayment 金額格式":     mk("/ap/1/payments", "1", url.Values{"amount": {"abc"}}),
		"CreateLookupItem 必填":      mk("/lookup", "", url.Values{}),
		"UpdateLookupItem 必填":      mk("/lookup/1", "1", url.Values{"code": {""}, "label": {""}}),
		"CreateSupplier 名稱必填":      mk("/suppliers", "", url.Values{"name": {""}}),
		"UpdateSupplier 名稱必填":      mk("/suppliers/1", "1", url.Values{"name": {""}}),
		"CreatePhrase 內容必填":        mk("/phrases/new", "", url.Values{"phrase": {""}}),
		"UpdatePhrase 內容必填":        mk("/phrases/1/update", "1", url.Values{"phrase": {""}}),
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c.call(rec, reqs[c.name])
			body := rec.Body.String()
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("code=%d body=%.200s", rec.Code, body)
			}
			if !strings.Contains(body, "msg-err") || !strings.Contains(body, "返回表單重填") {
				t.Fatalf("missing formError markers: %.300s", body)
			}
			if !strings.Contains(body, c.msg) {
				t.Fatalf("missing msg %q in %.300s", c.msg, body)
			}
		})
	}
}
