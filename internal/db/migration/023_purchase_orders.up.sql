-- 023: 進貨單（採購進貨紀錄）
--
-- 為什麼需要：
--   車行向零件商進貨時，需要記錄「這批進了什麼、各多少錢、總共要給廠商多少」。
--   舊系統與現行 inventory 都沒有進貨單據（print_docs.go 已註記此缺口），
--   導致無法對帳、無法追蹤進貨成本。本表補上這個單據。
--
-- 設計：
--   1. purchase_orders 主檔：供應商、進貨日、狀態、總額（自動由品項彙總）
--   2. purchase_order_items 品項明細：可指向 inventory_items（進貨後入庫）
--      或獨立品項（尚未建檔的零件），數量 × 單價 = 小計
--   3. 狀態：draft 草稿 / received 已進貨（received 時把數量加回庫存）

-- 進貨單主檔
CREATE TABLE IF NOT EXISTS purchase_orders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    supplier_id INTEGER NOT NULL,
    order_date TEXT DEFAULT '',
    status TEXT DEFAULT 'draft',           -- draft / received
    total_amount REAL DEFAULT 0,           -- 品項小計總和（要給廠商的錢）
    notes TEXT DEFAULT '',
    created_by INTEGER DEFAULT 0,
    created_at TEXT DEFAULT '',
    received_at TEXT DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_po_supplier ON purchase_orders(supplier_id);
CREATE INDEX IF NOT EXISTS idx_po_status ON purchase_orders(status);

-- 進貨單品項明細
CREATE TABLE IF NOT EXISTS purchase_order_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id INTEGER NOT NULL,
    inventory_item_id INTEGER DEFAULT 0,   -- 0 = 尚未建檔的臨時品項
    description TEXT DEFAULT '',           -- 品項名稱（獨立品項或快照）
    part_number TEXT DEFAULT '',           -- 料號快照（對帳用）
    quantity REAL DEFAULT 0,
    unit_cost REAL DEFAULT 0,              -- 進貨單價（給廠商的價）
    subtotal REAL DEFAULT 0                -- quantity × unit_cost
);
CREATE INDEX IF NOT EXISTS idx_poi_order ON purchase_order_items(order_id);
