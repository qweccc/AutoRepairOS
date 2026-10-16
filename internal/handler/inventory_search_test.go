package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

func TestSearchInventoryAPICostVisibility(t *testing.T) {
	for _, role := range []struct {
		name       string
		canSeeCost bool
	}{
		{name: "owner", canSeeCost: true},
		{name: "counter", canSeeCost: true},
		{name: "technician", canSeeCost: false},
	} {
		t.Run(role.name, func(t *testing.T) {
			d, err := openTestDB(t)
			if err != nil {
				t.Fatal(err)
			}
			db.DB = d
			_, err = d.Exec(`CREATE TABLE inventory_items (
				id INTEGER PRIMARY KEY,
				supplier_id INTEGER,
				name TEXT NOT NULL,
				part_number TEXT NOT NULL,
				barcode TEXT,
				category TEXT,
				bin_location TEXT,
				cost_price REAL,
				selling_price REAL,
				stock_quantity INTEGER,
				min_safety_stock INTEGER,
				item_kind TEXT,
				deleted_at TEXT
			)`)
			if err != nil {
				t.Fatal(err)
			}
			_, err = d.Exec(`CREATE VIEW active_inventory_items AS
				SELECT * FROM inventory_items WHERE deleted_at IS NULL`)
			if err != nil {
				t.Fatal(err)
			}
			_, err = d.Exec(`INSERT INTO inventory_items
				(id, name, part_number, cost_price, selling_price, stock_quantity, min_safety_stock, item_kind)
				VALUES (1, 'Test Part', 'TEST-1', 98765, 4321, 4, 2, 'part')`)
			if err != nil {
				t.Fatal(err)
			}
			_, err = d.Exec(`CREATE TABLE suppliers (id INTEGER PRIMARY KEY, name TEXT, deleted_at TEXT)`)
			if err != nil {
				t.Fatal(err)
			}
			_, err = d.Exec(`CREATE VIEW active_suppliers AS
				SELECT * FROM suppliers WHERE deleted_at IS NULL`)
			if err != nil {
				t.Fatal(err)
			}

			req := httptest.NewRequest("GET", "/api/inventory/search?q=Test", nil)
			rec := httptest.NewRecorder()
			SearchInventoryAPI(rec, req, model.User{Role: role.name})

			if rec.Code != 200 {
				t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if got := strings.Contains(body, "$98765"); got != role.canSeeCost {
				t.Errorf("response contains cost price = %t, want %t; body: %s", got, role.canSeeCost, body)
			}
			if !strings.Contains(body, "$4321") {
				t.Errorf("response must continue to include selling price; body: %s", body)
			}
		})
	}
}
