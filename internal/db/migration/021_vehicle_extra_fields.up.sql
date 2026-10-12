-- 021: 車輛擴充欄位（駕駛人、顏色、車門數）
--
-- 為什麼需要：
--   舊系統 CAR35 車輛基本資料有駕駛人、駕駛人電話、顏色、車門數等欄位，
--   新系統 vehicles 表沒有這些，無法完整記錄車輛資訊。
--
-- 設計：
--   1. vehicles 表新增 driver_name、driver_phone、color、door_count
--   2. 車輛編輯表單加入這些欄位
--   3. 車輛履歷頁顯示

ALTER TABLE vehicles ADD COLUMN driver_name TEXT DEFAULT '';
ALTER TABLE vehicles ADD COLUMN driver_phone TEXT DEFAULT '';
ALTER TABLE vehicles ADD COLUMN color TEXT DEFAULT '';
ALTER TABLE vehicles ADD COLUMN door_count INTEGER DEFAULT 0;
