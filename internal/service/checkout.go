package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("work order not found")
var ErrReceiptNotFound = errors.New("receipt not found")
var ErrReceiptVoided = errors.New("receipt is voided")
var ErrDuplicateVoucher = errors.New("receipt voucher number already exists")

// Service contains business operations backed by an injected database.
type Service struct {
	db *sql.DB
}

type sqlExecutor interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}

func New(db *sql.DB) *Service {
	return &Service{db: db}
}

// ReceiptInput contains the fields needed to create or update a receipt.
type ReceiptInput struct {
	VoucherNo         string
	CustomerID        int64
	WorkOrderID       int64
	PayDate           string
	DetailTotal       float64
	RealTotal         float64
	Discount          float64
	Tax               float64
	PayTotal          float64
	Cash              float64
	Card              float64
	CheckAmount       float64
	TransferAmount    float64
	TransferReference string
	CardNo            string
	CheckNo           string
	CheckDate         string
	Bank              string
	Notes             string
	CreatedBy         int64
}

// Checkout marks a work order paid and applies its inventory and customer-account
// effects atomically. Repeating a successful checkout is a no-op.
func (s *Service) Checkout(ctx context.Context, workOrderID, actorID int64) error {
	return s.withTx(ctx, "checkout", func(tx sqlExecutor) error {
		return checkoutTx(ctx, tx, workOrderID, actorID)
	})
}

// AssociateReceipt is kept as a compatibility alias for CreateReceipt.
func (s *Service) AssociateReceipt(ctx context.Context, receipt ReceiptInput) error {
	return s.CreateReceipt(ctx, receipt)
}

// CreateReceipt inserts a receipt and checks out its associated work order in
// the same transaction when active receipts cover the full work-order total.
// Replaying an identical voucher is a no-op; reusing its number for different
// receipt data is rejected.
func (s *Service) CreateReceipt(ctx context.Context, receipt ReceiptInput) error {
	if s == nil || s.db == nil {
		return errors.New("service database is nil")
	}
	if receipt.CustomerID <= 0 {
		return errors.New("customer ID is required")
	}
	if receipt.VoucherNo == "" {
		return errors.New("receipt voucher number is required")
	}
	if receipt.Cash < 0 || receipt.Card < 0 || receipt.CheckAmount < 0 ||
		receipt.TransferAmount < 0 || receipt.PayTotal < 0 {
		return errors.New("receipt amounts cannot be negative")
	}
	if receipt.WorkOrderID < 0 {
		return errors.New("work order ID cannot be negative")
	}

	return s.withTx(ctx, "create receipt", func(tx sqlExecutor) error {
		matched, err := existingReceiptMatches(ctx, tx, receipt)
		if err != nil {
			return err
		}
		if matched {
			return nil
		}

		var customerID, vehicleID int64
		if receipt.WorkOrderID > 0 {
			err = tx.QueryRowContext(ctx, `SELECT customer_id, vehicle_id
				FROM active_work_orders WHERE id = ?`, receipt.WorkOrderID).Scan(&customerID, &vehicleID)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("load work order for receipt: %w", err)
			}
			if customerID != receipt.CustomerID {
				return errors.New("receipt customer does not match work order")
			}
		} else {
			err = tx.QueryRowContext(ctx, `SELECT id FROM active_customers WHERE id = ?`, receipt.CustomerID).Scan(&customerID)
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("receipt customer not found")
			}
			if err != nil {
				return fmt.Errorf("load receipt customer: %w", err)
			}
		}

		_, err = tx.ExecContext(ctx, `INSERT INTO receivables
			(voucher_no, customer_id, vehicle_id, work_order_id, pay_date,
			 detail_total, real_total, discount, tax, pay_total,
			 cash, card, check_amount, transfer_amount, transfer_reference,
			 card_no, check_no, check_date, bank,
			 notes, source, created_by)
			VALUES (?,?,?,?,? ,?,?,?,?,? ,?,?,?,?,? ,?,?,?,?,? ,'manual',?)`,
			receipt.VoucherNo, receipt.CustomerID, nullableID(vehicleID), nullableID(receipt.WorkOrderID), receipt.PayDate,
			receipt.DetailTotal, receipt.RealTotal, receipt.Discount, receipt.Tax, receipt.PayTotal,
			receipt.Cash, receipt.Card, receipt.CheckAmount, receipt.TransferAmount,
			nullableString(receipt.TransferReference), nullableString(receipt.CardNo),
			nullableString(receipt.CheckNo), nullableString(receipt.CheckDate),
			nullableString(receipt.Bank), nullableString(receipt.Notes), nullableID(receipt.CreatedBy))
		if err != nil {
			return fmt.Errorf("insert associated receipt: %w", err)
		}

		if receipt.WorkOrderID > 0 {
			ready, err := receiptCompletesWorkOrder(ctx, tx, receipt.WorkOrderID)
			if err != nil {
				return err
			}
			if !ready {
				return nil
			}
			if err := checkoutTx(ctx, tx, receipt.WorkOrderID, receipt.CreatedBy); err != nil {
				return err
			}
		}
		return nil
	})
}

