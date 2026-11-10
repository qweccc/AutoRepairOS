package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"
)

func setupInventorySvcDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	for _, s := range []string{
		`CREATE TABLE inventory_items (id INTEGER PRIMARY KEY, name TEXT, category TEXT, stock_quantity INTEGER, reserved_quantity INTEGER, deleted_at TEXT)`,
		`CREATE TABLE inventory_movements (id INTEGER PRIMARY KEY, inventory_item_id INTEGER, delta INTEGER, before_quantity INTEGER, after_quantity INTEGER, reason TEXT, source TEXT, work_order_id INTEGER, created_by INTEGER)`,
		`CREATE TABLE work_order_items (id INTEGER PRIMARY KEY, work_order_id INTEGER, item_type TEXT, inventory_item_id INTEGER, quantity INTEGER)`,
		`INSERT INTO inventory_items (id, name, category, stock_quantity, reserved_quantity) VALUES (1,'煞車皮','制動',10,2),(2,'機油','潤滑',5,0),(3,'舊料','雜項',4,0)`,
		`UPDATE inventory_items SET deleted_at = '2026-01-01' WHERE id = 3`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("setup: %v\n%s", err, s)
		}
	}
	return db
}

func TestInventoryServiceCRUD(t *testing.T) {
	db := setupInventorySvcDB(t)
	svc := NewInventoryService(db)
	ctx := context.Background()

	items, err := svc.List(ctx)
	if err != nil || len(items) != 3 {
		t.Fatalf("List len=%d err=%v", len(items), err)
	}
	found, err := svc.Search(ctx, "煞車")
	if err != nil || len(found) != 1 {
		t.Fatalf("Search len=%d err=%v", len(found), err)
	}
	if n, err := svc.Search(ctx, "不存在"); err != nil || len(n) != 0 {
		t.Fatalf("Search empty len=%d err=%v", len(n), err)
	}
	id, err := svc.Create(ctx, "濾芯", "過濾", 7)
	if err != nil || id <= 0 {
		t.Fatalf("Create id=%d err=%v", id, err)
	}
	if err := svc.Update(ctx, id, "濾芯A", "過濾", 9, 1); err != nil {
		t.Fatal(err)
	}
	// delete referenced item -> error; unreferenced -> ok
	if _, err := db.Exec(`INSERT INTO work_order_items (work_order_id, item_type, inventory_item_id, quantity) VALUES (1,'part',1,1)`); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, 1); !errors.Is(err, ErrReferenceExists) {
		t.Fatalf("Delete referenced: %v", err)
	}
	if err := svc.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
}

func TestInventoryServiceAdjust(t *testing.T) {
	db := setupInventorySvcDB(t)
	svc := NewInventoryService(db)
	ctx := context.Background()

	if err := svc.ApplyStockAdjust(ctx, 999, 1, "test", "op1"); !errors.Is(err, ErrItemNotFound) {
		t.Fatalf("not found: %v", err)
	}
	if err := svc.ApplyStockAdjust(ctx, 1, 100, "test", "op1"); !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("insufficient: %v", err)
	}
	// success path: stock deducted and movement recorded with real columns
	if err := svc.ApplyStockAdjust(ctx, 1, 1, "test", "7"); err != nil {
		t.Fatalf("ApplyStockAdjust success: %v", err)
	}
	var stock int
	if err := db.QueryRow(`SELECT stock_quantity FROM inventory_items WHERE id=1`).Scan(&stock); err != nil || stock != 9 {
		t.Fatalf("stock=%d err=%v", stock, err)
	}
	var delta, before, after int
	var src string
	var createdBy int64
	if err := db.QueryRow(`SELECT delta, before_quantity, after_quantity, source, COALESCE(created_by,0) FROM inventory_movements WHERE inventory_item_id=1`).Scan(&delta, &before, &after, &src, &createdBy); err != nil {
		t.Fatal(err)
	}
	if delta != -1 || before != 10 || after != 9 || src != "test" || createdBy != 7 {
		t.Fatalf("movement delta=%d before=%d after=%d src=%q created_by=%d", delta, before, after, src, createdBy)
	}
	// non-numeric actor -> created_by NULL, still succeeds (rolls back? no: succeeds)
	if err := svc.ApplyStockAdjust(ctx, 1, 1, "test", "op1"); err != nil {
		t.Fatalf("non-numeric actor: %v", err)
	}
	if err := db.QueryRow(`SELECT stock_quantity FROM inventory_items WHERE id=1`).Scan(&stock); err != nil || stock != 8 {
		t.Fatalf("stock=%d err=%v", stock, err)
	}
}

