package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("work order not found")

// Service contains business operations backed by an injected database.
type Service struct {
	db *sql.DB
}

func New(db *sql.DB) *Service {
	return &Service{db: db}
}

// ReceiptInput contains the fields needed to create a receipt associated with a work order.
type ReceiptInput struct {
	VoucherNo   string
	CustomerID  int64
	WorkOrderID int64
	PayDate     string
	DetailTotal float64
	RealTotal   float64
	Discount    float64
	Tax         float64
	PayTotal    float64
	Cash        float64
	Card        float64
	CheckAmount float64
	CardNo      string
	CheckNo     string
	CheckDate   string
	Bank        string
	Notes       string
	CreatedBy   int64
}

// Checkout marks a work order paid and applies its inventory and customer-account
// effects atomically. Repeating a successful checkout is a no-op.
func (s *Service) Checkout(ctx context.Context, workOrderID, actorID int64) error {
	if s == nil || s.db == nil {
		return errors.New("service database is nil")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin checkout transaction: %w", err)
	}
	defer tx.Rollback()

	if err := checkoutTx(ctx, tx, workOrderID, actorID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit checkout transaction: %w", err)
	}
	return nil
}

// AssociateReceipt records a receipt and checks out its work order in one
// transaction, so neither operation can succeed without the other.
func (s *Service) AssociateReceipt(ctx context.Context, receipt ReceiptInput) error {
	if s == nil || s.db == nil {
		return errors.New("service database is nil")
	}
	if receipt.WorkOrderID <= 0 {
		return errors.New("work order ID is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin receipt transaction: %w", err)
	}
	defer tx.Rollback()

	var customerID, vehicleID int64
	err = tx.QueryRowContext(ctx, `SELECT customer_id, vehicle_id
		FROM work_orders WHERE id = ?`, receipt.WorkOrderID).Scan(&customerID, &vehicleID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load work order for receipt: %w", err)
	}
	if customerID != receipt.CustomerID {
		return errors.New("receipt customer does not match work order")
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO receivables
		(voucher_no, customer_id, vehicle_id, work_order_id, pay_date,
		 detail_total, real_total, discount, tax, pay_total,
		 cash, card, check_amount, card_no, check_no, check_date, bank,
		 notes, source, created_by)
		VALUES (?,?,?,?,?, ?,?,?,?,?, ?,?,?,?,?,?,?, ?, 'manual', ?)`,
		receipt.VoucherNo, receipt.CustomerID, vehicleID, receipt.WorkOrderID, receipt.PayDate,
		receipt.DetailTotal, receipt.RealTotal, receipt.Discount, receipt.Tax, receipt.PayTotal,
		receipt.Cash, receipt.Card, receipt.CheckAmount, nullableString(receipt.CardNo),
		nullableString(receipt.CheckNo), nullableString(receipt.CheckDate),
		nullableString(receipt.Bank), nullableString(receipt.Notes), nullableID(receipt.CreatedBy))
	if err != nil {
		return fmt.Errorf("insert associated receipt: %w", err)
	}

	if err := checkoutTx(ctx, tx, receipt.WorkOrderID, receipt.CreatedBy); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit receipt and checkout transaction: %w", err)
	}
	return nil
}

func checkoutTx(ctx context.Context, tx *sql.Tx, workOrderID, actorID int64) error {
	result, err := tx.ExecContext(ctx, `UPDATE work_orders
		SET status = 'paid', updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status != 'paid'`, workOrderID)
	if err != nil {
		return fmt.Errorf("mark work order paid: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check work order status update: %w", err)
	}
	if affected == 0 {
		var status string
		err := tx.QueryRowContext(ctx, `SELECT status FROM work_orders WHERE id = ?`, workOrderID).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("read work order status: %w", err)
		}
		if status == "paid" {
			return nil
		}
		return ErrNotFound
	}

	var customerID int64
	err = tx.QueryRowContext(ctx, `SELECT customer_id FROM work_orders WHERE id = ?`, workOrderID).Scan(&customerID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load work order customer: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `SELECT inventory_item_id, quantity
		FROM work_order_items
		WHERE work_order_id = ? AND item_type = 'part'
			AND inventory_item_id > 0 AND quantity > 0
		ORDER BY id`, workOrderID)
	if err != nil {
		return fmt.Errorf("load work order parts: %w", err)
	}
	type partDelta struct {
		inventoryID int64
		quantity    int
	}
	var deltas []partDelta
	for rows.Next() {
		var delta partDelta
		if err := rows.Scan(&delta.inventoryID, &delta.quantity); err != nil {
			rows.Close()
			return fmt.Errorf("scan work order part: %w", err)
		}
		deltas = append(deltas, delta)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read work order parts: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close work order parts: %w", err)
	}

	for _, delta := range deltas {
		var before int
		err := tx.QueryRowContext(ctx, `UPDATE inventory_items
			SET stock_quantity = stock_quantity - ?,
				reserved_quantity = MAX(COALESCE(reserved_quantity,0) - ?, 0)
			WHERE id = ? AND COALESCE(stock_quantity,0) >= ?
			RETURNING stock_quantity + ?`,
			delta.quantity, delta.quantity, delta.inventoryID, delta.quantity, delta.quantity).Scan(&before)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("insufficient inventory for item %d", delta.inventoryID)
		}
		if err != nil {
			return fmt.Errorf("deduct inventory item %d: %w", delta.inventoryID, err)
		}
		after := before - delta.quantity
		_, err = tx.ExecContext(ctx, `INSERT INTO inventory_movements
			(inventory_item_id, delta, before_quantity, after_quantity, reason, source,
			 work_order_id, created_by)
			VALUES (?,?,?,?,?,?,?,?)`,
			delta.inventoryID, -delta.quantity, before, after,
			fmt.Sprintf("工單 #%d 結帳扣料", workOrderID), "checkout",
			workOrderID, nullableID(actorID))
		if err != nil {
			return fmt.Errorf("record inventory movement for item %d: %w", delta.inventoryID, err)
		}
	}

	result, err = tx.ExecContext(ctx, `UPDATE customers
		SET total_spent = COALESCE(total_spent,0) +
			COALESCE((SELECT total_amount FROM work_orders WHERE id = ?),0),
			unpaid_amount = 0
		WHERE id = ?`, workOrderID, customerID)
	if err != nil {
		return fmt.Errorf("update customer spending: %w", err)
	}
	affected, err = result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check customer update: %w", err)
	}
	if affected != 1 {
		return errors.New("work order customer not found")
	}
	return nil
}

func nullableString(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}

func nullableID(value int64) interface{} {
	if value <= 0 {
		return nil
	}
	return value
}
