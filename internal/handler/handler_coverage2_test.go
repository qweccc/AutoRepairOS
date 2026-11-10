package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

func TestStaffHelpersAndList(t *testing.T) {
	newMigratedTestDB(t)
	for _, tc := range []struct {
		kind string
		ok   bool
	}{
		{"employee", true}, {"dependent", true}, {"vendor", false}, {"", false},
	} {
		if isValidStaffKind(tc.kind) != tc.ok {
			t.Fatalf("isValidStaffKind(%q)", tc.kind)
		}
	}
	owner := model.User{ID: 1, Role: "owner"}
	rec := httptest.NewRecorder()
	ListStaff(rec, httptest.NewRequest("GET", "/staff?q=a&kind=employee", nil), owner)
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	rec2 := httptest.NewRecorder()
	SearchStaffAPI(rec2, httptest.NewRequest("GET", "/staff/search?q=a", nil))
	if rec2.Code != 200 {
		t.Fatalf("code=%d", rec2.Code)
	}
}

func TestSOPHelpers(t *testing.T) {
	newMigratedTestDB(t)
	if got := LoadSOPRecords(999); len(got) != 0 {
		t.Fatalf("records %v", got)
	}
	if got := LoadAttachedTemplates(999); len(got) != 0 {
		t.Fatalf("templates %v", got)
	}
	_ = SOPTemplateOptions(nil)
	_ = SOPTemplateOptions([]int64{1, 2})
	if err := CheckSOPCompletionForStatus(999, "pending"); err != nil {
		t.Fatalf("non-completion should pass: %v", err)
	}
	if err := CheckSOPCompletionForStatus(999, "completed"); err != nil {
		t.Fatalf("no records should pass: %v", err)
	}
	if err := CheckSOPCompletionForStatus(999, "paid"); err != nil {
		t.Fatalf("paid no records: %v", err)
	}
	if !SOPAllRequiredComplete(999) {
		t.Fatal("no records = complete")
	}
	e := &SOPIncompleteError{Count: 3}
	if !strings.Contains(e.Error(), "3") {
		t.Fatalf("error %q", e.Error())
	}
	// incomplete required record blocks completion
	if _, err := db.DB.Exec(`INSERT INTO work_order_sop_records (work_order_id, sop_item_id, template_id, description, required, completed, sort_order) VALUES (10, 1, 1, 'check', 1, 0, 1)`); err != nil {
		t.Fatal(err)
	}
	if err := CheckSOPCompletionForStatus(10, "completed"); err == nil {
		t.Fatal("expected incomplete error")
	}
	if SOPAllRequiredComplete(10) {
		t.Fatal("expected incomplete")
	}
	if _, err := db.DB.Exec(`UPDATE work_order_sop_records SET completed=1 WHERE work_order_id=10`); err != nil {
		t.Fatal(err)
	}
	if err := CheckSOPCompletionForStatus(10, "completed"); err != nil {
		t.Fatal(err)
	}
	if !SOPAllRequiredComplete(10) {
		t.Fatal("expected complete")
	}
	updateSOPCompleted(10)
	updateSOPCompleted(999)
	// SOP template list/form handlers render
	owner := model.User{ID: 1, Role: "owner"}
	rec := httptest.NewRecorder()
	ListSOPTemplates(rec, httptest.NewRequest("GET", "/sop?q=x", nil), owner)
	if rec.Code != 200 {
		t.Fatalf("ListSOPTemplates code=%d", rec.Code)
	}
	rec2 := httptest.NewRecorder()
	NewSOPTemplateForm(rec2, httptest.NewRequest("GET", "/sop/new", nil), owner)
	if rec2.Code != 200 {
		t.Fatalf("NewSOPTemplateForm code=%d", rec2.Code)
	}
}

func TestReceivableHelpers(t *testing.T) {
	newMigratedTestDB(t)
	if paymentMismatch(100, 100) || !paymentMismatch(100, 90) || !paymentMismatch(100, 110) || paymentMismatch(0, 50) || paymentMismatch(100, 0) {
		t.Fatal("paymentMismatch")
	}
	cases := []struct {
		r    receivable
		want string
	}{
		{receivable{Cash: 10}, "現金"},
		{receivable{Card: 10}, "信用卡"},
		{receivable{CheckAmount: 10}, "票據"},
		{receivable{TransferAmount: 10}, "轉帳"},
		{receivable{}, "未收款"},
		{receivable{Cash: 5, Card: 5}, "混合"},
	}
	for _, c := range cases {
		got, _ := payMethod(c.r)
		if got != c.want {
			t.Fatalf("payMethod %+v = %q want %q", c.r, got, c.want)
		}
	}
	if pad3(5) != "005" || pad3(1234) != "1234" {
		t.Fatal("pad3")
	}
	if moneyIf(0) == "" || moneyIf(100) == "" {
		t.Fatal("moneyIf")
	}
	if intOrEmpty(0) != "" || intOrEmpty(-1) != "" || intOrEmpty(42) != "42" {
		t.Fatal("intOrEmpty")
	}
	if !strings.Contains(normDateSQL("pay_date"), "date(CASE") {
		t.Fatal("normDateSQL")
	}
	v1 := nextVoucherNo("2026-11-09")
	v2 := nextVoucherNo("2026-11-09")
	if v1 == v2 || !strings.HasPrefix(v1, "RCV-20261109-") {
		t.Fatalf("voucher %q %q", v1, v2)
	}
	_ = nextVoucherNo("") // today fallback
	owner := model.User{ID: 1, Role: "owner"}
	rec := httptest.NewRecorder()
	ListReceivables(rec, httptest.NewRequest("GET", "/receivables", nil), owner)
	if rec.Code != 200 {
		t.Fatalf("ListReceivables code=%d", rec.Code)
	}
	rec2 := httptest.NewRecorder()
	SearchReceivablesAPI(rec2, httptest.NewRequest("GET", "/receivables/search", nil), owner)
	if rec2.Code != 200 {
		t.Fatalf("SearchReceivablesAPI code=%d", rec2.Code)
	}
	_ = woCellText(0)
}

func TestPrintDocHelpers(t *testing.T) {
	if tb := computeTaxBreakdown(0); tb.Total != 0 {
		t.Fatal("zero tax")
	}
	tb := computeTaxBreakdown(1050)
	if tb.Total != 1050 || tb.Tax <= 0 || tb.Taxable+tb.Tax != 1050 {
		t.Fatalf("tax %+v", tb)
	}
	rows := payMethodRows(100, 50, 0, 25)
	if len(rows) != 3 {
		t.Fatalf("rows %d", len(rows))
	}
	if len(payMethodRows(0, 0, 0, 0)) != 0 {
		t.Fatal("empty rows")
	}
	rec := httptest.NewRecorder()
	renderPrintPage(rec, "測試文件", "<p>body</p>")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "測試文件") {
		t.Fatalf("code=%d", rec.Code)
	}
	if !strings.Contains(printToolbar(), "window.print") || !strings.Contains(printToolbar("<a>x</a>"), "<a>x</a>") {
		t.Fatal("printToolbar")
	}
	if !strings.Contains(printHead("報價單", []string{"2026-11-09"}), "報價單") {
		t.Fatal("printHead")
	}
}
