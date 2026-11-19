DROP VIEW IF EXISTS active_work_order_items;
ALTER TABLE work_order_items DROP COLUMN deleted_at;
