-- 工單明細軟刪除（SRS：重要營業資料與工單明細不得物理刪除）。
--
-- 既有删除路徑（移除單一項目）原本直接 DELETE，已刪項目會從明細、收據、
-- 成本統計中永遠消失。改成標記 deleted_at，並提供 active_work_order_items
-- 視給所有讀取路径使用（與 029 的 active_* 表同一套模式）。
ALTER TABLE work_order_items ADD COLUMN deleted_at TEXT;

CREATE VIEW IF NOT EXISTS active_work_order_items AS
    SELECT * FROM work_order_items WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_work_order_items_deleted_at ON work_order_items(deleted_at);
