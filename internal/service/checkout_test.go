package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"
)

func newCheckoutTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	statements := []string{
		`CREATE TABLE customers (
			id INTEGER PRIMARY KEY, total_spent REAL DEFAULT 0, unpaid_amount REAL DEFAULT 0
		)`,
		`CREATE TABLE work_orders (
			id INTEGER PRIMARY KEY, customer_id INTEGER NOT NULL, vehicle_id INTEGER,
			status TEXT NOT NULL, total_amount REAL DEFAULT 0, updated_at TEXT,
			deleted_at TEXT
		)`,
		`CREATE VIEW IF NOT EXISTS active_work_orders AS SELECT * FROM work_orders WHERE deleted_at IS NULL`,
		`CREATE TABLE work_order_items (
			id INTEGER PRIMARY KEY, work_order_id INTEGER NOT NULL, item_type TEXT NOT NULL,
			inventory_item_id INTEGER, quantity INTEGER NOT NULL
		)`,
		`CREATE TABLE inventory_items (
			id INTEGER PRIMARY KEY, stock_quantity INTEGER DEFAULT 0,
			reserved_quantity INTEGER DEFAULT 0
		)`,
		`CREATE TABLE inventory_movements (
			id INTEGER PRIMARY KEY, inventory_item_id INTEGER NOT NULL, delta INTEGER NOT NULL,
			before_quantity INTEGER DEFAULT 0, after_quantity INTEGER DEFAULT 0,
			reason TEXT DEFAULT '', source TEXT NOT NULL, work_order_id INTEGER, created_by INTEGER
		)`,
		`CREATE TABLE receivables (
			id INTEGER PRIMARY KEY, voucher_no TEXT NOT NULL, customer_id INTEGER NOT NULL,
			vehicle_id INTEGER, work_order_id INTEGER, pay_date TEXT, detail_total REAL,
			real_total REAL, discount REAL, tax REAL, pay_total REAL, cash REAL, card REAL,
			check_amount REAL, transfer_amount REAL DEFAULT 0, transfer_reference TEXT DEFAULT '',
			card_no TEXT, check_no TEXT, check_date TEXT, bank TEXT,
			notes TEXT, source TEXT, created_by INTEGER, voided INTEGER NOT NULL DEFAULT 0
		)`,
		`INSERT INTO customers (id, total_spent, unpaid_amount) VALUES (1, 12, 80)`,
		`INSERT INTO work_orders (id, customer_id, vehicle_id, status, total_amount)
			VALUES (10, 1, 7, 'completed', 125)`,
		`INSERT INTO inventory_items (id, stock_quantity, reserved_quantity) VALUES (3, 8, 4)`,
		`INSERT INTO work_order_items (id, work_order_id, item_type, inventory_item_id, quantity)
			VALUES (1, 10, 'part', 3, 2)`,
		`INSERT INTO work_order_items (id, work_order_id, item_type, inventory_item_id, quantity)
			VALUES (2, 10, 'labor', NULL, 1)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("setup database: %v\nSQL: %s", err, statement)
		}
	}
	return db
}

func TestCheckoutSuccessAndIdempotency(t *testing.T) {
	db := newCheckoutTestDB(t)
	svc := New(db)

	for i := 0; i < 2; i++ {
		if err := svc.Checkout(context.Background(), 10, 21); err != nil {
			t.Fatalf("Checkout attempt %d: %v", i+1, err)
		}
	}

	var status string
	var stock, reserved, movementCount int
	var delta, before, after int
	if err := db.QueryRow(`SELECT status FROM work_orders WHERE id=10`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT stock_quantity, reserved_quantity FROM inventory_items WHERE id=3`).
		Scan(&stock, &reserved); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(MAX(delta),0),
		COALESCE(MAX(before_quantity),0), COALESCE(MAX(after_quantity),0)
		FROM inventory_movements`).Scan(&movementCount, &delta, &before, &after); err != nil {
		t.Fatal(err)
	}
	if status != "paid" || stock != 6 || reserved != 2 || movementCount != 1 || delta != -2 || before != 8 || after != 6 {
		t.Fatalf("checkout state: status=%q stock=%d reserved=%d movements=%d (%d,%d,%d)",
			status, stock, reserved, movementCount, delta, before, after)
	}
}

func TestCheckoutRollsBackWhenMovementFails(t *testing.T) {
	db := newCheckoutTestDB(t)
	if _, err := db.Exec(`CREATE TRIGGER reject_checkout_movement
		BEFORE INSERT ON inventory_movements
		BEGIN SELECT RAISE(ABORT, 'movement rejected'); END`); err != nil {
		t.Fatal(err)
	}

	err := New(db).Checkout(context.Background(), 10, 21)
	if err == nil {
		t.Fatal("Checkout succeeded despite movement insert failure")
	}
	assertUnchangedCheckoutState(t, db)
}

func TestAssociateReceiptChecksOutAtomically(t *testing.T) {
	db := newCheckoutTestDB(t)
	input := ReceiptInput{
		VoucherNo: "RCV-001", CustomerID: 1, WorkOrderID: 10, PayDate: "2026-10-16",
		PayTotal: 125, TransferAmount: 125, TransferReference: "TX-001", CreatedBy: 21,
	}
	if err := New(db).AssociateReceipt(context.Background(), input); err != nil {
		t.Fatalf("AssociateReceipt: %v", err)
	}
	var receipts int
	if err := db.QueryRow(`SELECT COUNT(*) FROM receivables WHERE voucher_no='RCV-001'`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	var status string
	var stock, reserved int
	if err := db.QueryRow(`SELECT status FROM work_orders WHERE id=10`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT stock_quantity, reserved_quantity FROM inventory_items WHERE id=3`).
		Scan(&stock, &reserved); err != nil {
		t.Fatal(err)
	}
	if receipts != 1 || status != "paid" || stock != 6 || reserved != 2 {
		t.Fatalf("associated receipt state: receipts=%d status=%q stock=%d reserved=%d",
			receipts, status, stock, reserved)
	}
}

