package handler

import (
	"strings"
	"testing"

	"autorepairos/internal/db"
)

// setupCampaignTestDB 建立行銷方案分析所需的最小 schema。
func setupCampaignTestDB(t *testing.T) {
	t.Helper()
	d, err := openTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	db.DB = d
	statements := []string{
		`CREATE TABLE work_orders (
			id INTEGER PRIMARY KEY, status TEXT, created_at TEXT,
			customer_id INTEGER, total_amount REAL, discount_amount REAL,
			campaign_code TEXT, deleted_at TEXT
		)`,
		`CREATE VIEW active_work_orders AS SELECT * FROM work_orders WHERE deleted_at IS NULL`,
		`CREATE TABLE customers (
			id INTEGER PRIMARY KEY, name TEXT, campaign_code TEXT, deleted_at TEXT
		)`,
		`CREATE VIEW active_customers AS SELECT * FROM customers WHERE deleted_at IS NULL`,
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
			t.Fatalf("setup campaign database: %v", err)
		}
	}
}

func execCampaignFixture(t *testing.T, q string, args ...interface{}) {
	t.Helper()
	if _, err := db.DB.Exec(q, args...); err != nil {
		t.Fatalf("exec failed: %v (q=%s)", err, q)
	}
}

// 測試基本方案分組與營收計算
func TestCampaignAnalysisBlockBasic(t *testing.T) {
	setupCampaignTestDB(t)

	// 建立客戶與工單
	execCampaignFixture(t, `INSERT INTO customers (id, name, campaign_code) VALUES
		(1, '客戶A', 'VIP'),
		(2, '客戶B', 'NEW')`)
	execCampaignFixture(t, `INSERT INTO work_orders (id, status, created_at, customer_id, total_amount, discount_amount, campaign_code) VALUES
		(1, 'paid', '2026-01-02 10:00:00', 1, 1000, 0, 'VIP'),
		(2, 'paid', '2026-01-03 10:00:00', 1, 500, 50, 'VIP'),
		(3, 'paid', '2026-01-04 10:00:00', 2, 800, 0, 'NEW')`)

	html := campaignAnalysisBlock("2026-01-01", "2026-01-31", false)

	// 驗證方案代碼分組
	if !strings.Contains(html, "VIP") {
		t.Error("HTML 應包含 VIP 方案")
	}
	if !strings.Contains(html, "NEW") {
		t.Error("HTML 應包含 NEW 方案")
	}

	// 驗證工單數與營收
	if !strings.Contains(html, "2") {
		t.Error("VIP 應有 2 筆工單")
	}
	if !strings.Contains(html, "1,450") && !strings.Contains(html, "1450") {
		t.Error("VIP 營收應為 1450 (1000 + 500 - 50)")
	}
	if !strings.Contains(html, "800") {
		t.Error("NEW 營收應為 800")
	}

	// showCost=false 時不應有成本欄位
	if strings.Contains(html, "成本") {
		t.Error("showCost=false 時不應顯示成本欄位")
	}
	if strings.Contains(html, "毛利") {
		t.Error("showCost=false 時不應顯示毛利欄位")
	}
}

// 測試 showCost=true 時顯示成本與毛利
func TestCampaignAnalysisBlockWithCost(t *testing.T) {
	setupCampaignTestDB(t)

	execCampaignFixture(t, `INSERT INTO customers (id, name, campaign_code) VALUES (1, '客戶A', 'VIP')`)
	execCampaignFixture(t, `INSERT INTO work_orders (id, status, created_at, customer_id, total_amount, discount_amount, campaign_code) VALUES
		(1, 'paid', '2026-01-02 10:00:00', 1, 1000, 0, 'VIP')`)
	execCampaignFixture(t, `INSERT INTO inventory_items (id, cost_price) VALUES (1, 50)`)
	execCampaignFixture(t, `INSERT INTO work_order_items (work_order_id, item_type, inventory_item_id, quantity, subtotal) VALUES
		(1, 'part', 1, 2, 200)`)
	execCampaignFixture(t, `INSERT INTO technician_worklogs (work_order_id, work_date, hours, commission_amount) VALUES
		(1, '2026-01-03', 2, 150)`)

	html := campaignAnalysisBlock("2026-01-01", "2026-01-31", true)

	// 應顯示成本與毛利欄位
	if !strings.Contains(html, "成本") {
		t.Error("showCost=true 時應顯示成本欄位")
	}
	if !strings.Contains(html, "毛利") {
		t.Error("showCost=true 時應顯示毛利欄位")
	}

	// 成本 = 零件 100 (50*2) + 工資 150 = 250
	// 毛利 = 1000 - 250 = 750
	if !strings.Contains(html, "250") && !strings.Contains(html, "未記錄") {
		t.Error("應顯示成本 250 或工資未記錄")
	}
}

