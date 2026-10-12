-- 016: inventory_items 表加 reserved_quantity 栏位（零件预扣量）
--
-- 为什么需要：
--   工单进行中加入零件时，帐面库存不变，其他工单会误判料件充足，
--   造成现场拆车后无料可装。预扣量机制让「可用库存 = 实际库存 - 预扣量」，
--   多工单并行领料时不会超卖。
--
-- 规则：
--   - 工单加入零件时：reserved_quantity += quantity
--   - 工单删除零件时：reserved_quantity -= quantity
--   - 工单结帐 paid 时：stock_quantity -= quantity 且 reserved_quantity -= quantity
--   - 可用库存 = stock_quantity - reserved_quantity

ALTER TABLE inventory_items ADD COLUMN reserved_quantity INTEGER NOT NULL DEFAULT 0;

-- 索引：库存列表筛选低库存时可用
CREATE INDEX IF NOT EXISTS idx_inventory_reserved ON inventory_items(reserved_quantity);