func TestInventoryServiceAdjustTx(t *testing.T) {
	db := setupInventorySvcDB(t)
	svc := NewInventoryService(db)
	ctx := context.Background()

	// not found via fake executor
	if err := svc.ApplyStockAdjustTx(ctx, &fakeExecutor{db: db, rowQueue: []*sql.Row{noRowsRow(db)}}, 999, 1, "s", "a", false); !errors.Is(err, ErrItemNotFound) {
		t.Fatalf("not found: %v", err)
	}
	// generic query error
	if err := svc.ApplyStockAdjustTx(ctx, &fakeExecutor{db: db, rowQueue: []*sql.Row{errRow(db)}}, 1, 1, "s", "a", false); err == nil {
		t.Fatal("expected generic error")
	}
	// insufficient: stock 10 reserved 2 -> available 8 < 9
	okRow := db.QueryRow(`SELECT 10, 2`)
	if err := svc.ApplyStockAdjustTx(ctx, &fakeExecutor{db: db, rowQueue: []*sql.Row{okRow}}, 1, 9, "s", "a", false); !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("insufficient: %v", err)
	}
	// exec error on deduct
	fx := &fakeExecutor{db: db, rowQueue: []*sql.Row{db.QueryRow(`SELECT 10, 2`)}, execs: []execOutcome{{err: errors.New("deduct boom")}}}
	if err := svc.ApplyStockAdjustTx(ctx, fx, 1, 1, "s", "a", false); err == nil {
		t.Fatal("expected deduct error")
	}
	// success with reservation deduction: movement insert fails on simplified schema -> error, but deducts ran on fake (no real tx)
	fx2 := &fakeExecutor{db: db, rowQueue: []*sql.Row{db.QueryRow(`SELECT 10, 2`)}, execs: []execOutcome{{res: fakeResult{affected: 1}}, {res: fakeResult{affected: 1}}, {err: errors.New("movement boom")}}}
	if err := svc.ApplyStockAdjustTx(ctx, fx2, 1, 1, "s", "a", true); err == nil {
		t.Fatal("expected movement error")
	}
	// full success: all execs succeed (fake) -> nil, with reservation deduction
	fx3 := &fakeExecutor{db: db, rowQueue: []*sql.Row{db.QueryRow(`SELECT 10, 2`)}, execs: []execOutcome{{res: fakeResult{affected: 1}}, {res: fakeResult{affected: 1}}, {res: fakeResult{affected: 1}}}}
	if err := svc.ApplyStockAdjustTx(ctx, fx3, 1, 1, "s", "a", true); err != nil {
		t.Fatalf("success path: %v", err)
	}
}

