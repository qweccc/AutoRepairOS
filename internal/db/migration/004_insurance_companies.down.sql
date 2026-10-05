-- 回退 004：保險公司主檔
--
-- 注意：若 work_orders 已有 insurance_company_id 外鍵指向本表，
-- 必須先執行 005 的 down（移除該欄位）才能回退本表。

DROP INDEX IF EXISTS idx_insurance_companies_code;
DROP INDEX IF EXISTS idx_insurance_companies_active;
DROP TABLE IF EXISTS insurance_companies;