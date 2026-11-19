package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

var (
	ErrInsufficientStock = errors.New("insufficient stock available")
	ErrItemNotFound      = errors.New("inventory item not found")
	ErrReferenceExists   = errors.New("item cannot be deleted as it is referenced by work orders")
	// ErrMovementWrite is returned when the stock update succeeded but the
	// movement record could not be written; the transaction is rolled back.
	ErrMovementWrite = errors.New("failed to write inventory movement")
)

// StockShortfall reports an attempt to deduct more units than the item has
// in stock. Current is the stock quantity at the time of the attempt and
// Needed is the amount that was requested to be deducted.
type StockShortfall struct {
	Current int
	Needed  int
}

func (e *StockShortfall) Error() string {
	return fmt.Sprintf("%s: current %d, requested %d", ErrInsufficientStock, e.Current, e.Needed)
}

// Is reports whether the target is ErrInsufficientStock.
func (e *StockShortfall) Is(target error) bool { return target == ErrInsufficientStock }

// InventoryService handles all operations related to the inventory.
type InventoryService struct {
	db *sql.DB
}

// NewInventoryService creates a new instance of InventoryService.
func NewInventoryService(db *sql.DB) *InventoryService {
	return &InventoryService{db: db}
}

// ApplyStockAdjust handles the deduction of inventory stock and the recording of movements.
// It uses db.BeginTx with Serializable isolation to ensure atomicity.
func (s *InventoryService) ApplyStockAdjust(ctx context.Context, itemID int, quantity int, source string, actorID string) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.ApplyStockAdjustTx(ctx, tx, itemID, quantity, source, actorID, false); err != nil {
		return err
	}
	return tx.Commit()
}

