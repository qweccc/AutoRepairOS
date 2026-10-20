package handler

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"autorepairos/internal/db"
)

func setupAPTestDB(t *testing.T) {
	t.Helper()
	setupPurchaseTestDB(t)
	addAPTestSchema(t)
}

func addAPTestSchema(t *testing.T) {
	t.Helper()
	if _, err := db.DB.Exec(`ALTER TABLE suppliers ADD COLUMN payment_terms TEXT DEFAULT ''`); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE ap_payables (
			id INTEGER PRIMARY KEY AUTOINCREMENT, source_type TEXT NOT NULL, source_id INTEGER NOT NULL,
			supplier_id INTEGER NOT NULL, amount REAL NOT NULL, paid_amount REAL NOT NULL DEFAULT 0,
			balance REAL NOT NULL, due_date TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'open',
			created_at TEXT NOT NULL DEFAULT (datetime('now','localtime')), paid_at TEXT,
			UNIQUE(source_type, source_id))`,
		`CREATE TABLE ap_payments (
			id INTEGER PRIMARY KEY AUTOINCREMENT, payable_id INTEGER NOT NULL,
			amount REAL NOT NULL, paid_at TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now','localtime')))`,
	} {
		if _, err := db.DB.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReceivePurchaseOrderCreatesSinglePayableAndLocksSnapshot(t *testing.T) {
	setupAPTestDB(t)
	_, err := db.DB.Exec(`INSERT INTO suppliers (id,name,payment_terms) VALUES (1,'供應商','check30')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.DB.Exec(`INSERT INTO inventory_items (id,part_number,name,bin_location)
		VALUES (1,'PART-1','測試零件','A-1')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.DB.Exec(`INSERT INTO purchase_orders
		(id,supplier_id,order_date,status,total_amount,created_at) VALUES (1,1,'2026-10-01','draft',100,'2026-10-01')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.DB.Exec(`INSERT INTO purchase_order_items
		(id,order_id,inventory_item_id,description,quantity,unit_cost,subtotal)
		VALUES (1,1,1,'測試零件',2,50,100)`)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest("POST", "/purchase-orders/1/receive", nil)
	request.SetPathValue("id", "1")
	response := httptest.NewRecorder()
	ReceivePurchaseOrder(response, request)
	if response.Code != 302 {
		t.Fatalf("receive status = %d, want 302: %s", response.Code, response.Body.String())
	}

	var count int
	var amount, paid, balance float64
	var status, dueDate string
	if err := db.DB.QueryRow(`SELECT COUNT(*), COALESCE(MAX(amount),0), COALESCE(MAX(paid_amount),0),
		COALESCE(MAX(balance),0), COALESCE(MAX(status),''), COALESCE(MAX(due_date),'')
		FROM ap_payables WHERE source_type='purchase_receive' AND source_id=1`).
		Scan(&count, &amount, &paid, &balance, &status, &dueDate); err != nil {
		t.Fatal(err)
	}
	if count != 1 || amount != 100 || paid != 0 || balance != 100 || status != "open" {
		t.Fatalf("payable = count:%d amount:%.2f paid:%.2f balance:%.2f status:%q",
			count, amount, paid, balance, status)
	}
	expectedDue, err := apDueDate("check30", today())
	if err != nil {
		t.Fatal(err)
	}
	if dueDate != expectedDue {
		t.Fatalf("due date = %q, want %q", dueDate, expectedDue)
	}

	// Repeating receive is rejected by the source status guard and cannot duplicate the payable.
	response = httptest.NewRecorder()
	ReceivePurchaseOrder(response, request)
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM ap_payables WHERE source_type='purchase_receive' AND source_id=1`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("repeated receive created %d payables, want 1", count)
	}

	// The normal item edit path is locked after receipt; even an attempted delete leaves the AP snapshot intact.
	editRequest := httptest.NewRequest("DELETE", "/purchase-orders/1/items/1", nil)
	editRequest.SetPathValue("id", "1")
	editRequest.SetPathValue("itemId", "1")
	DeletePurchaseOrderItem(httptest.NewRecorder(), editRequest)
	if _, err := db.DB.Exec(`UPDATE purchase_orders SET total_amount=999 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.QueryRow(`SELECT amount FROM ap_payables WHERE source_type='purchase_receive' AND source_id=1`).Scan(&amount); err != nil {
		t.Fatal(err)
	}
	if amount != 100 {
		t.Fatalf("editing the received source changed AP amount to %.2f", amount)
	}
}

func TestCreateAPPayableIsIdempotent(t *testing.T) {
	setupAPTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO suppliers (id,name,payment_terms) VALUES (1,'供應商','monthly')`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := createAPPayable(tx, "subcontract", 7, 1, 120.50, "2026-10-20"); err != nil {
		t.Fatal(err)
	}
	if err := createAPPayable(tx, "subcontract", 7, 1, 999, "2026-11-01"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var count int
	var amount float64
	var dueDate string
	if err := db.DB.QueryRow(`SELECT COUNT(*), MAX(amount), MAX(due_date) FROM ap_payables
		WHERE source_type='subcontract' AND source_id=7`).Scan(&count, &amount, &dueDate); err != nil {
		t.Fatal(err)
	}
	if count != 1 || amount != 120.5 || dueDate != "2026-10-31" {
		t.Fatalf("idempotent AP row = count:%d amount:%.2f due:%s", count, amount, dueDate)
	}
}

func TestSubcontractCreationCreatesSnapshotAndPreventsFinancialEdits(t *testing.T) {
	setupAPTestDB(t)
	for _, stmt := range []string{
		`CREATE TABLE work_orders (id INTEGER PRIMARY KEY, deleted_at TEXT)`,
		`CREATE VIEW active_work_orders AS SELECT * FROM work_orders WHERE deleted_at IS NULL`,
		`CREATE TABLE subcontracts (
			id INTEGER PRIMARY KEY AUTOINCREMENT, work_order_id INTEGER NOT NULL, supplier_id INTEGER,
			description TEXT NOT NULL, cost_amount REAL DEFAULT 0, charge_amount REAL DEFAULT 0,
			status TEXT DEFAULT 'pending', expected_date TEXT, completed_date TEXT, notes TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP, updated_at TEXT DEFAULT CURRENT_TIMESTAMP, deleted_at TEXT)`,
		`INSERT INTO work_orders (id) VALUES (1)`,
		`INSERT INTO suppliers (id,name,payment_terms) VALUES (1,'外包廠商','check60')`,
	} {
		if _, err := db.DB.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	form := url.Values{
		"work_order_id": {"1"}, "supplier_id": {"1"}, "description": {"鈑金加工"},
		"cost_amount": {"2500.50"}, "charge_amount": {"3000"}, "status": {"pending"},
	}
	request := httptest.NewRequest(http.MethodPost, "/subcontracts", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	CreateSubcontract(response, request)
	if response.Code != http.StatusFound {
		t.Fatalf("create status = %d, want 302: %s", response.Code, response.Body.String())
	}
	var sourceID int64
	if err := db.DB.QueryRow(`SELECT id FROM subcontracts`).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	var count int
	var amount float64
	if err := db.DB.QueryRow(`SELECT COUNT(*),MAX(amount) FROM ap_payables
		WHERE source_type='subcontract' AND source_id=?`, sourceID).Scan(&count, &amount); err != nil {
		t.Fatal(err)
	}
	if count != 1 || amount != 2500.5 {
		t.Fatalf("subcontract AP = count:%d amount:%.2f", count, amount)
	}
	update := url.Values{
		"work_order_id": {"1"}, "supplier_id": {"1"}, "description": {"修改加工"},
		"cost_amount": {"2600"}, "charge_amount": {"3000"}, "status": {"pending"},
	}
	updateRequest := httptest.NewRequest(http.MethodPost, "/subcontracts/1", strings.NewReader(update.Encode()))
	updateRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	updateRequest.SetPathValue("id", "1")
	updateResponse := httptest.NewRecorder()
	UpdateSubcontract(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusBadRequest {
		t.Fatalf("financial source edit status = %d, want 400: %s", updateResponse.Code, updateResponse.Body.String())
	}
	if err := db.DB.QueryRow(`SELECT cost_amount FROM subcontracts WHERE id=1`).Scan(&amount); err != nil {
		t.Fatal(err)
	}
	if amount != 2500.5 {
		t.Fatalf("rejected subcontract edit changed cost to %.2f", amount)
	}
}

func TestAPPaymentsAccumulateAndRejectOverpayment(t *testing.T) {
	setupAPTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO ap_payables
		(id,source_type,source_id,supplier_id,amount,paid_amount,balance,status)
		VALUES (1,'subcontract',1,1,100,0,100,'open')`); err != nil {
		t.Fatal(err)
	}
	record := func(amount float64, paidAt string) error {
		tx, err := db.DB.Begin()
		if err != nil {
			return err
		}
		if err := applyAPPayment(tx, 1, amount, paidAt); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	}
	if err := record(35.25, "2026-10-20"); err != nil {
		t.Fatal(err)
	}
	var paid, balance float64
	var status string
	if err := db.DB.QueryRow(`SELECT paid_amount,balance,status FROM ap_payables WHERE id=1`).
		Scan(&paid, &balance, &status); err != nil {
		t.Fatal(err)
	}
	if paid != 35.25 || balance != 64.75 || status != "partial" {
		t.Fatalf("partial payment = paid %.2f balance %.2f status %q", paid, balance, status)
	}
	if err := record(64.75, "2026-10-21"); err != nil {
		t.Fatal(err)
	}
	var paidAt string
	if err := db.DB.QueryRow(`SELECT paid_amount,balance,status,paid_at FROM ap_payables WHERE id=1`).
		Scan(&paid, &balance, &status, &paidAt); err != nil {
		t.Fatal(err)
	}
	if paid != 100 || balance != 0 || status != "paid" || paidAt != "2026-10-21" {
		t.Fatalf("paid in full = paid %.2f balance %.2f status %q paid_at %q", paid, balance, status, paidAt)
	}
	if err := record(0.01, "2026-10-22"); !errors.Is(err, errAPPaymentRejected) {
		t.Fatalf("overpayment error = %v, want rejected", err)
	}
	var count int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM ap_payments WHERE payable_id=1`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("payment rows = %d, want 2", count)
	}
}

func TestVoidAPPayableOnlyBeforeAnyPayment(t *testing.T) {
	setupAPTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO ap_payables
		(id,source_type,source_id,supplier_id,amount,paid_amount,balance,status)
		VALUES (1,'subcontract',1,1,50,0,50,'open'),
			(2,'subcontract',2,1,50,10,40,'partial')`); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/ap/1/void", nil)
	request.SetPathValue("id", "1")
	response := httptest.NewRecorder()
	VoidAPPayable(response, request)
	if response.Code != http.StatusFound {
		t.Fatalf("void unpaid status = %d, want 302", response.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "/ap/2/void", nil)
	request.SetPathValue("id", "2")
	response = httptest.NewRecorder()
	VoidAPPayable(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("void partially paid status = %d, want 400", response.Code)
	}
	var status string
	if err := db.DB.QueryRow(`SELECT status FROM ap_payables WHERE id=2`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "partial" {
		t.Fatalf("partially paid AP status = %q, want partial", status)
	}
}

func TestAPAgeBucketsUseSuppliedReferenceDate(t *testing.T) {
	tests := []struct {
		due  string
		want string
		days int
	}{
		{"2026-10-20", "0–30", 0},
		{"2026-09-20", "0–30", 30},
		{"2026-09-19", "31–60", 31},
		{"2026-08-21", "31–60", 60},
		{"2026-08-20", "61–90", 61},
		{"2026-07-22", "61–90", 90},
		{"2026-07-21", "90+", 91},
		{"2026-10-21", "0–30", 0},
	}
	for _, test := range tests {
		gotBucket, gotDays, err := apAgeBucket(test.due, "", "2026-10-20")
		if err != nil {
			t.Fatal(err)
		}
		if gotBucket != test.want || gotDays != test.days {
			t.Errorf("due %s => %s/%d, want %s/%d", test.due, gotBucket, gotDays, test.want, test.days)
		}
	}
	if _, _, err := apAgeBucket("", "bad-date", "2026-10-20"); err == nil {
		t.Fatal("invalid fallback creation date should fail")
	}
}

func TestAPCSVTextNeutralizesFormulaPrefixes(t *testing.T) {
	for _, value := range []string{"=1+1", " +SUM(A1:A2)", "\t@cmd", "-4"} {
		if got := apCSVText(value); got == value || got[0] != '\'' {
			t.Errorf("formula value %q was not prefixed: %q", value, got)
		}
	}
	if got := apCSVText("供應商,股份有限公司"); got != "供應商,股份有限公司" {
		t.Errorf("safe text changed unexpectedly: %q", got)
	}
}

func TestAPDueDates(t *testing.T) {
	tests := []struct {
		terms string
		date  string
		want  string
	}{
		{"cash", "2026-10-20", "2026-10-20"},
		{"monthly", "2026-10-20", "2026-10-31"},
		{"monthly", "2026-02-18", "2026-02-28"},
		{"check30", "2026-10-20", "2026-11-19"},
		{"check60", "2026-10-20", "2026-12-19"},
	}
	for _, test := range tests {
		got, err := apDueDate(test.terms, test.date)
		if err != nil || got != test.want {
			t.Errorf("apDueDate(%q,%q) = %q, %v; want %q", test.terms, test.date, got, err, test.want)
		}
	}
}

func TestAPMigrationBackfillsSourcesIdempotently(t *testing.T) {
	d, err := openTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	db.DB = d
	for _, stmt := range []string{
		`CREATE TABLE suppliers (id INTEGER PRIMARY KEY, name TEXT, payment_terms TEXT)`,
		`CREATE TABLE purchase_orders (
			id INTEGER PRIMARY KEY, supplier_id INTEGER, order_date TEXT, status TEXT,
			total_amount REAL, created_at TEXT, received_at TEXT, deleted_at TEXT)`,
		`CREATE TABLE purchase_order_items (id INTEGER PRIMARY KEY, order_id INTEGER, subtotal REAL, deleted_at TEXT)`,
		`CREATE VIEW active_purchase_order_items AS SELECT * FROM purchase_order_items WHERE deleted_at IS NULL`,
		`CREATE TABLE subcontracts (
			id INTEGER PRIMARY KEY, supplier_id INTEGER, cost_amount REAL, status TEXT,
			created_at TEXT, deleted_at TEXT)`,
		`INSERT INTO suppliers VALUES (1,'材料商','monthly')`,
		`INSERT INTO purchase_orders VALUES (10,1,'2026-10-01','received',120,'2026-10-01','2026-10-20 09:30:00',NULL)`,
		`INSERT INTO purchase_order_items VALUES (1,10,120,NULL)`,
		`INSERT INTO subcontracts VALUES (20,1,35,'pending','2026-10-18 12:00:00',NULL)`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	migration, err := os.ReadFile(filepath.Join("..", "db", "migration", "041_ap_payables.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	runMigration := func() {
		t.Helper()
		for _, statement := range strings.Split(string(migration), ";") {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if _, err := d.Exec(statement); err != nil {
				t.Fatalf("apply AP migration statement: %v", err)
			}
		}
	}
	runMigration()
	runMigration()
	rows, err := d.Query(`SELECT source_type,source_id,amount,due_date FROM ap_payables ORDER BY source_type`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type apSource struct {
		sourceType string
		sourceID   int64
		amount     float64
		dueDate    string
	}
	var sources []apSource
	for rows.Next() {
		var source apSource
		if err := rows.Scan(&source.sourceType, &source.sourceID, &source.amount, &source.dueDate); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 {
		t.Fatalf("backfilled AP rows = %d, want 2", len(sources))
	}
	if sources[0] != (apSource{"purchase_receive", 10, 120, "2026-10-31"}) {
		t.Errorf("purchase AP backfill = %+v", sources[0])
	}
	if sources[1] != (apSource{"subcontract", 20, 35, "2026-10-31"}) {
		t.Errorf("subcontract AP backfill = %+v", sources[1])
	}
}

var _ db.TxExecutor = (*sql.Tx)(nil)
