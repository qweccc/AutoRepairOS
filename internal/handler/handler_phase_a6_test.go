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

func phaseA6AssertFormError(t *testing.T, rec *httptest.ResponseRecorder, wantSub string) {
	t.Helper()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d want 400 body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "msg-err") {
		t.Fatalf("body missing msg-err: %s", body)
	}
	if !strings.Contains(body, "返回表單重填") {
		t.Fatalf("body missing 返回表單重填: %s", body)
	}
	if wantSub != "" && !strings.Contains(body, wantSub) {
		t.Fatalf("body missing %q: %s", wantSub, body)
	}
}

func TestPhaseA6ReceivablesUpdateValidation(t *testing.T) {
	newMigratedTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO receivables (id, voucher_no, customer_id, pay_total, cash) VALUES (1,'RCV-1',1,100,100)`); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		form url.Values
		want string
	}{
		{"negative", url.Values{"voucher_no": {"V"}, "cash": {"-1"}}, "收款金額不可為負數"},
		{"empty-method", url.Values{"voucher_no": {"V"}}, "請至少填一種付款方式或收款合計"},
		{"mismatch", url.Values{"voucher_no": {"V"}, "cash": {"100"}, "pay_total": {"200"}}, "收款合計與各付款方式加總不符"},
		{"empty-voucher", url.Values{"cash": {"100"}}, "收款單號不能為空"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := postReq(t, "/receivables/1", tc.form)
			req.SetPathValue("id", "1")
			rec := httptest.NewRecorder()
			UpdateReceivable(rec, req)
			phaseA6AssertFormError(t, rec, tc.want)
		})
	}
}

func TestPhaseA6CustomersUpdateValidation(t *testing.T) {
	newMigratedTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912')`); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		form url.Values
		want string
	}{
		{"required", url.Values{"name": {""}, "phone": {""}}, "姓名與電話為必填"},
		{"bad-telegram", url.Values{"name": {"王小明"}, "phone": {"0912"}, "telegram_user_id": {"abc"}}, "Telegram"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := postReq(t, "/customers/1", tc.form)
			req.SetPathValue("id", "1")
			rec := httptest.NewRecorder()
			UpdateCustomer(rec, req)
			phaseA6AssertFormError(t, rec, tc.want)
		})
	}
}

func TestPhaseA6StaffCreateValidation(t *testing.T) {
	newMigratedTestDB(t)
	owner := model.User{ID: 1, Role: "owner"}
	req := postReq(t, "/staff", url.Values{})
	rec := httptest.NewRecorder()
	CreateStaff(rec, req, owner)
	phaseA6AssertFormError(t, rec, "帳號、密碼、姓名為必填")
}

func TestPhaseA6NegativeStockFixValidation(t *testing.T) {
	newMigratedTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO inventory_items (id, part_number, name, bin_location, stock_quantity, min_safety_stock) VALUES (1,'N-1','電源線','A1',-117,10)`); err != nil {
		t.Fatal(err)
	}
	owner := model.User{ID: 1, Role: "owner"}
	cases := []struct {
		name string
		form url.Values
		want string
	}{
		{"negative-target", url.Values{"item_id": {"1"}, "mode": {"custom"}, "target": {"-5"}}, "指定數量不可為負數"},
		{"unknown-mode", url.Values{"item_id": {"1"}, "mode": {"bogus"}}, "未知的補正方式"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := postReq(t, "/inventory/negative/fix", tc.form)
			rec := httptest.NewRecorder()
			ApplyNegativeStockFix(rec, req, owner)
			phaseA6AssertFormError(t, rec, tc.want)
		})
	}
}
