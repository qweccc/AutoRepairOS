-- 043 回滾：移除 LINE 數位維修履歷與保養收據表。
DROP INDEX IF EXISTS idx_passbook_status;
DROP INDEX IF EXISTS idx_passbook_work_order;
DROP INDEX IF EXISTS idx_passbook_vehicle;
DROP INDEX IF EXISTS idx_passbook_customer;
DROP TABLE IF EXISTS passbook;
