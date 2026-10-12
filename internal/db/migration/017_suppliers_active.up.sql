-- 017: suppliers 表加 active 栏位（软删除）
--
-- 为什么需要：
--   供应商被库存零件引用时不能真删，否则历史工单的供应商资讯会消失。
--   用 active=0 标记停用，列表预设只显示 active=1。

ALTER TABLE suppliers ADD COLUMN active INTEGER NOT NULL DEFAULT 1;

CREATE INDEX IF NOT EXISTS idx_suppliers_active ON suppliers(active);
