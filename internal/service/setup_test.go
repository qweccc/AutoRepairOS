package service

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// bgCtx is the shared background context for service tests.
var bgCtx = context.Background()

// setupTestDB builds a unified in-memory DB for all Service tests.
// Service uses database/sql (modernc sqlite driver), so seeding is done with
// raw DDL/DML against that *sql.DB instead of an ORM layer.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := strings.ReplaceAll(t.Name(), "/", "_")
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	statements := []string{
		`CREATE TABLE customers (id INTEGER PRIMARY KEY, total_spent REAL DEFAULT 0, unpaid_amount REAL DEFAULT 0)`,
		`CREATE VIEW IF NOT EXISTS active_customers AS SELECT * FROM customers`,
		`CREATE TABLE vehicles (id INTEGER PRIMARY KEY, customer_id INTEGER)`,
		`CREATE VIEW IF NOT EXISTS active_vehicles AS SELECT * FROM vehicles`,
		`CREATE TABLE work_orders (
			id INTEGER PRIMARY KEY AUTOINCREMENT, customer_id INTEGER NOT NULL, vehicle_id INTEGER,
			status TEXT NOT NULL, total_amount REAL DEFAULT 0, updated_at TEXT, deleted_at TEXT,
			current_mileage INTEGER, customer_complaint TEXT, approval_status TEXT,
			original_estimate_id INTEGER, quoted_total_amount REAL, quoted_discount_amount REAL)`,
		`CREATE VIEW IF NOT EXISTS active_work_orders AS SELECT * FROM work_orders WHERE deleted_at IS NULL`,
		`CREATE TABLE work_order_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT, work_order_id INTEGER NOT NULL, item_type TEXT NOT NULL,
			inventory_item_id INTEGER, quantity INTEGER NOT NULL, description TEXT, unit_price REAL,
			subtotal REAL, primary_technician_id INTEGER, assistant_technician_id INTEGER, photo_url TEXT)`,
		`CREATE TABLE inventory_items (id INTEGER PRIMARY KEY, stock_quantity INTEGER DEFAULT 0, reserved_quantity INTEGER DEFAULT 0)`,
		`CREATE TABLE inventory_movements (
			id INTEGER PRIMARY KEY, inventory_item_id INTEGER NOT NULL, delta INTEGER NOT NULL,
			before_quantity INTEGER DEFAULT 0, after_quantity INTEGER DEFAULT 0,
			reason TEXT DEFAULT '', source TEXT NOT NULL, work_order_id INTEGER, created_by INTEGER)`,
		`CREATE TABLE receivables (
			id INTEGER PRIMARY KEY, voucher_no TEXT NOT NULL, customer_id INTEGER NOT NULL,
			vehicle_id INTEGER, work_order_id INTEGER, pay_date TEXT, detail_total REAL,
			real_total REAL, discount REAL, tax REAL, pay_total REAL, cash REAL, card REAL,
			check_amount REAL, transfer_amount REAL DEFAULT 0, transfer_reference TEXT DEFAULT '',
			card_no TEXT, check_no TEXT, check_date TEXT, bank TEXT,
			notes TEXT, source TEXT, created_by INTEGER, voided INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE estimates (
			id INTEGER PRIMARY KEY AUTOINCREMENT, customer_id INTEGER, vehicle_id INTEGER,
			current_mileage INTEGER, customer_complaint TEXT, discount_amount REAL DEFAULT 0,
			total_amount REAL DEFAULT 0, status TEXT DEFAULT 'draft',
			created_by INTEGER, converted_work_order_id INTEGER)`,
		`CREATE TABLE estimate_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT, estimate_id INTEGER, item_type TEXT,
			inventory_item_id INTEGER, description TEXT, quantity INTEGER, unit_price REAL,
			subtotal REAL, primary_technician_id INTEGER, assistant_technician_id INTEGER, photo_url TEXT)`,
		// Seed data: Customer 1 / Vehicle 7 / WorkOrder 10 (completed, 125) / inventory item 3
		`INSERT INTO customers (id, total_spent, unpaid_amount) VALUES (1, 12, 80), (2, 0, 0)`,
		`INSERT INTO vehicles (id, customer_id) VALUES (7, 1)`,
		`INSERT INTO work_orders (id, customer_id, vehicle_id, status, total_amount) VALUES (10, 1, 7, 'completed', 125)`,
		`INSERT INTO inventory_items (id, stock_quantity, reserved_quantity) VALUES (3, 8, 4)`,
		`INSERT INTO work_order_items (id, work_order_id, item_type, inventory_item_id, quantity, description, unit_price, subtotal)
			VALUES (1, 10, 'part', 3, 2, 'part', 25, 50), (2, 10, 'labor', NULL, 1, 'labor', 75, 75)`,
		`INSERT INTO estimates (id, customer_id, vehicle_id, current_mileage, customer_complaint, status, created_by)
			VALUES (1, 1, 7, 85000, 'seed complaint', 'draft', 3)`,
	}
	for _, s := range statements {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("setupTestDB: %v\nSQL: %s", err, s)
		}
	}
	return db
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...interface{}) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// --- fake sqlExecutor for generic-error / RowsAffected branches ---------

type fakeResult struct {
	affected int64
	err      error
}

func (r fakeResult) LastInsertId() (int64, error) { return 1, nil }
func (r fakeResult) RowsAffected() (int64, error) { return r.affected, r.err }

type execOutcome struct {
	res sql.Result
	err error
}

type queryOutcome struct {
	rows *sql.Rows
	err  error
}

type fakeExecutor struct {
	db       *sql.DB
	execs    []execOutcome
	queries  []queryOutcome
	rowQueue []*sql.Row
	rowIdx   int
	execIdx  int
	queryIdx int
}

func (f *fakeExecutor) ExecContext(_ context.Context, q string, args ...interface{}) (sql.Result, error) {
	if f.execIdx < len(f.execs) {
		o := f.execs[f.execIdx]
		f.execIdx++
		return o.res, o.err
	}
	if f.db != nil {
		return f.db.ExecContext(context.Background(), q, args...)
	}
	return fakeResult{affected: 1}, nil
}

func (f *fakeExecutor) QueryContext(_ context.Context, q string, args ...interface{}) (*sql.Rows, error) {
	if f.queryIdx < len(f.queries) {
		o := f.queries[f.queryIdx]
		f.queryIdx++
		return o.rows, o.err
	}
	if f.db != nil {
		return f.db.QueryContext(context.Background(), q, args...)
	}
	return nil, sql.ErrNoRows
}

func (f *fakeExecutor) QueryRowContext(_ context.Context, q string, args ...interface{}) *sql.Row {
	if f.rowIdx < len(f.rowQueue) {
		r := f.rowQueue[f.rowIdx]
		f.rowIdx++
		return r
	}
	if f.db != nil {
		return f.db.QueryRowContext(context.Background(), q, args...)
	}
	return f.db.QueryRow("SELECT 1 WHERE 0")
}

// errRow returns a *sql.Row whose Scan fails with a generic (non-ErrNoRows) error.
func errRow(db *sql.DB) *sql.Row {
	return db.QueryRow("SELECT * FROM nonexistent_table_for_error_branch")
}

// noRowsRow returns a *sql.Row whose Scan returns sql.ErrNoRows.
func noRowsRow(db *sql.DB) *sql.Row {
	return db.QueryRow("SELECT 1 WHERE 0")
}
