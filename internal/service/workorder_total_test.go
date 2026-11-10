package service

import (
	"database/sql"
	"errors"
	"testing"
)

func totalOf(t *testing.T, db *sql.DB, id int64) float64 {
	t.Helper()
	var total float64
	if err := db.QueryRow(`SELECT COALESCE(total_amount,0) FROM work_orders WHERE id = ?`, id).Scan(&total); err != nil {
		t.Fatal(err)
	}
	return total
}

func TestRecalculateWorkOrderTotal_SumsSubtotals(t *testing.T) {
	db := setupTestDB(t)
	// Seed WO 10 already has items 50 + 75 = 125; corrupt the stored total first.
	mustExec(t, db, `UPDATE work_orders SET total_amount = 999 WHERE id = 10`)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := RecalculateWorkOrderTotal(bgCtx, tx, 10); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := totalOf(t, db, 10); got != 125 {
		t.Fatalf("expected 125, got %v", got)
	}
}

func TestRecalculateWorkOrderTotal_EmptyItemsZero(t *testing.T) {
	db := setupTestDB(t)
	mustExec(t, db, `INSERT INTO work_orders (id, customer_id, status, total_amount) VALUES (20, 1, 'in_progress', 777)`)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := RecalculateWorkOrderTotal(bgCtx, tx, 20); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := totalOf(t, db, 20); got != 0 {
		t.Fatalf("expected 0, got %v", got)
	}
}

func TestRecalculateWorkOrderTotal_NullSubtotalFallsBackToQtyTimesPrice(t *testing.T) {
	db := setupTestDB(t)
	mustExec(t, db, `INSERT INTO work_orders (id, customer_id, status, total_amount) VALUES (21, 1, 'in_progress', 0)`)
	// NULL subtotal with qty/price -> 2*10=20; NULL subtotal and NULL unit_price -> 0.
	mustExec(t, db, `INSERT INTO work_order_items (work_order_id, item_type, quantity, unit_price, subtotal) VALUES (21, 'labor', 2, 10, NULL), (21, 'labor', 3, NULL, NULL)`)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := RecalculateWorkOrderTotal(bgCtx, tx, 21); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := totalOf(t, db, 21); got != 20 {
		t.Fatalf("expected 20, got %v", got)
	}
}

func TestRecalculateWorkOrderTotal_PropagatesExecError(t *testing.T) {
	boom := errors.New("boom")
	fake := &fakeExecutor{execs: []execOutcome{{err: boom}}}
	if err := RecalculateWorkOrderTotal(bgCtx, fake, 10); !errors.Is(err, boom) {
		t.Fatalf("expected boom, got %v", err)
	}
	// Method form must propagate the same error.
	fake2 := &fakeExecutor{execs: []execOutcome{{err: boom}}}
	if err := New(nil).RecalculateWorkOrderTotal(bgCtx, fake2, 10); !errors.Is(err, boom) {
		t.Fatalf("method: expected boom, got %v", err)
	}
}

func TestRecalculateWorkOrderTotal_MethodForm(t *testing.T) {
	db := setupTestDB(t)
	mustExec(t, db, `UPDATE work_orders SET total_amount = 0 WHERE id = 10`)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := New(db).RecalculateWorkOrderTotal(bgCtx, tx, 10); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := totalOf(t, db, 10); got != 125 {
		t.Fatalf("expected 125, got %v", got)
	}
}
