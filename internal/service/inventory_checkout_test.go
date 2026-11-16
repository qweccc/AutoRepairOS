package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// TestApplyWorkOrderCheckoutTx 驗證結帳扣料的完整語意：
// 檢查用 stock（不是 stock-reserved）、同扣保留量、寫出可追溯工單的異動。
func TestApplyWorkOrderCheckoutTx(t *testing.T) {
	db := setupInventorySvcDB(t)
	svc := NewInventoryService(db)
	ctx := context.Background()

	// 品項 1：stock 10 / reserved 2，扣 3。
	if err := svc.ApplyWorkOrderCheckoutTx(ctx, db, 7, 1, 3, 42); err != nil {
		t.Fatal(err)
	}
	var stock, reserved int
	if err := db.QueryRow(`SELECT stock_quantity, reserved_quantity FROM inventory_items WHERE id=1`).Scan(&stock, &reserved); err != nil {
		t.Fatal(err)
	}
	if stock != 7 || reserved != 0 {
		t.Fatalf("stock=%d reserved=%d, want 7/0", stock, reserved)
	}

	var delta, before, after int
	var reason, source string
	var woID int64
	var createdBy sql.NullInt64
	if err := db.QueryRow(`SELECT delta, before_quantity, after_quantity, reason, source, work_order_id, created_by
		FROM inventory_movements WHERE inventory_item_id=1`).
		Scan(&delta, &before, &after, &reason, &source, &woID, &createdBy); err != nil {
		t.Fatal(err)
	}
	if delta != -3 || before != 10 || after != 7 {
		t.Fatalf("movement delta/before/after = %d/%d/%d, want -3/10/7", delta, before, after)
	}
	if reason != "工單 #7 結帳扣料" || source != "checkout" || woID != 7 {
		t.Fatalf("movement trace = %q/%q/%d, want 工單 #7 結帳扣料/checkout/7", reason, source, woID)
	}
	if !createdBy.Valid || createdBy.Int64 != 42 {
		t.Fatalf("created_by = %v, want 42", createdBy)
	}
}

func TestApplyWorkOrderCheckoutTxShortfall(t *testing.T) {
	db := setupInventorySvcDB(t)
	svc := NewInventoryService(db)

	// 品項 2 庫存 5，要扣 9。
	err := svc.ApplyWorkOrderCheckoutTx(context.Background(), db, 8, 2, 9, 1)
	if !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("err=%v, want ErrInsufficientStock", err)
	}
	var short *StockShortfall
	if !errors.As(err, &short) || short.Current != 5 || short.Needed != 9 {
		t.Fatalf("shortfall=%+v, want current 5 needed 9", short)
	}
}

func TestApplyWorkOrderCheckoutTxMissingItem(t *testing.T) {
	db := setupInventorySvcDB(t)
	svc := NewInventoryService(db)

	if err := svc.ApplyWorkOrderCheckoutTx(context.Background(), db, 8, 999, 1, 1); !errors.Is(err, ErrItemNotFound) {
		t.Fatalf("err=%v, want ErrItemNotFound", err)
	}
}
