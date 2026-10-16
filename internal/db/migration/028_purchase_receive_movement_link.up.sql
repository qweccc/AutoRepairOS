-- 028: 將進貨異動連結到進貨單品項，資料庫層防止同一品項重複入庫留痕。
ALTER TABLE inventory_movements
	ADD COLUMN purchase_order_item_id INTEGER REFERENCES purchase_order_items(id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_inventory_movements_purchase_item
	ON inventory_movements (purchase_order_item_id)
	WHERE purchase_order_item_id IS NOT NULL;