func TestCreateReceiptIsIdempotentForRepeatedVoucher(t *testing.T) {
	db := newCheckoutTestDB(t)
	input := ReceiptInput{
		VoucherNo: "RCV-RETRY", CustomerID: 1, WorkOrderID: 10, PayDate: "2026-10-16",
		PayTotal: 125, Cash: 125, CreatedBy: 21,
	}
	svc := New(db)
	start := make(chan struct{})
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			errs <- svc.CreateReceipt(context.Background(), input)
		}()
	}
	close(start)
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent CreateReceipt attempt: %v", err)
		}
	}
	var receipts, movements int
	var stock int
	if err := db.QueryRow(`SELECT COUNT(*) FROM receivables WHERE voucher_no='RCV-RETRY'`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM inventory_movements`).Scan(&movements); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT stock_quantity FROM inventory_items WHERE id=3`).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if receipts != 1 || movements != 1 || stock != 6 {
		t.Fatalf("duplicate receipt state: receipts=%d movements=%d stock=%d",
			receipts, movements, stock)
	}
}

func TestCreateReceiptFailureDoesNotCheckout(t *testing.T) {
	db := newCheckoutTestDB(t)
	if _, err := db.Exec(`CREATE TRIGGER reject_receipt_insert
		BEFORE INSERT ON receivables
		BEGIN SELECT RAISE(ABORT, 'receipt rejected'); END`); err != nil {
		t.Fatal(err)
	}
	err := New(db).CreateReceipt(context.Background(), ReceiptInput{
		VoucherNo: "RCV-FAIL", CustomerID: 1, WorkOrderID: 10, PayDate: "2026-10-16",
		PayTotal: 125, Cash: 125,
	})
	if err == nil {
		t.Fatal("CreateReceipt succeeded despite insert failure")
	}
	assertUnchangedCheckoutState(t, db)
}

