-- 保險公司舊編號對應
--
-- 問題：舊系統有兩套保險公司編號，彼此不同。
--   Insurance Company 表（主檔）  No = '07'、'05'、'13'…
--   Car Insurance List（投保）    Company No = '07'、'05'、'13'…  ← 同一套
-- 但先前遷移把主檔的 code 存成「簡稱」（Short Name，如「泰安產物」），
-- 導致投保紀錄的 '07' 對不到任何保險公司，2,438 筆投保紀錄的保險公司欄全空。
--
-- 修法：保留舊編號，讓投保紀錄能正確對應。
ALTER TABLE insurance_companies ADD COLUMN legacy_no TEXT DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_ins_legacy_no ON insurance_companies(legacy_no);
