package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
)

var ErrEstimateNotFound = errors.New("estimate not found")
var ErrEstimateNotDraft = errors.New("estimate is no longer editable")
var ErrEstimateHasNoItems = errors.New("estimate must contain at least one item")

type EstimateInput struct {
	CustomerID        int64
	VehicleID         int64
	CurrentMileage    int
	CustomerComplaint string
	CreatedBy         int64
}

type EstimateItemInput struct {
	ItemType        string
	InventoryItemID int64
	Description     string
	Quantity        int
	UnitPrice       float64
}

// CreateEstimate creates a draft attached to an existing vehicle and its owner.
func (s *Service) CreateEstimate(ctx context.Context, input EstimateInput) (int64, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("service database is nil")
	}
	if input.VehicleID <= 0 || input.CreatedBy <= 0 || input.CurrentMileage < 0 {
		return 0, errors.New("vehicle, creator, and a non-negative mileage are required")
	}
	returnEstimateID := int64(0)
	err := s.withTx(ctx, "create estimate", func(tx sqlExecutor) error {
		var customerID int64
		err := tx.QueryRowContext(ctx, `SELECT customer_id FROM active_vehicles WHERE id = ?`,
			input.VehicleID).Scan(&customerID)
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("vehicle not found")
		}
		if err != nil {
			return fmt.Errorf("load estimate vehicle: %w", err)
		}
		if input.CustomerID > 0 && input.CustomerID != customerID {
			return errors.New("estimate customer does not own the selected vehicle")
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO estimates
			(customer_id, vehicle_id, current_mileage, customer_complaint, created_by)
			VALUES (?,?,?,?,?)`,
			customerID, input.VehicleID, input.CurrentMileage,
			strings.TrimSpace(input.CustomerComplaint), input.CreatedBy)
		if err != nil {
			return fmt.Errorf("insert estimate: %w", err)
		}
		returnEstimateID, err = result.LastInsertId()
		if err != nil {
			return fmt.Errorf("read estimate ID: %w", err)
		}
		return nil
	})
	return returnEstimateID, err
}

// AddEstimateItem adds a quote line and refreshes the draft's discounted total.
func (s *Service) AddEstimateItem(ctx context.Context, estimateID int64, input EstimateItemInput) error {
	if s == nil || s.db == nil {
		return errors.New("service database is nil")
	}
	if estimateID <= 0 {
		return ErrEstimateNotFound
	}
	if input.ItemType != "labor" && input.ItemType != "part" && input.ItemType != "subcontract" {
		return errors.New("invalid estimate item type")
	}
	if input.Description = strings.TrimSpace(input.Description); input.Description == "" {
		return errors.New("item description is required")
	}
	if input.Quantity <= 0 || input.UnitPrice < 0 ||
		math.IsNaN(input.UnitPrice) || math.IsInf(input.UnitPrice, 0) {
		return errors.New("item quantity must be positive and unit price cannot be negative")
	}
	return s.withTx(ctx, "add estimate item", func(tx sqlExecutor) error {
		if err := requireDraftEstimate(ctx, tx, estimateID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO estimate_items
			(estimate_id, item_type, inventory_item_id, description, quantity, unit_price, subtotal)
			VALUES (?,?,?,?,?,?,?)`,
			estimateID, input.ItemType, nullableID(input.InventoryItemID), input.Description,
			input.Quantity, input.UnitPrice, float64(input.Quantity)*input.UnitPrice)
		if err != nil {
			return fmt.Errorf("insert estimate item: %w", err)
		}
		return recalculateEstimate(ctx, tx, estimateID)
	})
}

func (s *Service) DeleteEstimateItem(ctx context.Context, estimateID, itemID int64) error {
	if s == nil || s.db == nil {
		return errors.New("service database is nil")
	}
	return s.withTx(ctx, "delete estimate item", func(tx sqlExecutor) error {
		if err := requireDraftEstimate(ctx, tx, estimateID); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx,
			`DELETE FROM estimate_items WHERE id = ? AND estimate_id = ?`, itemID, estimateID)
		if err != nil {
			return fmt.Errorf("delete estimate item: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("check deleted estimate item: %w", err)
		}
		if affected == 0 {
			return errors.New("estimate item not found")
		}
		return recalculateEstimate(ctx, tx, estimateID)
	})
}

