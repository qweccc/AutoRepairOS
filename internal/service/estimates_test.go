package service

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func newEstimateTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = d.Close() })
	for _, statement := range []string{
		`CREATE TABLE customers (id INTEGER PRIMARY KEY)`,
		`CREATE TABLE vehicles (id INTEGER PRIMARY KEY, customer_id INTEGER)`,
		`CREATE VIEW active_vehicles AS SELECT * FROM vehicles`,
		`CREATE TABLE estimates (
			id INTEGER PRIMARY KEY AUTOINCREMENT, customer_id INTEGER, vehicle_id INTEGER,
			current_mileage INTEGER, customer_complaint TEXT, discount_amount REAL DEFAULT 0,
			total_amount REAL DEFAULT 0, status TEXT DEFAULT 'draft',
			created_by INTEGER, converted_work_order_id INTEGER
		)`,
		`CREATE TABLE estimate_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT, estimate_id INTEGER, item_type TEXT,
			inventory_item_id INTEGER, description TEXT, quantity INTEGER, unit_price REAL,
			subtotal REAL, primary_technician_id INTEGER, assistant_technician_id INTEGER, photo_url TEXT
		)`,
		`CREATE TABLE work_orders (
			id INTEGER PRIMARY KEY AUTOINCREMENT, vehicle_id INTEGER, customer_id INTEGER,
			status TEXT, current_mileage INTEGER, customer_complaint TEXT, approval_status TEXT,
			total_amount REAL, original_estimate_id INTEGER, quoted_total_amount REAL,
			quoted_discount_amount REAL
		)`,
		`CREATE TABLE work_order_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT, work_order_id INTEGER, item_type TEXT,
			inventory_item_id INTEGER, description TEXT, quantity INTEGER, unit_price REAL,
			subtotal REAL, primary_technician_id INTEGER, assistant_technician_id INTEGER, photo_url TEXT
		)`,
		`INSERT INTO customers (id) VALUES (1)`,
		`INSERT INTO vehicles (id, customer_id) VALUES (7, 1)`,
	} {
		if _, err := d.Exec(statement); err != nil {
			t.Fatalf("setup database: %v\nSQL: %s", err, statement)
		}
	}
	return d
}

func TestEstimateDraftTotalsAndConversionAreIdempotent(t *testing.T) {
	ctx := context.Background()
	d := newEstimateTestDB(t)
	svc := New(d)
	estimateID, err := svc.CreateEstimate(ctx, EstimateInput{
		VehicleID: 7, CurrentMileage: 85000, CustomerComplaint: "冷氣不冷", CreatedBy: 3,
	})
	if err != nil {
		t.Fatalf("CreateEstimate: %v", err)
	}
	for _, item := range []EstimateItemInput{
		{ItemType: "labor", Description: "冷氣系統檢修", Quantity: 2, UnitPrice: 500},
		{ItemType: "part", Description: "冷媒", Quantity: 1, UnitPrice: 250},
	} {
		if err := svc.AddEstimateItem(ctx, estimateID, item); err != nil {
			t.Fatalf("AddEstimateItem: %v", err)
		}
	}
	if err := svc.SetEstimateDiscount(ctx, estimateID, 100); err != nil {
		t.Fatalf("SetEstimateDiscount: %v", err)
	}

	var itemCount int
	var total, discount float64
	if err := d.QueryRow(`SELECT COUNT(*), total_amount, discount_amount
		FROM estimates JOIN estimate_items ON estimates.id = estimate_items.estimate_id
		WHERE estimates.id = ? GROUP BY estimates.id`, estimateID).Scan(&itemCount, &total, &discount); err != nil {
		t.Fatal(err)
	}
	if itemCount != 2 || total != 1150 || discount != 100 {
		t.Fatalf("draft count/total/discount = %d/%.2f/%.2f; want 2/1150/100",
			itemCount, total, discount)
	}

	firstID, err := svc.ConvertEstimate(ctx, estimateID)
	if err != nil {
		t.Fatalf("first ConvertEstimate: %v", err)
	}
	secondID, err := svc.ConvertEstimate(ctx, estimateID)
	if err != nil {
		t.Fatalf("repeated ConvertEstimate: %v", err)
	}
	if firstID <= 0 || secondID != firstID {
		t.Fatalf("converted IDs = %d, %d; expected same positive ID", firstID, secondID)
	}
	var workOrderCount, workOrderItemCount int
	var woTotal, quotedTotal, quotedDiscount float64
	var originalEstimateID int64
	if err := d.QueryRow(`SELECT COUNT(*), MAX(total_amount), MAX(quoted_total_amount),
		MAX(quoted_discount_amount), MAX(original_estimate_id) FROM work_orders`).
		Scan(&workOrderCount, &woTotal, &quotedTotal, &quotedDiscount, &originalEstimateID); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM work_order_items WHERE work_order_id = ?`, firstID).
		Scan(&workOrderItemCount); err != nil {
		t.Fatal(err)
	}
	if workOrderCount != 1 || workOrderItemCount != 2 || woTotal != 1150 ||
		quotedTotal != 1150 || quotedDiscount != 100 || originalEstimateID != estimateID {
		t.Fatalf("conversion trace: count=%d items=%d total=%.2f quote=%.2f discount=%.2f source=%d",
			workOrderCount, workOrderItemCount, woTotal, quotedTotal, quotedDiscount, originalEstimateID)
	}
	var status string
	var convertedID int64
	if err := d.QueryRow(`SELECT status, converted_work_order_id FROM estimates WHERE id = ?`,
		estimateID).Scan(&status, &convertedID); err != nil {
		t.Fatal(err)
	}
	if status != "converted" || convertedID != firstID {
		t.Fatalf("estimate status/source = %q/%d, want converted/%d", status, convertedID, firstID)
	}
}
