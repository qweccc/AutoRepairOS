package handler

import (
	"database/sql"
	"strings"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// setupPurchaseTestDB 建 purchase_orders / purchase_order_items / inventory_items /
// inventory_movements 四張表的記憶體 DB，隔離測試自動建檔流程。
func setupPurchaseTestDB(t *testing.T) {
	t.Helper()
	d, err := openTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	db.DB = d
	createPurchaseTestSchema(t, d)
}

func createPurchaseTestSchema(t *testing.T, d *sql.DB) {
	t.Helper()
	stmts := []string{
		`CREATE TABLE suppliers (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, deleted_at TEXT)`,
		`CREATE TABLE purchase_orders (id INTEGER PRIMARY KEY AUTOINCREMENT,
			supplier_id INTEGER, order_date TEXT, status TEXT, total_amount REAL DEFAULT 0,
			notes TEXT, created_by INTEGER, created_at TEXT, received_at TEXT, deleted_at TEXT)`,
		`CREATE TABLE purchase_order_items (id INTEGER PRIMARY KEY AUTOINCREMENT,
			order_id INTEGER NOT NULL, inventory_item_id INTEGER DEFAULT 0,
			description TEXT DEFAULT '', part_number TEXT DEFAULT '', category TEXT DEFAULT '',
			quantity REAL DEFAULT 0, unit_cost REAL DEFAULT 0, subtotal REAL DEFAULT 0,
			deleted_at TEXT)`,
		`CREATE TABLE inventory_items (id INTEGER PRIMARY KEY AUTOINCREMENT,
			supplier_id INTEGER, part_number TEXT NOT NULL UNIQUE, barcode TEXT,
			name TEXT NOT NULL, bin_location TEXT NOT NULL, category TEXT DEFAULT '',
			item_kind TEXT NOT NULL DEFAULT 'part', cost_price REAL DEFAULT 0,
			selling_price REAL DEFAULT 0, stock_quantity INTEGER DEFAULT 0,
			min_safety_stock INTEGER DEFAULT 5, deleted_at TEXT)`,
		`CREATE TABLE inventory_movements (id INTEGER PRIMARY KEY AUTOINCREMENT,
			inventory_item_id INTEGER NOT NULL, delta INTEGER NOT NULL,
			before_quantity INTEGER DEFAULT 0, after_quantity INTEGER DEFAULT 0,
			reason TEXT DEFAULT '', source TEXT NOT NULL DEFAULT 'manual',
			work_order_id INTEGER, created_by INTEGER, purchase_order_item_id INTEGER)`,
		`CREATE VIEW active_suppliers AS SELECT * FROM suppliers WHERE deleted_at IS NULL`,
		`CREATE VIEW active_purchase_orders AS SELECT * FROM purchase_orders WHERE deleted_at IS NULL`,
		`CREATE VIEW active_purchase_order_items AS SELECT * FROM purchase_order_items WHERE deleted_at IS NULL`,
		`CREATE VIEW active_inventory_items AS SELECT * FROM inventory_items WHERE deleted_at IS NULL`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.Exec(`CREATE UNIQUE INDEX idx_inventory_movements_purchase_item
		ON inventory_movements (purchase_order_item_id)
		WHERE purchase_order_item_id IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
}

// 注意：openTestDB 用 MaxOpenConns(1)，記憶體 DB 的 tx 會獨佔唯一連線。
// 因此不可「tx 開著時又用 db.DB 查詢」——會死鎖。
// 流程一律：先用 db.DB 備好前置資料 → 開 tx → 呼叫待測函式 → commit → 再用 db.DB 驗證。

// TestAutoCreatePartFromItem 驗證：類別帶入、料號自動產生、UNIQUE 碰撞換後綴。
func TestAutoCreatePartFromItem(t *testing.T) {
	setupPurchaseTestDB(t)

	// 前置：既有零件佔用 OIL-530 與 PO-1-11，用來測碰撞換後綴。
	db.DB.Exec(`INSERT INTO inventory_items (part_number, name, bin_location) VALUES ('OIL-530','既有','A-1')`)
	db.DB.Exec(`INSERT INTO inventory_items (part_number, name, bin_location) VALUES ('PO-1-11','佔位','A-2')`)
	// 一張單、兩個品項：一個有料號有類別、一個沒料號沒類別。
	db.DB.Exec(`INSERT INTO purchase_orders (id, supplier_id, order_date, status) VALUES (1, 7, '2026-10-14', 'draft')`)
	db.DB.Exec(`INSERT INTO purchase_order_items (id, order_id, description, part_number, category, quantity, unit_cost)
		VALUES (10, 1, '機油 5W-30', 'OIL-530', '引擎', 4, 650),
		       (11, 1, '神秘零件', '', '', 2, 100)`)

	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}

	// 品項 10：料號 OIL-530（已存在）→ 換後綴 OIL-530-1；類別引擎。
	it10 := model.PurchaseOrderItem{ID: 10, Description: "機油 5W-30", PartNumber: "OIL-530", UnitCost: 650}
	id10, err := autoCreatePartFromItem(tx, 1, 7, it10)
	if err != nil {
		t.Fatal(err)
	}
	// 品項 11：沒料號 → 產生 PO-1-11（已存在）→ 換後綴 PO-1-11-1；無類別。
	it11 := model.PurchaseOrderItem{ID: 11, Description: "神秘零件", PartNumber: "", UnitCost: 100}
	id11, err := autoCreatePartFromItem(tx, 1, 7, it11)
	if err != nil {
		t.Fatal(err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// 驗證品項 10 建的零件。
	var part10, name10, cat10, bin10 string
	var cost10 float64
	var sup10 int64
	db.DB.QueryRow(`SELECT part_number, name, category, bin_location, cost_price, supplier_id
		FROM inventory_items WHERE id = ?`, id10).Scan(&part10, &name10, &cat10, &bin10, &cost10, &sup10)
	if part10 != "OIL-530-1" || name10 != "機油 5W-30" || cat10 != "引擎" || bin10 != "待歸位" {
		t.Errorf("品項10零件不正確：part=%q name=%q cat=%q bin=%q", part10, name10, cat10, bin10)
	}
	if cost10 != 650 || sup10 != 7 {
		t.Errorf("成本/供應商應帶入：cost=%v sup=%d", cost10, sup10)
	}

	// 驗證品項 11 建的零件。
	var part11, cat11 string
	db.DB.QueryRow(`SELECT part_number, category FROM inventory_items WHERE id = ?`, id11).Scan(&part11, &cat11)
	if part11 != "PO-1-11-1" {
		t.Errorf("碰撞應換後綴成 PO-1-11-1，got %q", part11)
	}
	if cat11 != "" {
		t.Errorf("沒猜到的類別應為空，got %q", cat11)
	}

	// 原本兩顆佔位零件不該被改動。
	var n int
	db.DB.QueryRow(`SELECT COUNT(1) FROM inventory_items WHERE part_number IN ('OIL-530','PO-1-11')`).Scan(&n)
	if n != 2 {
		t.Errorf("原有佔位零件不該被動到，剩 %d 筆", n)
	}
}

// TestAutoCreatePartCategoryFromDB 類別從 purchase_order_items.category 讀（確認進貨路徑）。
func TestAutoCreatePartCategoryFromDB(t *testing.T) {
	setupPurchaseTestDB(t)
	db.DB.Exec(`INSERT INTO purchase_orders (id, supplier_id, order_date, status) VALUES (2, 1, '2026-10-14', 'draft')`)
	db.DB.Exec(`INSERT INTO purchase_order_items (id, order_id, description, category, quantity, unit_cost)
		VALUES (20, 2, '輪胎 195/65R15', '輪胎', 4, 3200)`)

	tx, _ := db.DB.Begin()
	it := model.PurchaseOrderItem{ID: 20, Description: "輪胎 195/65R15", UnitCost: 3200}
	id, err := autoCreatePartFromItem(tx, 2, 1, it)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var cat, part string
	db.DB.QueryRow(`SELECT category, part_number FROM inventory_items WHERE id = ?`, id).Scan(&cat, &part)
	if cat != "輪胎" {
		t.Errorf("類別應從 purchase_order_items.category 帶入「輪胎」，got %q", cat)
	}
	if !strings.HasPrefix(part, "PO-2-20") {
		t.Errorf("料號應以 PO-2-20 開頭，got %q", part)
	}
}
