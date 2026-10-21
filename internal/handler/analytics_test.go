package handler

import (
	"context"
	"fmt"
	"math"
	"testing"

	"autorepairos/internal/db"
)

func TestLoadAnalyticsChartDataGroupsRevenueAndGrossProfit(t *testing.T) {
	setupAnalyticsChartTestDB(t)

	tests := []struct {
		groupBy string
		want    map[string]analyticsMonthTotal
	}{
		{
			groupBy: "kind",
			want: map[string]analyticsMonthTotal{
				"工資":   {Revenue: 30 + 90*40.0/110},
				"零件":   {Revenue: 90*50.0/110 + 30, Cost: 26},
				"外包加工": {Revenue: 90 * 20.0 / 110},
			},
		},
		{
			groupBy: "customer",
			want: map[string]analyticsMonthTotal{
				"VIP":       {Revenue: 45, Cost: 8},
				"returning": {Revenue: 45, Cost: 8},
				"Fleet":     {Revenue: 60, Cost: 10},
			},
		},
		{
			groupBy: "vehicle",
			want: map[string]analyticsMonthTotal{
				"自小客": {Revenue: 90, Cost: 16},
				"貨車":  {Revenue: 60, Cost: 10},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.groupBy, func(t *testing.T) {
			got, err := loadAnalyticsChartData(context.Background(), "2026-01-01", "2026-02-28", test.groupBy, true)
			if err != nil {
				t.Fatalf("load analytics chart: %v", err)
			}
			if len(got.Points) != 2 {
				t.Fatalf("got %d months, want 2", len(got.Points))
			}
			assertClose(t, parseChartAmount(t, got.Points[0].Revenue), 90)
			assertClose(t, parseChartAmount(t, got.Points[0].Cost), 76) // 零件 16 + 外包 20 + 工資 40
			assertClose(t, parseChartAmount(t, got.Points[0].Gross), 14)
			assertClose(t, parseChartAmount(t, got.Points[1].Revenue), 60)
			assertClose(t, parseChartAmount(t, got.Points[1].Cost), 30) // 零件 10 + 外包 0 + 工資 20
			assertClose(t, parseChartAmount(t, got.Points[1].Gross), 30)

			sums := make(map[string]analyticsMonthTotal, len(got.Groups))
			for _, group := range got.Groups {
				sums[group.Label] = analyticsMonthTotal{
					Revenue: parseChartAmount(t, group.Revenue),
					Cost:    parseChartAmount(t, group.Cost),
				}
			}
			for label, want := range test.want {
				total, ok := sums[label]
				if !ok {
					t.Fatalf("missing group %q; got %#v", label, sums)
				}
				assertClose(t, total.Revenue, want.Revenue)
				assertClose(t, total.Cost, want.Cost)
				assertClose(t, total.Revenue-total.Cost, want.Revenue-want.Cost)
			}
			var revenue, cost float64
			for _, total := range sums {
				revenue += total.Revenue
				cost += total.Cost
			}
			assertClose(t, revenue, 150)
			assertClose(t, cost, 26)
			assertClose(t, revenue-cost, 124)
		})
	}
}

func TestLoadAnalyticsChartDataHidesCostForCounter(t *testing.T) {
	setupAnalyticsChartTestDB(t)
	got, err := loadAnalyticsChartData(context.Background(), "2026-01-01", "2026-02-28", "vehicle", false)
	if err != nil {
		t.Fatalf("load analytics chart: %v", err)
	}
	for _, point := range got.Points {
		if point.Cost != "0.00" {
			t.Errorf("counter chart cost = %q, want hidden zero value", point.Cost)
		}
	}
	for _, group := range got.Groups {
		if group.Cost != "0.00" {
			t.Errorf("counter chart group cost = %q, want hidden zero value", group.Cost)
		}
	}
}

func setupAnalyticsChartTestDB(t *testing.T) {
	t.Helper()
	d, err := openTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	db.DB = d
	statements := []string{
		`CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT, tags TEXT, deleted_at TEXT)`,
		`CREATE VIEW active_customers AS SELECT * FROM customers WHERE deleted_at IS NULL`,
		`CREATE TABLE vehicles (id INTEGER PRIMARY KEY, vehicle_type TEXT, deleted_at TEXT)`,
		`CREATE VIEW active_vehicles AS SELECT * FROM vehicles WHERE deleted_at IS NULL`,
		`CREATE TABLE work_orders (
			id INTEGER PRIMARY KEY, customer_id INTEGER, vehicle_id INTEGER, status TEXT,
			created_at TEXT, discount_amount REAL, total_amount REAL, deleted_at TEXT
		)`,
		`CREATE VIEW active_work_orders AS SELECT * FROM work_orders WHERE deleted_at IS NULL`,
		`CREATE TABLE work_order_items (
			id INTEGER PRIMARY KEY, work_order_id INTEGER, item_type TEXT,
			inventory_item_id INTEGER, quantity INTEGER, subtotal REAL
		)`,
		`CREATE TABLE inventory_items (id INTEGER PRIMARY KEY, cost_price REAL, deleted_at TEXT)`,
		`CREATE VIEW active_inventory_items AS SELECT * FROM inventory_items WHERE deleted_at IS NULL`,
		`CREATE TABLE receivables (
			id INTEGER PRIMARY KEY, work_order_id INTEGER, voided INTEGER,
			cash REAL, card REAL, check_amount REAL, transfer_amount REAL
		)`,
		`CREATE TABLE receivable_payments (receivable_id INTEGER, work_order_id INTEGER)`,
		`CREATE TABLE lookup_items (category TEXT, code TEXT, label TEXT, sort_order INTEGER)`,
		`CREATE TABLE technician_worklogs (
			id INTEGER PRIMARY KEY, work_order_id INTEGER, technician_id INTEGER,
			work_date TEXT, hours REAL, commission_amount REAL, deleted_at TEXT
		)`,
		`CREATE VIEW active_technician_worklogs AS SELECT * FROM technician_worklogs WHERE deleted_at IS NULL`,
		`INSERT INTO customers (id, name, tags) VALUES (1, '甲客戶', 'VIP, returning'), (2, '乙客戶', 'Fleet')`,
		`INSERT INTO vehicles (id, vehicle_type) VALUES (1, '01'), (2, '02')`,
		`INSERT INTO lookup_items (category, code, label, sort_order)
			VALUES ('vehicle_type', '01', '自小客', 1), ('vehicle_type', '02', '貨車', 2)`,
		`INSERT INTO work_orders (id, customer_id, vehicle_id, status, created_at, discount_amount, total_amount)
			VALUES (1, 1, 1, 'paid', '2026-01-02 12:00:00', 10, 100),
				(2, 2, 2, 'paid', '2026-02-04 12:00:00', 0, 60)`,
		`INSERT INTO inventory_items (id, cost_price) VALUES (1, 8), (2, 5)`,
		`INSERT INTO work_order_items (work_order_id, item_type, inventory_item_id, quantity, subtotal)
			VALUES (1, 'labor', NULL, 1, 40), (1, 'part', 1, 2, 50), (1, 'subcontract', NULL, 1, 20),
				(2, 'labor', NULL, 1, 30), (2, 'part', 2, 2, 30)`,
		// T2.13：工資成本（技師佣金）以工時月歸屬。工單 1 的工時在 1 月、
		// 工單 2 的在 2 月，正好與各自的工單建立月相同，方便驗證歸屬。
		`INSERT INTO technician_worklogs (work_order_id, work_date, hours, commission_amount)
			VALUES (1, '2026-01-10', 2, 40), (2, '2026-02-10', 2, 20)`,
	}
	for _, statement := range statements {
		if _, err := d.Exec(statement); err != nil {
			t.Fatalf("setup analytics chart database: %v", err)
		}
	}
}

func parseChartAmount(t *testing.T, value string) float64 {
	t.Helper()
	var result float64
	if _, err := fmt.Sscanf(value, "%f", &result); err != nil {
		t.Fatalf("parse chart amount %q: %v", value, err)
	}
	return result
}

func assertClose(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.01 {
		t.Errorf("got %.2f, want %.2f", got, want)
	}
}
