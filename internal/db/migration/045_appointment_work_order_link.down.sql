-- 045 回滾：移除預約與工單的關聯。
--
-- SQLite 3.35 以前不支援 DROP COLUMN；這個 repo 用 modernc.org/sqlite，
-- 版本足夠，因此直接 DROP。先移除索引再移除欄位。
DROP INDEX IF EXISTS idx_work_orders_appointment;
ALTER TABLE work_orders DROP COLUMN appointment_id;
