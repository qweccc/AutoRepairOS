-- 工單加保險理賠欄位
--
-- CAR35 對應：
--   Carp32 — 完整估價單（Header.Insurance 保險公司、Header.Tax、Header.Discount）
--   Carp77 — 保險公司車輛（Car.Insurance、Company.Short、Temp.License）
--   Carp91 — 保險/稅務帳務拆分（Header.Insurance、Header.Pay、Header.Real）
--   Carp92 — 公司別帳務彙總
--
-- 為何需要：保險理賠佔車行營收 5–7 成。
-- is_insurance 與 insurance_company_id 分開設計，是因為：
--   - 有些案件「自費維修但車主有保險」，可能不報理賠
--   - 報表要能快速篩「所有保險件」而不必 JOIN insurance_companies
--   - 報廢車全損理賠時可能沒指定特定保險公司

ALTER TABLE work_orders ADD COLUMN insurance_company_id INTEGER REFERENCES insurance_companies(id);
ALTER TABLE work_orders ADD COLUMN claim_no TEXT DEFAULT '';            -- 保險理賠案件編號
ALTER TABLE work_orders ADD COLUMN is_insurance INTEGER NOT NULL DEFAULT 0;  -- 1=保險件 0=自費件
ALTER TABLE work_orders ADD COLUMN accident_date TEXT DEFAULT '';        -- 事故發生日期
ALTER TABLE work_orders ADD COLUMN deductible_amount REAL NOT NULL DEFAULT 0; -- 折舊/自負額

-- 報表最常見：「本月保險件有哪些」
CREATE INDEX IF NOT EXISTS idx_work_orders_is_insurance ON work_orders(is_insurance);
CREATE INDEX IF NOT EXISTS idx_work_orders_insurance_company ON work_orders(insurance_company_id);