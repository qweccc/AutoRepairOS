DROP VIEW IF EXISTS active_estimate_items;
ALTER TABLE estimate_items DROP COLUMN deleted_at;