// 空字串的方案代碼必須當成「未設定」，不可顯示成空白列。
// 實測踩到：直接寫入的舊資料是 ”（不是 NULL），COALESCE 不處理空字串，
// 畫面上就會出現一個沒有名稱的方案列。
func TestCampaignAnalysisBlockEmptyCodeFallsBackToUnset(t *testing.T) {
	setupCampaignTestDB(t)

	execCampaignFixture(t, `INSERT INTO customers (id, name, campaign_code) VALUES (1, '客戶A', '')`)
	execCampaignFixture(t, `INSERT INTO work_orders (id, status, created_at, customer_id, total_amount, discount_amount, campaign_code) VALUES
		(1, 'paid', '2026-01-02 10:00:00', 1, 500, 0, '')`)

	html := campaignAnalysisBlock("2026-01-01", "2026-01-31", false)

	if !strings.Contains(html, "(未設定)") {
		t.Error("空字串方案代碼應歸為「(未設定)」")
	}
	// 不得出現「<code></code>」這種空代碼欄
	if strings.Contains(html, "<code></code>") {
		t.Error("不應渲染空白的方案代碼欄")
	}
}

// 測試方案代碼優先順序：工單 > 客戶 > (未設定)
func TestCampaignAnalysisBlockPriority(t *testing.T) {
	setupCampaignTestDB(t)

	// 工單有代碼時優先使用工單的
	execCampaignFixture(t, `INSERT INTO customers (id, name, campaign_code) VALUES (1, '客戶A', 'CUSTOMER_CODE')`)
	execCampaignFixture(t, `INSERT INTO work_orders (id, status, created_at, customer_id, total_amount, discount_amount, campaign_code) VALUES
		(1, 'paid', '2026-01-02 10:00:00', 1, 1000, 0, 'WO_CODE')`)

	html := campaignAnalysisBlock("2026-01-01", "2026-01-31", false)

	if !strings.Contains(html, "WO_CODE") {
		t.Error("工單有代碼時應使用工單的代碼")
	}
	if strings.Contains(html, "CUSTOMER_CODE") {
		t.Error("工單有代碼時不應使用客戶的代碼")
	}

	// 工單無代碼時使用客戶的（需要新建一個客戶和工單來測試）
	execCampaignFixture(t, `INSERT INTO customers (id, name, campaign_code) VALUES (2, '客戶B', 'CUSTOMER_CODE')`)
	execCampaignFixture(t, `INSERT INTO work_orders (id, status, created_at, customer_id, total_amount, discount_amount, campaign_code) VALUES
		(2, 'paid', '2026-01-03 10:00:00', 2, 800, 0, NULL)`)

	html = campaignAnalysisBlock("2026-01-01", "2026-01-31", false)
	// 應該有 CUSTOMER_CODE 出現（工單 2）
	if !strings.Contains(html, "CUSTOMER_CODE") {
		t.Error("工單無代碼時應使用客戶的代碼")
	}

	// 兩者皆空時歸為 (未設定)
	execCampaignFixture(t, `INSERT INTO customers (id, name, campaign_code) VALUES (3, '客戶C', NULL)`)
	execCampaignFixture(t, `INSERT INTO work_orders (id, status, created_at, customer_id, total_amount, discount_amount, campaign_code) VALUES
		(3, 'paid', '2026-01-04 10:00:00', 3, 600, 0, NULL)`)

	html = campaignAnalysisBlock("2026-01-01", "2026-01-31", false)
	if !strings.Contains(html, "(未設定)") {
		t.Error("兩者皆空時應歸為 (未設定)")
	}
}

// 測試工資未記錄時顯示「未記錄」
func TestCampaignAnalysisBlockUnrecordedLabor(t *testing.T) {
	setupCampaignTestDB(t)

	execCampaignFixture(t, `INSERT INTO customers (id, name, campaign_code) VALUES (1, '客戶A', 'VIP')`)
	execCampaignFixture(t, `INSERT INTO work_orders (id, status, created_at, customer_id, total_amount, discount_amount, campaign_code) VALUES
		(1, 'paid', '2026-01-02 10:00:00', 1, 1000, 0, 'VIP')`)
	execCampaignFixture(t, `INSERT INTO inventory_items (id, cost_price) VALUES (1, 50)`)
	execCampaignFixture(t, `INSERT INTO work_order_items (work_order_id, item_type, inventory_item_id, quantity, subtotal) VALUES
		(1, 'part', 1, 2, 200)`)
	// 沒有 technician_worklogs

	html := campaignAnalysisBlock("2026-01-01", "2026-01-31", true)

	// 工資未記錄，成本應顯示「未記錄」而不是 0
	if !strings.Contains(html, "未記錄") {
		t.Error("工資未記錄時應顯示「未記錄」")
	}
}

// 測試期間內無工單時顯示空狀態
func TestCampaignAnalysisBlockEmpty(t *testing.T) {
	setupCampaignTestDB(t)

	html := campaignAnalysisBlock("2026-01-01", "2026-01-31", false)

	if !strings.Contains(html, "期間無已結帳工單") {
		t.Error("期間無工單時應顯示空狀態")
	}
}
