package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrAlreadyPaid indicates the target work order has already been paid and
// cannot be transitioned away from the paid state.
var ErrAlreadyPaid = errors.New("work order already paid")

// TransitionWorkOrderStatus atomically updates a work order's status with a
// conditional update that never overwrites a paid order. Repeating a
// transition to "paid" on an already-paid order is a no-op. The paid-checkout
// path (inventory deduction, movements, receipt effects) remains in Checkout;
// callers transitioning to "paid" should use Checkout instead.
func (s *Service) TransitionWorkOrderStatus(ctx context.Context, id int64, target string, actorID int64) error {
	return s.withTx(ctx, "transition work order status", func(tx sqlExecutor) error {
		return transitionWorkOrderStatusTx(ctx, tx, id, target, actorID)
	})
}

func transitionWorkOrderStatusTx(ctx context.Context, tx sqlExecutor, id int64, target string, actorID int64) error {
	_ = actorID // reserved for interface symmetry with Checkout; work_orders has no updated_by column
	if id <= 0 {
		return ErrNotFound
	}
	result, err := tx.ExecContext(ctx, `UPDATE work_orders SET status=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status != 'paid'`, target, id)
	if err != nil {
		return fmt.Errorf("update work order status: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check work order status update: %w", err)
	}
	if affected == 0 {
		var status string
		err := tx.QueryRowContext(ctx, `SELECT status FROM active_work_orders WHERE id = ?`, id).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("read work order status: %w", err)
		}
		if status == "paid" {
			if target == "paid" {
				return nil
			}
			return ErrAlreadyPaid
		}
		return ErrNotFound
	}
	return nil
}
