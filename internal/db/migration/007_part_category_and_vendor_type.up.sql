-- 零件類別 + 廠商型態
--
-- 背景：舊系統 CAR35 把四種東西混在 Staff 表（廠商 / 保險公司 / 眷屬 / 員工），
-- 且零件沒有可讀的類別欄位（只有 Material.Class No 單碼）。
--
-- 對應 CAR35：
--   Material.Class No → Class 查表（A 引擎 / B 電機音響 / C 傳動轉向 /
--                       D 制動 / E 車身鈑金 / F 輪胎 / G 工資 / H 烤漆 / I 其他）
--   Firm（559 家材料商）與 Staff Branch H（86 家外包廠商）是兩種不同的往來對象

-- 零件類別（可讀名稱，如「引擎」「電機音響」）
ALTER TABLE inventory_items ADD COLUMN category TEXT DEFAULT '';

-- 零件性質：part（零件）/ labor（工資）/ service（服務）
-- CAR35 用 Material.Class 0/1/2 表示，這裡轉成可讀值
ALTER TABLE inventory_items ADD COLUMN item_kind TEXT NOT NULL DEFAULT 'part';

CREATE INDEX IF NOT EXISTS idx_inventory_category ON inventory_items(category);

-- 廠商型態：
--   material 材料商（CAR35 Firm 表，賣零件給我方）
--   vendor   外包廠商（CAR35 Staff Branch H，如輪胎行、鈑金廠）
--   other    其他往來（廣告社、事務所等）
ALTER TABLE suppliers ADD COLUMN vendor_type TEXT NOT NULL DEFAULT 'material';

CREATE INDEX IF NOT EXISTS idx_suppliers_vendor_type ON suppliers(vendor_type);

-- 員工標記：區分真正的技師/員工與舊系統誤建的資料
-- CAR35 Staff 表的 Branch：A 引擎 / B 電機 / C 鈑金 / D 烤漆 / E 行政 /
--                          F 零件 / G 眷屬 / H 廠商
ALTER TABLE users ADD COLUMN staff_branch TEXT DEFAULT '';
