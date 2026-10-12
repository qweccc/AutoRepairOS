-- 020: 車輛車種類別
--
-- 為什麼需要：
--   舊系統 CAR35 車輛有車種代碼（01-12），如自小客、貨車、公車、消防車等。
--   新系統沒有車種欄位，無法統計不同車種的維修需求。
--
-- 設計：
--   1. vehicles 表新增 vehicle_type 欄位，存車種代碼
--   2. lookup_items 新增 category='vehicle_type' 的對照資料
--   3. 車輛列表和編輯表單加上車種下拉選單

-- 1. vehicles 表新增車種欄位
ALTER TABLE vehicles ADD COLUMN vehicle_type TEXT DEFAULT '';

-- 2. 初始化車種對照資料（對應舊系統 01-12）
INSERT OR IGNORE INTO lookup_items (category, code, label, sort_order) VALUES
    ('vehicle_type', '01', '自小客', 1),
    ('vehicle_type', '02', '警用車', 2),
    ('vehicle_type', '03', '貨車', 3),
    ('vehicle_type', '04', '公車', 4),
    ('vehicle_type', '05', '消防車', 5),
    ('vehicle_type', '06', '吊車', 6),
    ('vehicle_type', '07', '救護車', 7),
    ('vehicle_type', '08', '拖車', 8),
    ('vehicle_type', '09', '機車', 9),
    ('vehicle_type', '10', '計程車', 10),
    ('vehicle_type', '11', '租賃車', 11),
    ('vehicle_type', '12', '其他', 12);