// ApplyStockAdjustTx performs the same logic as ApplyStockAdjust but uses the provided sqlExecutor.
// The caller owns the transaction lifecycle (commit/rollback), mirroring checkoutTx/withTx.
// deductReservation: if true, also deducts the same amount from reserved_quantity (common in checkout).
func (s *InventoryService) ApplyStockAdjustTx(ctx context.Context, tx sqlExecutor, itemID int, quantity int, source string, actorID string, deductReservation bool) error {
	// 1. Check current stock and reserved quantity
	var stockQty, reservedQty int
	err := tx.QueryRowContext(ctx, "SELECT stock_quantity, reserved_quantity FROM inventory_items WHERE id = ?", itemID).Scan(&stockQty, &reservedQty)
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrItemNotFound
		}
		return err
	}

	available := stockQty - reservedQty
	if available < quantity {
		return fmt.Errorf("%w: requested %d, available %d", ErrInsufficientStock, quantity, available)
	}

	// 2. Deduct stock
	_, err = tx.ExecContext(ctx, "UPDATE inventory_items SET stock_quantity = stock_quantity - ? WHERE id = ?", quantity, itemID)
	if err != nil {
		return err
	}

	// 3. Deduct reservation if requested
	if deductReservation {
		_, err = tx.ExecContext(ctx, "UPDATE inventory_items SET reserved_quantity = MAX(COALESCE(reserved_quantity,0) - ?, 0) WHERE id = ?", quantity, itemID)
		if err != nil {
			return err
		}
	}

	// 4. Record movement (columns match the real schema: delta/before/after/reason/source)
	after := stockQty - quantity
	var createdBy interface{}
	if n, err := strconv.ParseInt(actorID, 10, 64); err == nil {
		createdBy = n
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO inventory_movements
		(inventory_item_id, delta, before_quantity, after_quantity, reason, source, work_order_id, created_by)
		VALUES (?,?,?,?,?,?,?,?)`,
		itemID, -quantity, stockQty, after, "庫存扣減調整", source, nil, createdBy)
	if err != nil {
		return err
	}

	return nil
}

// ApplyWorkOrderCheckoutTx 在指定交易內扣減工單零件庫存（含保留量），並寫一筆
// 可追溯到工單的庫存異動。
//
// 這是結帳專屬的扣料方法，與 ApplyStockAdjustTx 語意不同：
//
//   - 檢查用 stock_quantity（不是 stock − reserved）：結帳是實際銷售，
//     不能因預留量存在就拒絕扣貨。
//   - 同時扣除 reserved_quantity：VoidReceipt 回沖時 stock 與 reserved 都加回，
//     兩邊必須對稱，否則作廢後保留量憑空消失。
//   - 異動帶 reason「工單 #N 結帳扣料」、source='checkout' 與 work_order_id，
//     讓庫存異動頁能追溯到工單。
func (s *InventoryService) ApplyWorkOrderCheckoutTx(ctx context.Context, tx sqlExecutor, workOrderID, itemID int64, quantity int, actorID int64) error {
	// RETURNING stock_quantity + ? = 扣減前的庫存量（before）。
	var before int
	err := tx.QueryRowContext(ctx, `UPDATE inventory_items
		SET stock_quantity = stock_quantity - ?,
			reserved_quantity = MAX(COALESCE(reserved_quantity,0) - ?, 0)
		WHERE id = ? AND COALESCE(stock_quantity,0) >= ?
		RETURNING stock_quantity + ?`,
		quantity, quantity, itemID, quantity, quantity).Scan(&before)
	if errors.Is(err, sql.ErrNoRows) {
		// UPDATE 沒命中有兩種可能：品項不存在、或庫存不足。要分開回報，
		// 否則庫存不足會被誤判成資料缺失。
		var current int
		qerr := tx.QueryRowContext(ctx, `SELECT COALESCE(stock_quantity,0) FROM inventory_items WHERE id = ?`, itemID).Scan(&current)
		if qerr != nil {
			return ErrItemNotFound
		}
		return &StockShortfall{Current: current, Needed: quantity}
	}
	if err != nil {
		return err
	}
	after := before - quantity
	_, err = tx.ExecContext(ctx, `INSERT INTO inventory_movements
		(inventory_item_id, delta, before_quantity, after_quantity, reason, source, work_order_id, created_by)
		VALUES (?,?,?,?,?,?,?,?)`,
		itemID, -quantity, before, after,
		fmt.Sprintf("工單 #%d 結帳扣料", workOrderID), "checkout", workOrderID, nullableID(actorID))
	return err
}

// ApplyStockDelta applies a signed stock delta (positive restocks, negative
// deducts) and records a movement in the same transaction, mirroring the
// manual stock-adjust semantics of the HTTP handler: deductions are
// conditional on sufficient stock at the SQL level, reserved quantities are
// not consulted, and the caller-supplied reason is stored on the movement.
// It returns the stock quantity after the adjustment. A non-numeric or
// non-positive actorID is stored as NULL created_by.
func (s *InventoryService) ApplyStockDelta(ctx context.Context, itemID int, delta int, reason, source, actorID string) (int, error) {
	// BEGIN IMMEDIATE on a dedicated connection, mirroring the handler's
	// original transaction: acquiring the write reservation up front is what
	// makes concurrent manual adjustments serialize instead of racing on a
	// deferred-transaction upgrade.
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "PRAGMA busy_timeout=5000"); err != nil {
		return 0, err
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
		}
	}()

	var after int
	var updateErr error
	if delta < 0 {
		needed := -delta
		updateErr = conn.QueryRowContext(ctx, `UPDATE inventory_items SET stock_quantity = stock_quantity - ?
			WHERE id = ? AND stock_quantity >= ? RETURNING stock_quantity`, needed, itemID, needed).Scan(&after)
	} else {
		updateErr = conn.QueryRowContext(ctx, `UPDATE inventory_items SET stock_quantity = stock_quantity + ?
			WHERE id = ? RETURNING stock_quantity`, delta, itemID).Scan(&after)
	}
	if updateErr == sql.ErrNoRows {
		var current int
		if err := conn.QueryRowContext(ctx, "SELECT COALESCE(stock_quantity,0) FROM inventory_items WHERE id = ? AND deleted_at IS NULL", itemID).Scan(&current); err != nil {
			return 0, ErrItemNotFound
		}
		if delta < 0 {
			return 0, &StockShortfall{Current: current, Needed: -delta}
		}
		return 0, ErrItemNotFound
	}
	if updateErr != nil {
		return 0, updateErr
	}

	before := after - delta
	var createdBy interface{}
	if n, err := strconv.ParseInt(actorID, 10, 64); err == nil && n > 0 {
		createdBy = n
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO inventory_movements
		(inventory_item_id, delta, before_quantity, after_quantity, reason, source, work_order_id, created_by)
		VALUES (?,?,?,?,?,?,?,?)`,
		itemID, delta, before, after, reason, source, nil, createdBy); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrMovementWrite, err)
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return 0, err
	}
	committed = true
	return after, nil
}

