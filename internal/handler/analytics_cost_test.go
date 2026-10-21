package handler

import (
	"testing"

	"autorepairos/internal/db"
)

// setupAnalyticsCostTestDB 建立成本口徑所需的最小 schema。
// 與 analytics_test.go 的 fixture 分開，避免兩邊互相牽動。
func setupAnalyticsCostTestDB(t *testing.T) {
	t.Helper()
	d, err := openTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	db.DB = d
	statements := []string{
		`CREATE TABLE work_orders (
			id INTEGER PRIMARY KEY, status TEXT, created_at TEXT, deleted_at TEXT,
			total_amount REAL, discount_amount REAL, is_warranty INTEGER
		)`,
		`CREATE VIEW active_work_orders AS SELECT * FROM work_orders WHERE deleted_at IS NULL`,
		`CREATE TABLE work_order_items (
			id INTEGER PRIMARY KEY, work_order_id INTEGER, item_type TEXT,
			inventory_item_id INTEGER, quantity INTEGER, subtotal REAL
		)`,
		`CREATE TABLE inventory_items (id INTEGER PRIMARY KEY, cost_price REAL, deleted_at TEXT)`,
		`CREATE VIEW active_inventory_items AS SELECT * FROM inventory_items WHERE deleted_at IS NULL`,
		`CREATE TABLE technician_worklogs (
			id INTEGER PRIMARY KEY, work_order_id INTEGER, technician_id INTEGER,
			work_date TEXT, hours REAL, commission_amount REAL, deleted_at TEXT
		)`,
		`CREATE VIEW active_technician_worklogs AS SELECT * FROM technician_worklogs WHERE deleted_at IS NULL`,
	}
	for _, statement := range statements {
		if _, err := d.Exec(statement); err != nil {
			t.Fatalf("setup analytics cost database: %v", err)
		}
	}
}

func execCostFixture(t *testing.T, q string, args ...interface{}) {
	t.Helper()
	if _, err := db.DB.Exec(q, args...); err != nil {
		t.Fatalf("exec failed: %v (q=%s)", err, q)
	}
}

// 期間成本：零件取 cost_price×數量、外包取明細小計、工資取佣金。
func TestAnalyticsPeriodCostsSumsThreeCostTypes(t *testing.T) {
	setupAnalyticsCostTestDB(t)

	execCostFixture(t, `INSERT INTO work_orders (id, status, created_at) VALUES
		(1, 'paid', '2026-01-02 10:00:00'), (2, 'paid', '2026-01-20 10:00:00'),
		(3, 'pending', '2026-01-21 10:00:00')`)
	execCostFixture(t, `INSERT INTO inventory_items (id, cost_price) VALUES (1, 8), (2, 50)`)
	execCostFixture(t, `INSERT INTO work_order_items (work_order_id, item_type, inventory_item_id, quantity, subtotal) VALUES
		(1, 'part', 1, 2, 50), (1, 'subcontract', NULL, 1, 20), (1, 'labor', NULL, 1, 40),
		(2, 'part', 2, 1, 90), (3, 'part', 1, 5, 999)`)
	// 工單 1 的工時落在期間內、工單 2 的工時跨到下一期（驗證歸屬以 work_date 為準）。
	execCostFixture(t, `INSERT INTO technician_worklogs (work_order_id, work_date, hours, commission_amount) VALUES
		(1, '2026-01-05', 2, 120), (2, '2026-02-05', 2, 300)`)

	got := analyticsPeriodCosts("2026-01-01", "2026-01-31")
	assertClose(t, got.PartsCost, 66)       // 8*2 + 50*1（未結帳的工單 3 不計）
	assertClose(t, got.SubcontractCost, 20) // 只有工單 1 有外包
	assertClose(t, got.LaborCost, 120)      // 工單 2 的工時在 2 月，不歸本期
	assertClose(t, got.Total(), 206)
	if !got.LaborRecorded {
		t.Error("期間內有工時紀錄，LaborRecorded 應為 true")
	}
}

// 查不到料號的零件不計成本（回 0 而非造假）。
func TestAnalyticsPeriodCostsSkipsUnknownPartCost(t *testing.T) {
	setupAnalyticsCostTestDB(t)
	execCostFixture(t, `INSERT INTO work_orders (id, status, created_at) VALUES (1, 'paid', '2026-01-02 10:00:00')`)
	execCostFixture(t, `INSERT INTO work_order_items (work_order_id, item_type, inventory_item_id, quantity, subtotal)
		VALUES (1, 'part', 999, 3, 100)`)

	got := analyticsPeriodCosts("2026-01-01", "2026-01-31")
	assertClose(t, got.PartsCost, 0)
	if got.LaborRecorded {
		t.Error("沒有工時紀錄時 LaborRecorded 應為 false")
	}
}

