-- 014: users 表加 active 欄位（離職標記）
--
-- 舊系統 CAR35 的 Staff 表沒有「在職／离职」欄位，
-- 匯入時全部預設 active=1（在職）。
--
-- 為什麼需要：
--   1. 離職技師不該出現在技師指派選單（technicianOptions）
--   2. 傭金統計不該包含已離職的人
--   3. 健保退保日（insurance_end）可對到離職時間
--
-- 為什麼用 INTEGER 而非 BOOLEAN：
--   SQLite 沒有真正的 BOOLEAN，INTEGER 0/1 是慣例。
--   DEFAULT 1 讓舊資料自動是在職。

ALTER TABLE users ADD COLUMN active INTEGER NOT NULL DEFAULT 1;

-- 索引：technicianOptions 會用 active=1 過濾
CREATE INDEX IF NOT EXISTS idx_users_active ON users(active);
