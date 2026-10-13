-- 022: 車輛行照完整欄位
--
-- 為什麼需要：
--   舊系統 CAR35 車輛基本資料對應行照（汽車行車執照）欄位，
--   新系統 vehicles 表缺少燃料種類、排氣量、出廠年月、發照日期、
--   空車/總重量、輪胎規格、軸距、座位數等，無法完整記錄行照資訊。
--
-- 設計：
--   1. vehicles 表補齊行照欄位
--   2. 聯絡人與駕駛人分開：contact_name/contact_phone 是車輛聯絡人，
--      driver_name/driver_phone 是實際駕駛人（可能是不同人）
--   3. fuel_type 用 lookup 對照（category='fuel_type'）

ALTER TABLE vehicles ADD COLUMN manufacture_date TEXT DEFAULT '';
ALTER TABLE vehicles ADD COLUMN license_issue_date TEXT DEFAULT '';
ALTER TABLE vehicles ADD COLUMN fuel_type TEXT DEFAULT '';
ALTER TABLE vehicles ADD COLUMN displacement INTEGER DEFAULT 0;
ALTER TABLE vehicles ADD COLUMN body_style TEXT DEFAULT '';
ALTER TABLE vehicles ADD COLUMN seating_capacity INTEGER DEFAULT 0;
ALTER TABLE vehicles ADD COLUMN curb_weight INTEGER DEFAULT 0;
ALTER TABLE vehicles ADD COLUMN gross_weight INTEGER DEFAULT 0;
ALTER TABLE vehicles ADD COLUMN tire_size TEXT DEFAULT '';
ALTER TABLE vehicles ADD COLUMN wheelbase INTEGER DEFAULT 0;
ALTER TABLE vehicles ADD COLUMN contact_name TEXT DEFAULT '';
ALTER TABLE vehicles ADD COLUMN contact_phone TEXT DEFAULT '';
ALTER TABLE vehicles ADD COLUMN owner_name TEXT DEFAULT '';
ALTER TABLE vehicles ADD COLUMN owner_address TEXT DEFAULT '';
ALTER TABLE vehicles ADD COLUMN owner_id_number TEXT DEFAULT '';
ALTER TABLE vehicles ADD COLUMN license_expiry_date TEXT DEFAULT '';

-- 燃料種類對照
INSERT OR IGNORE INTO lookup_items (category, code, label, sort_order) VALUES
    ('fuel_type', '01', '汽油', 1),
    ('fuel_type', '02', '柴油', 2),
    ('fuel_type', '03', '油電混合', 3),
    ('fuel_type', '04', '電動', 4),
    ('fuel_type', '05', 'LPG', 5),
    ('fuel_type', '06', '其他', 6);

-- 車身式樣對照
INSERT OR IGNORE INTO lookup_items (category, code, label, sort_order) VALUES
    ('body_style', '01', '轎式', 1),
    ('body_style', '02', '掀背式', 2),
    ('body_style', '03', '旅行式', 3),
    ('body_style', '04', 'SUV', 4),
    ('body_style', '05', '貨式', 5),
    ('body_style', '06', '其他', 6);