func TestInventoryServiceScanErrors(t *testing.T) {
	db := setupInventorySvcDB(t)
	svc := NewInventoryService(db)
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO inventory_items (id, name, category, stock_quantity, reserved_quantity) VALUES (9,'壞','x','abc',0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.List(ctx); err == nil {
		t.Fatal("List scan expected error")
	}
	if _, err := svc.Search(ctx, "壞"); err == nil {
		t.Fatal("Search scan expected error")
	}
}

func TestInventoryServiceDBErrors(t *testing.T) {
	db := setupInventorySvcDB(t)
	svc := NewInventoryService(db)
	ctx := context.Background()
	// Drop tables to force query errors
	if _, err := db.Exec(`DROP TABLE inventory_items`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.List(ctx); err == nil {
		t.Fatal("List expected error")
	}
	if _, err := svc.Search(ctx, "x"); err == nil {
		t.Fatal("Search expected error")
	}
	if _, err := svc.Create(ctx, "n", "c", 1); err == nil {
		t.Fatal("Create expected error")
	}
	if err := svc.Update(ctx, 1, "n", "c", 1, 0); err == nil {
		t.Fatal("Update expected error")
	}
	if err := svc.Delete(ctx, 1); err == nil {
		t.Fatal("Delete expected error (work_order_items query on dropped inventory? count query still works)")
	}
	if err := svc.ApplyStockAdjust(ctx, 1, 1, "s", "a"); err == nil {
		t.Fatal("Adjust expected error")
	}
}

func TestInventoryServiceApplyStockDelta(t *testing.T) {
	for _, tc := range []struct {
		name       string
		itemID     int
		delta      int
		reason     string
		source     string
		actorID    string
		wantAfter  int
		wantStock  int
		wantErrIs  error
		wantShort  *StockShortfall
		wantMov    *struct{ delta, before, after int; reason, source string; createdBy sql.NullInt64 }
	}{
		{
			name: "deduct succeeds and ignores reserved", itemID: 1, delta: -3,
			reason: "領用", source: "manual", actorID: "7",
			wantAfter: 7, wantStock: 7,
			wantMov: &struct{ delta, before, after int; reason, source string; createdBy sql.NullInt64 }{-3, 10, 7, "領用", "manual", sql.NullInt64{Int64: 7, Valid: true}},
		},
		{
			name: "restock succeeds", itemID: 2, delta: 5,
			reason: "手動調庫", source: "manual", actorID: "9",
			wantAfter: 10, wantStock: 10,
			wantMov: &struct{ delta, before, after int; reason, source string; createdBy sql.NullInt64 }{5, 5, 10, "手動調庫", "manual", sql.NullInt64{Int64: 9, Valid: true}},
		},
		{
			name: "non-numeric actor stores NULL created_by", itemID: 1, delta: -1,
			reason: "盤點", source: "manual", actorID: "op1",
			wantAfter: 9, wantStock: 9,
			wantMov: &struct{ delta, before, after int; reason, source string; createdBy sql.NullInt64 }{-1, 10, 9, "盤點", "manual", sql.NullInt64{}},
		},
		{
			name: "zero actor stores NULL created_by", itemID: 1, delta: 1,
			reason: "x", source: "manual", actorID: "0",
			wantAfter: 11, wantStock: 11,
			wantMov: &struct{ delta, before, after int; reason, source string; createdBy sql.NullInt64 }{1, 10, 11, "x", "manual", sql.NullInt64{}},
		},
		{
			name: "deduct beyond stock is a shortfall with current qty", itemID: 2, delta: -9,
			reason: "x", source: "manual", actorID: "1",
			wantErrIs: ErrInsufficientStock, wantShort: &StockShortfall{Current: 5, Needed: 9},
		},
		{
			name: "missing item is not found", itemID: 999, delta: -1,
			reason: "x", source: "manual", actorID: "1",
			wantErrIs: ErrItemNotFound,
		},
		{
			name: "missing item restock is not found", itemID: 999, delta: 4,
			reason: "x", source: "manual", actorID: "1",
			wantErrIs: ErrItemNotFound,
		},
		{
			// mirrors the handler: the stock UPDATE has no deleted_at filter,
			// so a soft-deleted item can still be adjusted when stock suffices.
			name: "soft-deleted item within stock still adjusts", itemID: 3, delta: -2,
			reason: "x", source: "manual", actorID: "1",
			wantAfter: 2, wantStock: 2,
			wantMov: &struct{ delta, before, after int; reason, source string; createdBy sql.NullInt64 }{-2, 4, 2, "x", "manual", sql.NullInt64{Int64: 1, Valid: true}},
		},
		{
			// the existence probe filters deleted_at, so an oversized deduction
			// against a soft-deleted item reports not-found, not shortfall.
			name: "soft-deleted item beyond stock is not found", itemID: 3, delta: -99,
			reason: "x", source: "manual", actorID: "1",
			wantErrIs: ErrItemNotFound,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupInventorySvcDB(t)
			svc := NewInventoryService(db)
			after, err := svc.ApplyStockDelta(context.Background(), tc.itemID, tc.delta, tc.reason, tc.source, tc.actorID)
			if tc.wantErrIs != nil || tc.wantShort != nil {
				if err == nil {
					t.Fatalf("expected error, got after=%d", after)
				}
				if tc.wantShort != nil {
					var got *StockShortfall
					if !errors.As(err, &got) {
						t.Fatalf("expected StockShortfall, got %v", err)
					}
					if *got != *tc.wantShort {
						t.Fatalf("shortfall=%+v want %+v", got, tc.wantShort)
					}
					if !errors.Is(err, ErrInsufficientStock) {
						t.Fatalf("shortfall should match ErrInsufficientStock: %v", err)
					}
				} else if !errors.Is(err, tc.wantErrIs) {
					t.Fatalf("err=%v want %v", err, tc.wantErrIs)
				}
				// rejected adjustments must not move stock or write a movement
				var movs int
				if err := db.QueryRow(`SELECT COUNT(*) FROM inventory_movements WHERE inventory_item_id=?`, tc.itemID).Scan(&movs); err != nil || movs != 0 {
					t.Fatalf("movements=%d err=%v, want 0", movs, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if after != tc.wantAfter {
				t.Fatalf("after=%d want %d", after, tc.wantAfter)
			}
			var stock int
			if err := db.QueryRow(`SELECT stock_quantity FROM inventory_items WHERE id=?`, tc.itemID).Scan(&stock); err != nil || stock != tc.wantStock {
				t.Fatalf("stock=%d err=%v want %d", stock, err, tc.wantStock)
			}
			if tc.wantMov != nil {
				var m struct {
					delta, before, after int
					reason, source       string
					createdBy            sql.NullInt64
				}
				if err := db.QueryRow(`SELECT delta, before_quantity, after_quantity, reason, source, created_by FROM inventory_movements WHERE inventory_item_id=? ORDER BY id DESC LIMIT 1`, tc.itemID).
					Scan(&m.delta, &m.before, &m.after, &m.reason, &m.source, &m.createdBy); err != nil {
					t.Fatal(err)
				}
				want := *tc.wantMov
				if m.delta != want.delta || m.before != want.before || m.after != want.after ||
					m.reason != want.reason || m.source != want.source || m.createdBy != want.createdBy {
					t.Fatalf("movement=%+v want %+v", m, want)
				}
			}
		})
	}
}

func TestInventoryServiceApplyStockDeltaMovementFailure(t *testing.T) {
	db := setupInventorySvcDB(t)
	svc := NewInventoryService(db)
	if _, err := db.Exec(`DROP TABLE inventory_movements`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyStockDelta(context.Background(), 1, -1, "x", "manual", "1"); !errors.Is(err, ErrMovementWrite) {
		t.Fatalf("err=%v want ErrMovementWrite", err)
	}
	// the stock update must have been rolled back with the failed movement write
	var stock int
	if err := db.QueryRow(`SELECT stock_quantity FROM inventory_items WHERE id=1`).Scan(&stock); err != nil || stock != 10 {
		t.Fatalf("stock=%d err=%v, want 10 (rolled back)", stock, err)
	}
}

func TestInventoryServiceApplyStockDeltaUpdateError(t *testing.T) {
	db := setupInventorySvcDB(t)
	svc := NewInventoryService(db)
	if _, err := db.Exec(`DROP TABLE inventory_items`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyStockDelta(context.Background(), 1, -1, "x", "manual", "1"); err == nil ||
		errors.Is(err, ErrItemNotFound) || errors.Is(err, ErrMovementWrite) {
		t.Fatalf("expected generic update error, got %v", err)
	}
	if _, err := svc.ApplyStockDelta(context.Background(), 1, 1, "x", "manual", "1"); err == nil ||
		errors.Is(err, ErrItemNotFound) {
		t.Fatalf("expected generic restock update error, got %v", err)
	}
}

func TestStockShortfallError(t *testing.T) {
	err := &StockShortfall{Current: 3, Needed: 5}
	if got, want := err.Error(), "insufficient stock available: current 3, requested 5"; got != want {
		t.Fatalf("Error()=%q want %q", got, want)
	}
	if !errors.Is(err, ErrInsufficientStock) {
		t.Fatal("StockShortfall should match ErrInsufficientStock")
	}
}
