package handler

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/internal/session"
)

func TestEncodeReportExportCSVProtectsSpreadsheetText(t *testing.T) {
	rows := []reportExportRow{
		{Customer: "=1+1", Payment: "+SUM(A1:A2)"},
		{Customer: "-2+3", Payment: "@HYPERLINK"},
		{Customer: " \t=1+1", Payment: "現金"},
		{Customer: "客戶, \"甲\"\n維修", Payment: "現金"},
	}
	data, err := encodeReportExportCSV(rows, false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		t.Fatalf("CSV missing UTF-8 BOM: %x", data[:min(len(data), 3)])
	}

	reader := csv.NewReader(bytes.NewReader(data[3:]))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("read CSV: %v", err)
	}
	if len(records) != 5 {
		t.Fatalf("got %d records, want header and 4 rows", len(records))
	}
	if got, want := records[1][1], "'=1+1"; got != want {
		t.Errorf("formula-like customer = %q, want %q", got, want)
	}
	if got, want := records[1][6], "'+SUM(A1:A2)"; got != want {
		t.Errorf("formula-like payment = %q, want %q", got, want)
	}
	if got, want := records[2][1], "'-2+3"; got != want {
		t.Errorf("formula-like customer = %q, want %q", got, want)
	}
	if got, want := records[2][6], "'@HYPERLINK"; got != want {
		t.Errorf("formula-like payment = %q, want %q", got, want)
	}
	if got, want := records[3][1], "' \t=1+1"; got != want {
		t.Errorf("formula after leading whitespace = %q, want %q", got, want)
	}
	if got, want := records[4][1], "客戶, \"甲\"\n維修"; got != want {
		t.Errorf("special customer text = %q, want %q", got, want)
	}
}

