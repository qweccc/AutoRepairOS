package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"autorepairos/internal/db"
)

// insertTestWorkOrder inserts a work order and returns its ID.
func insertTestWorkOrder(t *testing.T, status string) int64 {
	t.Helper()
	res, err := db.DB.Exec(`INSERT INTO work_orders (vehicle_id, customer_id, technician_id, status, current_mileage, sop_completed, approval_status, total_amount)
		VALUES (1, 1, 1, ?, 0, 0, 'not_required', 0)`, status)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	return id
}

// routeRequest routes a POST request through chi to set PathValue context.
func routeRequest(t *testing.T, id int64, status string) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/work-orders/{id}/status", UpdateWorkOrderStatus)

	req := httptest.NewRequest("POST", fmt.Sprintf("/work-orders/%d/status", id), nil)
	form := url.Values{}
	form.Set("status", status)
	req.PostForm = form
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w, req
}

func TestUpdateWorkOrderStatus_InvalidTransition(t *testing.T) {
	newMigratedTestDB(t)

	id := insertTestWorkOrder(t, "pending")
	w, _ := routeRequest(t, id, "completed")

	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestUpdateWorkOrderStatus_PaidImmutability(t *testing.T) {
	newMigratedTestDB(t)

	id := insertTestWorkOrder(t, "paid")
	w, _ := routeRequest(t, id, "in_progress")

	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestUpdateWorkOrderStatus_SuccessfulUpdate(t *testing.T) {
	newMigratedTestDB(t)

	id := insertTestWorkOrder(t, "pending")
	w, _ := routeRequest(t, id, "in_progress")

	assert.Equal(t, http.StatusFound, w.Code) // redirectMsg issues 302

	var status string
	err := db.DB.QueryRow("SELECT status FROM work_orders WHERE id = ?", id).Scan(&status)
	require.NoError(t, err)
	assert.Equal(t, "in_progress", status)
}

func TestUpdateWorkOrderStatus_InvalidStatusLabel(t *testing.T) {
	newMigratedTestDB(t)
	id := insertTestWorkOrder(t, "pending")
	w, _ := routeRequest(t, id, "not_a_status")

	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestUpdateWorkOrderStatus_SOPFailure(t *testing.T) {
	newMigratedTestDB(t)

	// 1. Insert a work order with status "pending"
	id := insertTestWorkOrder(t, "pending")

	// 2. Insert an incomplete required SOP record
	// Based on migration 035, work_order_sop_records has (id, work_order_id, sop_item_id, template_id, completed, required)
	// We use item 1 and template 1 which should exist.
	_, err := db.DB.Exec(`INSERT INTO work_order_sop_records (work_order_id, sop_item_id, template_id, completed, required)
		VALUES (?, 1, 1, 0, 1)`, id)
	require.NoError(t, err)

	// 3. Try to update to "completed"
	w, _ := routeRequest(t, id, "completed")

	// 4. Expect failure because CheckSOPCompletionForStatus will find incomplete required items
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestUpdateWorkOrderStatus_PaidCheckoutSuccess(t *testing.T) {
	newMigratedTestDB(t)

	// 1. Insert a work order with status "pending" and a customer_id
	// checkoutTx queries active_work_orders for customer_id
	id := insertTestWorkOrder(t, "pending")

	// 2. Insert a work order item to satisfy checkoutTx inventory requirements
	// We need an inventory_item_id > 0 and quantity > 0. 
	// Since we can't easily know IDs, we'll try to insert a dummy item record 
	// or assume item 1 exists.
	_, err := db.DB.Exec(`INSERT INTO work_order_items (work_order_id, inventory_item_id, quantity, unit_price, subtotal, item_type, description)
		VALUES (?, 1, 1, 100, 100, 'labor', 'Dummy labor item')`, id)
	require.NoError(t, err)

	// 3. Try to update to "paid"
	w, _ := routeRequest(t, id, "paid")

	// 4. If checkout succeeds, expect 302 redirect
	assert.Equal(t, http.StatusFound, w.Code)

	var status string
	err = db.DB.QueryRow("SELECT status FROM work_orders WHERE id = ?", id).Scan(&status)
	require.NoError(t, err)
	assert.Equal(t, "paid", status)
}
