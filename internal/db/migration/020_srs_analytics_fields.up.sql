-- 020_srs_analytics_fields
--
-- 補齊 SRS 5.8 / 5.12 / 5.13 需要但缺欄位的部分。
-- 全部是 ADD COLUMN + DEFAULT，既有資料不受影響（SQLite 3.35+ 支援）。

-- FR-SUP-02：供應商付款條件（現結 / 月結 / 期票），供 AP 模組使用。
ALTER TABLE suppliers ADD COLUMN payment_terms TEXT;

-- FR-CRM-01：客戶標籤（VIP、奧客警示等），以逗號分隔多值。
ALTER TABLE customers ADD COLUMN tags TEXT;

-- FR-CRM-05 / FR-ANA-06：行銷活動代碼，供方案成效分析。
-- 記在客戶上當作歸戶，也記在工單上（客戶可能換代碼）。
ALTER TABLE customers ADD COLUMN campaign_code TEXT;
ALTER TABLE work_orders ADD COLUMN campaign_code TEXT;

-- FR-FIN-07：工單折扣與保固標記，報表需統計折扣金額與保固成本。
ALTER TABLE work_orders ADD COLUMN discount_amount REAL NOT NULL DEFAULT 0;
ALTER TABLE work_orders ADD COLUMN is_warranty INTEGER NOT NULL DEFAULT 0;
