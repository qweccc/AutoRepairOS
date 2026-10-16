-- 027: 加速依零件查詢庫存異動
-- 調庫與進貨會在交易內寫入異動紀錄，零件異動歷史依 inventory_item_id 讀取。
CREATE INDEX IF NOT EXISTS idx_inventory_movements_item_id_id
	ON inventory_movements (inventory_item_id, id DESC);
