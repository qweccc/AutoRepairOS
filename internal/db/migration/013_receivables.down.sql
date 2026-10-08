-- 回滾 013：移除收款沖銷相關表。
--
-- 注意：這會刪掉所有收款紀錄（含從舊系統遷移的 90,762 筆歷史）。
-- 若只是要重新遷移舊資料，請改用
--   DELETE FROM receivables WHERE source='legacy'
-- 而不是跑這個 down migration。

DROP INDEX IF EXISTS idx_recv_pay_work_order;
DROP INDEX IF EXISTS idx_recv_pay_receivable;
DROP TABLE IF EXISTS receivable_payments;

DROP INDEX IF EXISTS idx_receivables_source;
DROP INDEX IF EXISTS idx_receivables_voucher;
DROP INDEX IF EXISTS idx_receivables_pay_date;
DROP INDEX IF EXISTS idx_receivables_work_order;
DROP INDEX IF EXISTS idx_receivables_customer;
DROP TABLE IF EXISTS receivables;