func TestAssociateReceiptRollsBackReceiptAndCheckout(t *testing.T) {
	db := newCheckoutTestDB(t)
	if _, err := db.Exec(`CREATE TRIGGER reject_checkout_movement
		BEFORE INSERT ON inventory_movements
		BEGIN SELECT RAISE(ABORT, 'movement rejected'); END`); err != nil {
		t.Fatal(err)
	}

	err := New(db).AssociateReceipt(context.Background(), ReceiptInput{
		VoucherNo: "RCV-002", CustomerID: 1, WorkOrderID: 10, PayDate: "2026-10-16",
		PayTotal: 125, Cash: 125,
	})
	if err == nil {
		t.Fatal("AssociateReceipt succeeded despite checkout failure")
	}
	assertUnchangedCheckoutState(t, db)
	var receipts int
	if err := db.QueryRow(`SELECT COUNT(*) FROM receivables`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 0 {
		t.Fatalf("receipt count after rollback = %d, want 0", receipts)
	}
}

func TestUpdateReceiptAndCheckoutRollBackTogether(t *testing.T) {
	db := newCheckoutTestDB(t)
	if _, err := db.Exec(`INSERT INTO receivables
		(id, voucher_no, customer_id, work_order_id, pay_date, pay_total, cash, source)
		VALUES (20, 'RCV-OLD', 1, 10, '2026-10-16', 100, 100, 'manual')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER reject_checkout_movement
		BEFORE INSERT ON inventory_movements
		BEGIN SELECT RAISE(ABORT, 'movement rejected'); END`); err != nil {
		t.Fatal(err)
	}
	err := New(db).UpdateReceipt(context.Background(), 20, ReceiptInput{
		VoucherNo: "RCV-UPDATED", PayDate: "2026-10-17", PayTotal: 125, Cash: 125,
	})
	if err == nil {
		t.Fatal("UpdateReceipt succeeded despite checkout failure")
	}
	assertUnchangedCheckoutState(t, db)
	var voucher string
	var amount float64
	if err := db.QueryRow(`SELECT voucher_no, pay_total FROM receivables WHERE id=20`).
		Scan(&voucher, &amount); err != nil {
		t.Fatal(err)
	}
	if voucher != "RCV-OLD" || amount != 100 {
		t.Fatalf("receipt update was not rolled back: voucher=%q amount=%v", voucher, amount)
	}
}

func TestPartialReceiptThenFullTransferChecksOutOnce(t *testing.T) {
	db := newCheckoutTestDB(t)
	svc := New(db)
	if err := svc.CreateReceipt(context.Background(), ReceiptInput{
		VoucherNo: "RCV-PART", CustomerID: 1, WorkOrderID: 10, PayDate: "2026-10-16",
		PayTotal: 60, Cash: 60, CreatedBy: 21,
	}); err != nil {
		t.Fatalf("create partial receipt: %v", err)
	}
	var status string
	var stock, movements int
	var spent float64
	if err := db.QueryRow(`SELECT status FROM work_orders WHERE id=10`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT stock_quantity FROM inventory_items WHERE id=3`).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM inventory_movements`).Scan(&movements); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT total_spent FROM customers WHERE id=1`).Scan(&spent); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || stock != 8 || movements != 0 {
		t.Fatalf("partial receipt state: status=%q stock=%d movements=%d",
			status, stock, movements)
	}

	if err := svc.CreateReceipt(context.Background(), ReceiptInput{
		VoucherNo: "RCV-TRANSFER", CustomerID: 1, WorkOrderID: 10, PayDate: "2026-10-16",
		PayTotal: 65, TransferAmount: 65, TransferReference: "BANK-12345", CreatedBy: 21,
	}); err != nil {
		t.Fatalf("create final transfer receipt: %v", err)
	}
	var transferAmount float64
	var transferReference string
	if err := db.QueryRow(`SELECT transfer_amount, transfer_reference FROM receivables
		WHERE voucher_no='RCV-TRANSFER'`).Scan(&transferAmount, &transferReference); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status FROM work_orders WHERE id=10`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT stock_quantity FROM inventory_items WHERE id=3`).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM inventory_movements`).Scan(&movements); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT total_spent FROM customers WHERE id=1`).Scan(&spent); err != nil {
		t.Fatal(err)
	}
	if status != "paid" || stock != 6 || movements != 1 || transferAmount != 65 || transferReference != "BANK-12345" {
		t.Fatalf("full transfer state: status=%q stock=%d movements=%d transfer=%v reference=%q",
			status, stock, movements, transferAmount, transferReference)
	}
}

func TestVoidedPartialReceiptDoesNotCheckoutAndAllowsFullReplacement(t *testing.T) {
	db := newCheckoutTestDB(t)
	svc := New(db)
	if err := svc.CreateReceipt(context.Background(), ReceiptInput{
		VoucherNo: "RCV-VOID", CustomerID: 1, WorkOrderID: 10, PayDate: "2026-10-16",
		PayTotal: 60, TransferAmount: 60, TransferReference: "VOID-TX", CreatedBy: 21,
	}); err != nil {
		t.Fatalf("create receipt to void: %v", err)
	}
	var receiptID int64
	if err := db.QueryRow(`SELECT id FROM receivables WHERE voucher_no='RCV-VOID'`).Scan(&receiptID); err != nil {
		t.Fatal(err)
	}
	if err := svc.VoidReceipt(context.Background(), receiptID); err != nil {
		t.Fatalf("void partial receipt: %v", err)
	}
	if err := svc.CreateReceipt(context.Background(), ReceiptInput{
		VoucherNo: "RCV-REPLACEMENT", CustomerID: 1, WorkOrderID: 10, PayDate: "2026-10-16",
		PayTotal: 125, TransferAmount: 125, TransferReference: "VALID-TX", CreatedBy: 21,
	}); err != nil {
		t.Fatalf("create replacement full receipt: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM receivables WHERE voucher_no='RCV-REPLACEMENT'`).Scan(&receiptID); err != nil {
		t.Fatal(err)
	}
	if err := svc.VoidReceipt(context.Background(), receiptID); err != nil {
		t.Fatalf("void completed receipt: %v", err)
	}
	if err := svc.CreateReceipt(context.Background(), ReceiptInput{
		VoucherNo: "RCV-REPLACEMENT-2", CustomerID: 1, WorkOrderID: 10, PayDate: "2026-10-16",
		PayTotal: 125, TransferAmount: 125, TransferReference: "VALID-TX-2", CreatedBy: 21,
	}); err != nil {
		t.Fatalf("create replacement after voided checkout: %v", err)
	}
	var status string
	var stock, movements int
	var spent float64
	if err := db.QueryRow(`SELECT status FROM work_orders WHERE id=10`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT stock_quantity FROM inventory_items WHERE id=3`).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM inventory_movements`).Scan(&movements); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT total_spent FROM customers WHERE id=1`).Scan(&spent); err != nil {
		t.Fatal(err)
	}
	if status != "paid" || stock != 6 || movements != 3 {
		t.Fatalf("void and replacement state: status=%q stock=%d movements=%d",
			status, stock, movements)
	}
}

func TestUpdatingPartialReceiptToFullChecksOutOnlyOnce(t *testing.T) {
	db := newCheckoutTestDB(t)
	svc := New(db)
	input := ReceiptInput{
		VoucherNo: "RCV-CORRECT", CustomerID: 1, WorkOrderID: 10, PayDate: "2026-10-16",
		PayTotal: 60, Cash: 60, CreatedBy: 21,
	}
	if err := svc.CreateReceipt(context.Background(), input); err != nil {
		t.Fatalf("create partial receipt: %v", err)
	}
	input.PayTotal = 125
	input.Cash = 0
	input.TransferAmount = 125
	input.TransferReference = "CORRECTED-TX"
	if err := svc.UpdateReceipt(context.Background(), 1, input); err != nil {
		t.Fatalf("correct receipt: %v", err)
	}
	if err := svc.UpdateReceipt(context.Background(), 1, input); err != nil {
		t.Fatalf("repeat corrected receipt update: %v", err)
	}
	var status string
	var stock, movements int
	var spent, transferAmount float64
	if err := db.QueryRow(`SELECT status FROM work_orders WHERE id=10`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT stock_quantity FROM inventory_items WHERE id=3`).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM inventory_movements`).Scan(&movements); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT total_spent FROM customers WHERE id=1`).Scan(&spent); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT transfer_amount FROM receivables WHERE id=1`).Scan(&transferAmount); err != nil {
		t.Fatal(err)
	}
	if status != "paid" || stock != 6 || movements != 1 || transferAmount != 125 {
		t.Fatalf("corrected receipt state: status=%q stock=%d movements=%d transfer=%v",
			status, stock, movements, transferAmount)
	}
}

func TestCheckoutMissingWorkOrder(t *testing.T) {
	db := newCheckoutTestDB(t)
	err := New(db).Checkout(context.Background(), 999, 21)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Checkout error = %v, want ErrNotFound", err)
	}
}

func assertUnchangedCheckoutState(t *testing.T, db *sql.DB) {
	t.Helper()
	var status string
	var stock, reserved, movements int
	var spent, unpaid float64
	if err := db.QueryRow(`SELECT status FROM work_orders WHERE id=10`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT stock_quantity, reserved_quantity FROM inventory_items WHERE id=3`).
		Scan(&stock, &reserved); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT total_spent, unpaid_amount FROM customers WHERE id=1`).
		Scan(&spent, &unpaid); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM inventory_movements`).Scan(&movements); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || stock != 8 || reserved != 4 || spent != 12 || unpaid != 80 || movements != 0 {
		t.Fatalf("state not rolled back: status=%q stock=%d reserved=%d spent=%v unpaid=%v movements=%d",
			status, stock, reserved, spent, unpaid, movements)
	}
}
