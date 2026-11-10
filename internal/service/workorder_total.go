package service

import "context"

// RecalculateWorkOrderTotal recomputes work_orders.total_amount as the sum of
// its line items' subtotals, falling back to quantity*unit_price when subtotal
// is NULL (COALESCE chain preserved verbatim from the original handler
// implementation). The caller owns the transaction lifecycle.
func RecalculateWorkOrderTotal(ctx context.Context, tx sqlExecutor, woID int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE work_orders SET total_amount =
		COALESCE((SELECT SUM(COALESCE(subtotal, COALESCE(quantity,0)*COALESCE(unit_price,0))) FROM work_order_items WHERE work_order_id = ?), 0),
		updated_at = CURRENT_TIMESTAMP WHERE id = ?`, woID, woID)
	return err
}

// RecalculateWorkOrderTotal is the Service-method form of the package-level
// helper above; both share the same SQL and semantics.
func (s *Service) RecalculateWorkOrderTotal(ctx context.Context, tx sqlExecutor, woID int64) error {
	return RecalculateWorkOrderTotal(ctx, tx, woID)
}