// UpdateReceipt updates a receipt and checks out its associated work order in
// the same transaction. The receipt's customer and work-order association are
// immutable through this operation.
func (s *Service) UpdateReceipt(ctx context.Context, id int64, receipt ReceiptInput) error {
	if s == nil || s.db == nil {
		return errors.New("service database is nil")
	}
	if id <= 0 {
		return ErrReceiptNotFound
	}
	if receipt.VoucherNo == "" {
		return errors.New("receipt voucher number is required")
	}
	if receipt.Cash < 0 || receipt.Card < 0 || receipt.CheckAmount < 0 ||
		receipt.TransferAmount < 0 || receipt.PayTotal < 0 {
		return errors.New("receipt amounts cannot be negative")
	}
	return s.withTx(ctx, "update receipt", func(tx sqlExecutor) error {
		var workOrderID int64
		var voided int
		err := tx.QueryRowContext(ctx, `SELECT COALESCE(work_order_id,0), voided
			FROM receivables WHERE id = ?`, id).Scan(&workOrderID, &voided)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrReceiptNotFound
		}
		if err != nil {
			return fmt.Errorf("load receipt for update: %w", err)
		}
		if voided != 0 {
			return ErrReceiptVoided
		}
		var duplicateID int64
		err = tx.QueryRowContext(ctx, `SELECT id FROM receivables WHERE voucher_no = ? AND id != ? LIMIT 1`,
			receipt.VoucherNo, id).Scan(&duplicateID)
		if err == nil {
			return ErrDuplicateVoucher
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check receipt voucher: %w", err)
		}
		_, err = tx.ExecContext(ctx, `UPDATE receivables SET
			voucher_no = ?, pay_date = ?, detail_total = ?, real_total = ?,
			discount = ?, tax = ?, pay_total = ?,
			cash = ?, card = ?, check_amount = ?, transfer_amount = ?, transfer_reference = ?,
			card_no = ?, check_no = ?, check_date = ?, bank = ?,
			notes = ?
			WHERE id = ? AND voided = 0`,
			receipt.VoucherNo, receipt.PayDate, receipt.DetailTotal, receipt.RealTotal,
			receipt.Discount, receipt.Tax, receipt.PayTotal, receipt.Cash, receipt.Card,
			receipt.CheckAmount, receipt.TransferAmount, nullableString(receipt.TransferReference),
			nullableString(receipt.CardNo), nullableString(receipt.CheckNo),
			nullableString(receipt.CheckDate), nullableString(receipt.Bank),
			nullableString(receipt.Notes), id)
		if err != nil {
			return fmt.Errorf("update receipt: %w", err)
		}
		if workOrderID > 0 {
			ready, err := receiptCompletesWorkOrder(ctx, tx, workOrderID)
			if err != nil {
				return err
			}
			if !ready {
				return nil
			}
			if err := checkoutTx(ctx, tx, workOrderID, receipt.CreatedBy); err != nil {
				return err
			}
		}
		return nil
	})
}

// VoidReceipt soft-deletes a receipt in a transaction. Voiding a receipt does
// not reverse an already completed checkout or its inventory/accounting effects.
func (s *Service) VoidReceipt(ctx context.Context, id int64) error {
	if s == nil || s.db == nil {
		return errors.New("service database is nil")
	}
	if id <= 0 {
		return ErrReceiptNotFound
	}
	return s.withTx(ctx, "void receipt", func(tx sqlExecutor) error {
		result, err := tx.ExecContext(ctx,
			`UPDATE receivables SET voided = 1 WHERE id = ? AND voided = 0`, id)
		if err != nil {
			return fmt.Errorf("void receipt: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("check receipt void: %w", err)
		}
		if affected == 0 {
			var exists int
			err := tx.QueryRowContext(ctx, `SELECT 1 FROM receivables WHERE id = ?`, id).Scan(&exists)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrReceiptNotFound
			}
			if err != nil {
				return fmt.Errorf("check receipt existence: %w", err)
			}
			return ErrReceiptVoided
		}
		return nil
	})
}