// ApplyStocktakeCountTx 把盤點實盤量寫入庫存（絕對值設定，非增減），
// 並在同一交易內寫 inventory_movements（delta＝實盤−目前帳面）。
// 盤點是「校正帳面」，不做库存不足檢查——負庫存 legacy 正是盤點要修的對象。
// delta 為 0 不寫異動；回傳是否有調整。
func (s *InventoryService) ApplyStocktakeCountTx(ctx context.Context, tx sqlExecutor, itemID int64, counted int, stocktakeID int64, actorID int64) (bool, error) {
	var current int
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(stock_quantity,0) FROM inventory_items WHERE id = ? AND deleted_at IS NULL`, itemID).Scan(&current)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrItemNotFound
		}
		return false, err
	}
	delta := counted - current
	if delta == 0 {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE inventory_items SET stock_quantity = ? WHERE id = ?`, counted, itemID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO inventory_movements
		(inventory_item_id, delta, before_quantity, after_quantity, reason, source, work_order_id, created_by)
		VALUES (?,?,?,?,?,?,?,?)`,
		itemID, delta, current, counted,
		fmt.Sprintf("盤點 #%d", stocktakeID), "stocktake", nil, nullableID(actorID)); err != nil {
		return false, fmt.Errorf("%w: %v", ErrMovementWrite, err)
	}
	return true, nil
}

// List returns all inventory items.
func (s *InventoryService) List(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, stock_quantity, reserved_quantity, category FROM inventory_items")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []map[string]interface{}
	for rows.Next() {
		var id int
		var name, category string
		var stock, reserved int
		if err := rows.Scan(&id, &name, &stock, &reserved, &category); err != nil {
			return nil, err
		}
		items = append(items, map[string]interface{}{
			"id":                id,
			"name":              name,
			"stock_quantity":    stock,
			"reserved_quantity": reserved,
			"category":          category,
		})
	}
	return items, nil
}

// Search looks for inventory items by name or category.
func (s *InventoryService) Search(ctx context.Context, query string) ([]map[string]interface{}, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, stock_quantity, reserved_quantity, category FROM inventory_items WHERE name LIKE ? OR category LIKE ?", "%"+query+"%", "%"+query+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []map[string]interface{}
	for rows.Next() {
		var id int
		var name, category string
		var stock, reserved int
		if err := rows.Scan(&id, &name, &stock, &reserved, &category); err != nil {
			return nil, err
		}
		items = append(items, map[string]interface{}{
			"id":                id,
			"name":              name,
			"stock_quantity":    stock,
			"reserved_quantity": reserved,
			"category":          category,
		})
	}
	return items, nil
}

// Create adds a new inventory item.
func (s *InventoryService) Create(ctx context.Context, name, category string, initialStock int) (int, error) {
	res, err := s.db.ExecContext(ctx, "INSERT INTO inventory_items (name, category, stock_quantity, reserved_quantity) VALUES (?, ?, ?, 0)", name, category, initialStock)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return int(id), nil
}

// Update modifies an existing inventory item.
func (s *InventoryService) Update(ctx context.Context, id int, name, category string, stock, reserved int) error {
	_, err := s.db.ExecContext(ctx, "UPDATE inventory_items SET name = ?, category = ?, stock_quantity = ?, reserved_quantity = ? WHERE id = ?", name, category, stock, reserved, id)
	return err
}

// Delete 軟刪除庫存項目（標記 deleted_at，不再物理刪除）；若仍被工單明細
// 參考則拒絕。
func (s *InventoryService) Delete(ctx context.Context, id int) error {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM active_work_order_items WHERE inventory_item_id = ?", id).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrReferenceExists
	}

	// 軟刪除：不留物理刪除的路徑（SRS：重要營業資料不得物理刪除）。
	// 參考計量改用 active_work_order_items，已刪明細不擋刪除。
	_, err = s.db.ExecContext(ctx, "UPDATE inventory_items SET deleted_at = datetime('now') WHERE id = ? AND deleted_at IS NULL", id)
	return err
}
