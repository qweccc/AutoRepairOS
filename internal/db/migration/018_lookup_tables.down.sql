-- 回滚 018：移除 lookup_items 表。

DROP INDEX IF EXISTS idx_lookup_active;
DROP INDEX IF EXISTS idx_lookup_category;
DROP TABLE IF EXISTS lookup_items;
