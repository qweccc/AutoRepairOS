-- 回滚 016：移除 inventory_items.reserved_quantity 栏位。

-- DROP INDEX IF EXISTS idx_inventory_reserved;
-- ALTER TABLE inventory_items DROP COLUMN reserved_quantity;