func (s *Service) SetEstimateDiscount(ctx context.Context, estimateID int64, discount float64) error {
	if s == nil || s.db == nil {
		return errors.New("service database is nil")
	}
	if discount < 0 || math.IsNaN(discount) || math.IsInf(discount, 0) {
		return errors.New("discount cannot be negative")
	}
	return s.withTx(ctx, "update estimate discount", func(tx sqlExecutor) error {
		if err := requireDraftEstimate(ctx, tx, estimateID); err != nil {
			return err
		}
		var subtotal float64
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(subtotal),0) FROM estimate_items WHERE estimate_id = ?`,
			estimateID).Scan(&subtotal); err != nil {
			return fmt.Errorf("calculate estimate subtotal: %w", err)
		}
		if discount > subtotal {
			return errors.New("discount cannot exceed estimate subtotal")
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE estimates SET discount_amount = ? WHERE id = ?`, discount, estimateID); err != nil {
			return fmt.Errorf("update estimate discount: %w", err)
		}
		return recalculateEstimate(ctx, tx, estimateID)
	})
}

// ConvertEstimate is transactional and idempotent. A repeated conversion returns
// the first work-order ID and never duplicates its items.
func (s *Service) ConvertEstimate(ctx context.Context, estimateID int64) (int64, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("service database is nil")
	}
	if estimateID <= 0 {
		return 0, ErrEstimateNotFound
	}
	workOrderID := int64(0)
	err := s.withTx(ctx, "convert estimate", func(tx sqlExecutor) error {
		var status string
		var convertedID sql.NullInt64
		var vehicleID, customerID int64
		var mileage int
		var complaint string
		var total, discount float64
		err := tx.QueryRowContext(ctx, `SELECT status, converted_work_order_id, vehicle_id,
			customer_id, current_mileage, customer_complaint, total_amount, discount_amount
			FROM estimates WHERE id = ?`, estimateID).Scan(&status, &convertedID, &vehicleID,
			&customerID, &mileage, &complaint, &total, &discount)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrEstimateNotFound
		}
		if err != nil {
			return fmt.Errorf("load estimate for conversion: %w", err)
		}
		if status == "converted" && convertedID.Valid {
			workOrderID = convertedID.Int64
			return nil
		}
		if status != "draft" {
			return ErrEstimateNotDraft
		}
		var itemCount int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM estimate_items WHERE estimate_id = ?`,
			estimateID).Scan(&itemCount); err != nil {
			return fmt.Errorf("count estimate items: %w", err)
		}
		if itemCount == 0 {
			return ErrEstimateHasNoItems
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO work_orders
			(vehicle_id, customer_id, status, current_mileage, customer_complaint,
			 approval_status, total_amount, original_estimate_id, quoted_total_amount,
			 quoted_discount_amount)
			VALUES (?,?,'pending',?,?,'not_required',?,?,?,?)`,
			vehicleID, customerID, mileage, complaint, total, estimateID, total, discount)
		if err != nil {
			return fmt.Errorf("create work order from estimate: %w", err)
		}
		workOrderID, err = result.LastInsertId()
		if err != nil {
			return fmt.Errorf("read converted work-order ID: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO work_order_items
			(work_order_id, item_type, inventory_item_id, description, quantity, unit_price,
			 subtotal, primary_technician_id, assistant_technician_id, photo_url)
			SELECT ?, item_type, inventory_item_id, description, quantity, unit_price,
			 subtotal, primary_technician_id, assistant_technician_id, photo_url
			FROM estimate_items WHERE estimate_id = ? ORDER BY id`, workOrderID, estimateID); err != nil {
			return fmt.Errorf("copy estimate items to work order: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE estimates SET
			status = 'converted', converted_work_order_id = ? WHERE id = ? AND status = 'draft'`,
			workOrderID, estimateID); err != nil {
			return fmt.Errorf("mark estimate converted: %w", err)
		}
		return nil
	})
	return workOrderID, err
}

func requireDraftEstimate(ctx context.Context, tx sqlExecutor, estimateID int64) error {
	var status string
	err := tx.QueryRowContext(ctx, `SELECT status FROM estimates WHERE id = ?`, estimateID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrEstimateNotFound
	}
	if err != nil {
		return fmt.Errorf("load estimate: %w", err)
	}
	if status != "draft" {
		return ErrEstimateNotDraft
	}
	return nil
}

func recalculateEstimate(ctx context.Context, tx sqlExecutor, estimateID int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE estimates SET
		discount_amount = MIN(discount_amount,
			COALESCE((SELECT SUM(subtotal) FROM estimate_items WHERE estimate_id = ?),0)),
		total_amount = MAX(
			COALESCE((SELECT SUM(subtotal) FROM estimate_items WHERE estimate_id = ?),0) -
			MIN(discount_amount,
				COALESCE((SELECT SUM(subtotal) FROM estimate_items WHERE estimate_id = ?),0)), 0)
		WHERE id = ?`, estimateID, estimateID, estimateID, estimateID)
	if err != nil {
		return fmt.Errorf("recalculate estimate total: %w", err)
	}
	return nil
}