func (s *Service) withTx(ctx context.Context, operation string, fn func(sqlExecutor) error) error {
	if s == nil || s.db == nil {
		return errors.New("service database is nil")
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("begin %s transaction: %w", operation, err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "PRAGMA busy_timeout=5000"); err != nil {
		return fmt.Errorf("configure %s transaction: %w", operation, err)
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("begin %s transaction: %w", operation, err)
	}
	defer conn.ExecContext(ctx, "ROLLBACK")
	if err := fn(conn); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit %s transaction: %w", operation, err)
	}
	return nil
}

func existingReceiptMatches(ctx context.Context, tx sqlExecutor, receipt ReceiptInput) (bool, error) {
	var existing ReceiptInput
	var source string
	err := tx.QueryRowContext(ctx, `SELECT voucher_no, customer_id, COALESCE(work_order_id,0),
		pay_date, detail_total, real_total, discount, tax, pay_total, cash, card,
		check_amount, transfer_amount, COALESCE(transfer_reference,''),
		COALESCE(card_no,''), COALESCE(check_no,''), COALESCE(check_date,''),
		COALESCE(bank,''), COALESCE(notes,''), source
		FROM receivables WHERE voucher_no = ? LIMIT 1`, receipt.VoucherNo).Scan(
		&existing.VoucherNo, &existing.CustomerID, &existing.WorkOrderID,
		&existing.PayDate, &existing.DetailTotal, &existing.RealTotal, &existing.Discount,
		&existing.Tax, &existing.PayTotal, &existing.Cash, &existing.Card, &existing.CheckAmount,
		&existing.TransferAmount, &existing.TransferReference,
		&existing.CardNo, &existing.CheckNo, &existing.CheckDate, &existing.Bank, &existing.Notes, &source)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check receipt voucher: %w", err)
	}
	if source == "manual" && sameReceipt(existing, receipt) {
		return true, nil
	}
	return false, ErrDuplicateVoucher
}

func sameReceipt(a, b ReceiptInput) bool {
	return a.VoucherNo == b.VoucherNo && a.CustomerID == b.CustomerID &&
		a.WorkOrderID == b.WorkOrderID && a.PayDate == b.PayDate &&
		a.DetailTotal == b.DetailTotal && a.RealTotal == b.RealTotal &&
		a.Discount == b.Discount && a.Tax == b.Tax && a.PayTotal == b.PayTotal &&
		a.Cash == b.Cash && a.Card == b.Card && a.CheckAmount == b.CheckAmount &&
		a.TransferAmount == b.TransferAmount && a.TransferReference == b.TransferReference &&
		a.CardNo == b.CardNo && a.CheckNo == b.CheckNo && a.CheckDate == b.CheckDate &&
		a.Bank == b.Bank && a.Notes == b.Notes
}

func receiptCompletesWorkOrder(ctx context.Context, tx sqlExecutor, workOrderID int64) (bool, error) {
	var status string
	var total, received float64
	err := tx.QueryRowContext(ctx, `SELECT wo.status, wo.total_amount,
		COALESCE((SELECT SUM(r.pay_total) FROM receivables r
			WHERE r.work_order_id = wo.id AND r.voided = 0),0)
		FROM active_work_orders wo WHERE wo.id = ?`, workOrderID).Scan(&status, &total, &received)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("calculate work order receipts: %w", err)
	}
	if status != "completed" {
		return false, nil
	}
	if total <= 0 {
		return false, nil
	}
	return received >= total, nil
}

func checkoutTx(ctx context.Context, tx sqlExecutor, workOrderID, actorID int64) error {
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
		err := tx.QueryRowContext(ctx, `SELECT status FROM active_work_orders WHERE id = ?`, workOrderID).Scan(&status)
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
	err = tx.QueryRowContext(ctx, `SELECT customer_id FROM active_work_orders WHERE id = ?`, workOrderID).Scan(&customerID)
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
			COALESCE((SELECT total_amount FROM active_work_orders WHERE id = ?),0),
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
		return ""
	}
	return value
}

func nullableID(value int64) interface{} {
	if value <= 0 {
		return nil
	}
	return value
}
