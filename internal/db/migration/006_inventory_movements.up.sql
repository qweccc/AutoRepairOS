-- 庫存異動紀錄
--
-- 為什麼需要：舊系統 CAR35 的庫存有 38.7% 是負數（進貨未登錄就直接扣料），
-- 匯入後必須能補正，而且補正要留痕 —— 否則日後沒人知道某筆庫存
-- 為什麼從 -117 變成 0、是誰改的、什麼時候改的。
--
-- 設計：
--   - delta      異動量（正數進貨、負數領用/補正）
--   - before/after 異動前後的庫存，方便對帳時一眼看出變化
--   - reason     異動原因，legacy_fix 專門標記「舊系統負庫存補正」
--   - source     來源：manual（手動調庫）/ checkout（結帳扣料）/
--                legacy_fix（補正）/ import（匯入）
--
-- 對應 CAR35 的 Carp62（庫存異動）/ Carp84（進出明細）。

CREATE TABLE IF NOT EXISTS inventory_movements (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    inventory_item_id INTEGER NOT NULL REFERENCES inventory_items(id),
    delta INTEGER NOT NULL,
    before_quantity INTEGER NOT NULL DEFAULT 0,
    after_quantity INTEGER NOT NULL DEFAULT 0,
    reason TEXT DEFAULT '',
    source TEXT NOT NULL DEFAULT 'manual',
    work_order_id INTEGER REFERENCES work_orders(id),
    created_by INTEGER REFERENCES users(id),
    created_at TEXT NOT NULL DEFAULT (datetime('now','localtime'))
);

CREATE INDEX IF NOT EXISTS idx_inv_mov_item ON inventory_movements(inventory_item_id);
CREATE INDEX IF NOT EXISTS idx_inv_mov_created ON inventory_movements(created_at);
CREATE INDEX IF NOT EXISTS idx_inv_mov_source ON inventory_movements(source);

-- 補正紀錄的專用欄位：記錄補正前的原始值，方便必要時回復
ALTER TABLE inventory_items ADD COLUMN stock_corrected_at TEXT DEFAULT '';
ALTER TABLE inventory_items ADD COLUMN stock_corrected_from INTEGER;