// 單張工單成本：缺工時紀錄必須標記「未記錄」，不可用 0 冒充。
func TestWorkOrderCostByIDMarksUnrecordedLabor(t *testing.T) {
	setupAnalyticsCostTestDB(t)
	execCostFixture(t, `INSERT INTO work_orders (id, status, created_at) VALUES (1, 'paid', '2026-01-02 10:00:00')`)
	execCostFixture(t, `INSERT INTO inventory_items (id, cost_price) VALUES (1, 10)`)
	execCostFixture(t, `INSERT INTO work_order_items (work_order_id, item_type, inventory_item_id, quantity, subtotal)
		VALUES (1, 'part', 1, 2, 100), (1, 'subcontract', NULL, 1, 30)`)

	got := workOrderCostByID(1)
	assertClose(t, got.PartsCost, 20)
	assertClose(t, got.SubcontractCost, 30)
	if got.LaborRecorded {
		t.Error("工單 1 無工時紀錄，LaborRecorded 應為 false")
	}
	if label := costLabel(got.LaborRecorded, got.LaborCost); label != "未記錄" {
		t.Errorf("無工時紀錄應顯示「未記錄」，得到 %q", label)
	}

	// 補上工時後，同樣是 0 也要顯示「0」而不是「未記錄」。
	execCostFixture(t, `INSERT INTO technician_worklogs (work_order_id, work_date, hours, commission_amount)
		VALUES (1, '2026-01-03', 1, 0)`)
	got = workOrderCostByID(1)
	if !got.LaborRecorded {
		t.Fatal("補上工時後 LaborRecorded 應為 true")
	}
	if label := costLabel(got.LaborRecorded, got.LaborCost); label == "未記錄" {
		t.Error("已有工時紀錄且佣金為 0 時應顯示 0，不是「未記錄」")
	}
}

// 保固收入必須用「折扣後」金額，與損益分拆、方案毛利同口徑——
// 若用未折扣金額，同一張保固工單在「保固毛利」與「損益毛利」會算出兩個不同數字。
func TestWarrantyStatsUsesNetRevenue(t *testing.T) {
	setupAnalyticsCostTestDB(t)
	// 保固工單：總額 1000、折讓 250 → 淨收入應為 750
	execCostFixture(t, `INSERT INTO work_orders (id, status, created_at, is_warranty, total_amount, discount_amount)
		VALUES (1, 'paid', '2026-01-05 10:00:00', 1, 1000, 250)`)
	// 非保固工單：總額 500、折讓 50 → 不應計入保固
	execCostFixture(t, `INSERT INTO work_orders (id, status, created_at, is_warranty, total_amount, discount_amount)
		VALUES (2, 'paid', '2026-01-06 10:00:00', 0, 500, 50)`)

	count, revenue, discount := warrantyStats("2026-01-01", "2026-01-31")
	if count != 1 {
		t.Errorf("保固工單數 = %d，應為 1", count)
	}
	assertClose(t, revenue, 750) // 1000-250，不是 1000
	assertClose(t, discount, 250)
}

// 保固成本：三類成本加總，且缺工時紀錄時標記未記錄。
func TestWarrantyCostTotals(t *testing.T) {
	setupAnalyticsCostTestDB(t)
	execCostFixture(t, `INSERT INTO work_orders (id, status, created_at, is_warranty, total_amount, discount_amount)
		VALUES (1, 'paid', '2026-01-05 10:00:00', 1, 1000, 0)`)
	execCostFixture(t, `INSERT INTO inventory_items (id, cost_price) VALUES (1, 100)`)
	execCostFixture(t, `INSERT INTO work_order_items (work_order_id, item_type, inventory_item_id, quantity, subtotal)
		VALUES (1, 'part', 1, 2, 300), (1, 'subcontract', NULL, 1, 200)`)

	total, recorded := warrantyCostTotals("2026-01-01", "2026-01-31")
	assertClose(t, total, 400) // 零件 200 + 外包 200
	if recorded {
		t.Error("沒有工時紀錄時 recorded 應為 false（保固成本要顯示「未記錄」）")
	}

	execCostFixture(t, `INSERT INTO technician_worklogs (work_order_id, work_date, hours, commission_amount)
		VALUES (1, '2026-01-06', 2, 60)`)
	total, recorded = warrantyCostTotals("2026-01-01", "2026-01-31")
	assertClose(t, total, 460) // 400 + 工資 60
	if !recorded {
		t.Error("補上工時後 recorded 應為 true")
	}
}

// 批次查詢：多張工單的成本一次取回，且缺資料的工單不在 map 內。
func TestWorkOrderCostsByIDs(t *testing.T) {
	setupAnalyticsCostTestDB(t)
	execCostFixture(t, `INSERT INTO work_orders (id, status, created_at) VALUES (1, 'paid', '2026-01-02 10:00:00')`)
	execCostFixture(t, `INSERT INTO inventory_items (id, cost_price) VALUES (1, 10)`)
	execCostFixture(t, `INSERT INTO work_order_items (work_order_id, item_type, inventory_item_id, quantity, subtotal)
		VALUES (1, 'part', 1, 3, 90)`)
	execCostFixture(t, `INSERT INTO technician_worklogs (work_order_id, work_date, hours, commission_amount)
		VALUES (1, '2026-01-03', 1, 45)`)

	got := workOrderCostsByIDs([]int64{1, 2})
	if len(got) != 1 {
		t.Fatalf("只應回傳有資料的 1 筆，得到 %d 筆", len(got))
	}
	c := got[1]
	assertClose(t, c.PartsCost, 30)
	assertClose(t, c.LaborCost, 45)
	if !c.LaborRecorded {
		t.Error("LaborRecorded 應為 true")
	}
}
