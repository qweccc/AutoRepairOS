-- 回退 005：工單保險理賠欄位
--
-- SQLite 的 DROP COLUMN 需 SQLite 3.35+；modernc.org/sqlite 版本足夠。
-- 若舊版本報錯，改用「重建表 + 搬資料」的替代方案。

ALTER TABLE work_orders DROP COLUMN deductible_amount;
ALTER TABLE work_orders DROP COLUMN accident_date;
ALTER TABLE work_orders DROP COLUMN is_insurance;
ALTER TABLE work_orders DROP COLUMN claim_no;
ALTER TABLE work_orders DROP COLUMN insurance_company_id;