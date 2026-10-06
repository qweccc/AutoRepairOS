-- 區分員工與協力廠商
--
-- 背景：舊系統 Maintain Header.Staff No 記錄「誰做的」，可能是指派員工
-- （王彩秀，被引用 21,969 次）也可能是外包廠商（上上汽車、元發電機、
-- 精修汽車）。兩者混在同一個參照欄位，UI 上無法分辨。
--
-- 因此 users 表保留兩者（歷史工單的外鍵不能斷），但用 staff_kind 標記，
-- 技師指派的下拉選單只列 employee。
--
-- employee 員工（含老闆、技師、行政）
-- vendor   協力廠商（外包引擎/電機/鈑金/烤漆/輪胎等）
-- family   眷屬（舊系統 Branch G，車行家族成員）
ALTER TABLE users ADD COLUMN staff_kind TEXT NOT NULL DEFAULT 'employee';

CREATE INDEX IF NOT EXISTS idx_users_staff_kind ON users(staff_kind);