func TestEncodeReportExportCSVHidesCostForNonOwner(t *testing.T) {
	data, err := encodeReportExportCSV([]reportExportRow{{PartsCost: 98765}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "零件成本") || strings.Contains(string(data), "98765") {
		t.Fatalf("non-owner CSV exposed cost: %q", data)
	}
}

func TestReportsExportRoleAndDownloadHeaders(t *testing.T) {
	setupReportsExportTestDB(t)

	technician := httptest.NewRecorder()
	ReportsExport(technician, httptest.NewRequest(http.MethodGet, "/reports/export?from=2026-01-01&to=2026-01-31", nil),
		model.User{Role: "technician"})
	if technician.Code != http.StatusForbidden {
		t.Fatalf("technician export status = %d, want %d", technician.Code, http.StatusForbidden)
	}
	if technician.Body.Len() != 0 && strings.Contains(technician.Body.String(), "零件成本") {
		t.Fatalf("denied response disclosed cost: %q", technician.Body.String())
	}

	owner := httptest.NewRecorder()
	ReportsExport(owner, httptest.NewRequest(http.MethodGet, "/reports/export?from=2026-01-01&to=2026-01-31", nil),
		model.User{Role: "owner"})
	if owner.Code != http.StatusOK {
		t.Fatalf("owner export status = %d, want %d: %s", owner.Code, http.StatusOK, owner.Body.String())
	}
	if got := owner.Header().Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got, want := owner.Header().Get("Content-Disposition"),
		`attachment; filename="work_orders_2026-01-01_2026-01-31.csv"`; got != want {
		t.Errorf("Content-Disposition = %q, want %q", got, want)
	}
	if !bytes.HasPrefix(owner.Body.Bytes(), []byte{0xef, 0xbb, 0xbf}) {
		t.Fatal("owner CSV missing BOM")
	}

	records, err := csv.NewReader(bytes.NewReader(owner.Body.Bytes()[3:])).ReadAll()
	if err != nil {
		t.Fatalf("read owner CSV: %v", err)
	}
	if got, want := records[0], []string{"工單編號", "客戶", "日期", "工資收入", "零件收入", "外包收入", "零件成本", "支付方式", "折扣", "總計"}; !reflect.DeepEqual(got, want) {
		t.Errorf("owner columns = %#v, want %#v", got, want)
	}
	if got, want := records[1], []string{"7", "客戶, \"甲\"\n丙", "2026-01-02", "20.00", "50.00", "30.00", "16.00", "現金、信用卡", "5.00", "100.00"}; !reflect.DeepEqual(got, want) {
		t.Errorf("owner row = %#v, want %#v", got, want)
	}

	counter := httptest.NewRecorder()
	ReportsExport(counter, httptest.NewRequest(http.MethodGet, "/reports/export?from=2026-01-01&to=2026-01-31", nil),
		model.User{Role: "counter"})
	if counter.Code != http.StatusOK {
		t.Fatalf("counter export status = %d, want %d: %s", counter.Code, http.StatusOK, counter.Body.String())
	}
	counterRecords, err := csv.NewReader(bytes.NewReader(counter.Body.Bytes()[3:])).ReadAll()
	if err != nil {
		t.Fatalf("read counter CSV: %v", err)
	}
	if got, want := counterRecords[0], []string{"工單編號", "客戶", "日期", "工資收入", "零件收入", "外包收入", "支付方式", "折扣", "總計"}; !reflect.DeepEqual(got, want) {
		t.Errorf("counter columns = %#v, want %#v", got, want)
	}
	if strings.Contains(counter.Body.String(), "零件成本") || strings.Contains(counter.Body.String(), "16.00") {
		t.Fatalf("counter CSV exposed cost: %q", counter.Body.String())
	}
}

func TestReportsExportRejectsUnauthenticatedRequest(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/reports/export", nil)
	AllStaff(ReportsExport).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusFound {
		t.Fatalf("unauthenticated export status = %d, want %d", recorder.Code, http.StatusFound)
	}
	if got := recorder.Header().Get("Location"); got != "/login" {
		t.Fatalf("redirect location = %q, want /login", got)
	}
}

func TestAnalyticsExportRejectsNonOwnerCostDownload(t *testing.T) {
	setupStaffActiveTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO users (id, username, password, role, name, active)
		VALUES (1, 'counter1', 'x', 'counter', '櫃檯', 1)`); err != nil {
		t.Fatal(err)
	}
	token, err := session.Start(1)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/analytics/export", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	recorder := httptest.NewRecorder()
	AnalyticsExport(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("counter analytics export status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	if strings.Contains(recorder.Body.String(), "零件成本") {
		t.Fatalf("denied analytics export disclosed cost: %q", recorder.Body.String())
	}
}

func setupReportsExportTestDB(t *testing.T) {
	t.Helper()
	d, err := openTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	db.DB = d
	statements := []string{
		`CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT, deleted_at TEXT)`,
		`CREATE VIEW active_customers AS SELECT * FROM customers WHERE deleted_at IS NULL`,
		`CREATE TABLE work_orders (
			id INTEGER PRIMARY KEY, customer_id INTEGER, status TEXT, created_at TEXT,
			discount_amount REAL, total_amount REAL, deleted_at TEXT
		)`,
		`CREATE VIEW active_work_orders AS SELECT * FROM work_orders WHERE deleted_at IS NULL`,
		`CREATE TABLE work_order_items (
			id INTEGER PRIMARY KEY, work_order_id INTEGER, item_type TEXT,
			inventory_item_id INTEGER, quantity INTEGER, subtotal REAL
		)`,
		`CREATE TABLE inventory_items (id INTEGER PRIMARY KEY, cost_price REAL, deleted_at TEXT)`,
		`CREATE VIEW active_inventory_items AS SELECT * FROM inventory_items WHERE deleted_at IS NULL`,
		`CREATE TABLE receivables (
			id INTEGER PRIMARY KEY, work_order_id INTEGER, voided INTEGER,
			cash REAL, card REAL, check_amount REAL, transfer_amount REAL
		)`,
		`CREATE TABLE receivable_payments (receivable_id INTEGER, work_order_id INTEGER)`,
		`INSERT INTO customers (id, name) VALUES (1, '客戶, "甲"' || char(10) || '丙')`,
		`INSERT INTO work_orders (id, customer_id, status, created_at, discount_amount, total_amount)
			VALUES (7, 1, 'paid', '2026-01-02 12:00:00', 5, 100)`,
		`INSERT INTO inventory_items (id, cost_price) VALUES (2, 8)`,
		`INSERT INTO work_order_items (work_order_id, item_type, inventory_item_id, quantity, subtotal)
			VALUES (7, 'labor', NULL, 1, 20), (7, 'part', 2, 2, 50), (7, 'subcontract', NULL, 1, 30)`,
		`INSERT INTO receivables (id, work_order_id, voided, cash, card, check_amount, transfer_amount)
			VALUES (3, 7, 0, 25, 75, 0, 0)`,
	}
	for _, statement := range statements {
		if _, err := d.Exec(statement); err != nil {
			t.Fatalf("setup report export database: %v", err)
		}
	}
}
