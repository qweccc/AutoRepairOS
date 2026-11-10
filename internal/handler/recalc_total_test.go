package handler

import (
	"context"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/service"
)

func TestRecalcWorkOrderTotal(t *testing.T) {
	d, err := openTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	db.DB = d
	stmts := []string{
		`CREATE TABLE work_orders (id INTEGER PRIMARY KEY, total_amount REAL, updated_at TEXT)`,
		`CREATE TABLE work_order_items (id INTEGER PRIMARY KEY, work_order_id INTEGER, quantity INTEGER, unit_price REAL, subtotal REAL)`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.Exec(`INSERT INTO work_orders (id, total_amount) VALUES (1, 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO work_order_items (work_order_id, quantity, unit_price, subtotal) VALUES (1, 2, 10, 20), (1, 1, 5, NULL)`); err != nil {
		t.Fatal(err)
	}
	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RecalculateWorkOrderTotal(context.Background(), tx, 1); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var total float64
	if err := d.QueryRow(`SELECT total_amount FROM work_orders WHERE id=1`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 25 {
		t.Fatalf("expected 25, got %v", total)
	}
}
