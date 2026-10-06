-- 健保眷屬 + 員工健保資訊
--
-- 背景：舊系統 CAR35 的 Staff 表把員工與其「健保眷屬」放在同一張表，
-- 用編號後綴區分：
--   E003    王彩秀        主被保險人（員工）
--   E03-1   嚴一舒        E003 的眷屬
--   E03-2   嚴可欣        E003 的眷屬
--   G11     黃協湯        主被保險人
--   G11-1   蔡雪雲        G11 的眷屬
-- 實測 40 筆 Branch=G 中有 33 筆可用「去後綴 → 正規化編號」對應到主員工。
--
-- 為什麼要分開：
--   1. 眷屬沒有在做事，出現在技師佣金表會污染財務數字
--   2. 健保眷屬是投保單位要維護的資料（加保/退保日、身分證號），
--      與「誰是技師」是兩件不同的事
--   3. 眷屬離職（退保）時要能單獨標記，不能連帶停用員工帳號

-- 眷屬語意改精確：family → dependent（健保眷屬）
UPDATE users SET staff_kind = 'dependent' WHERE staff_kind = 'family';

-- 健保與人事資料（CAR35 Staff 表：Id No / Birthday / Start Date / End Date / Bank No）
ALTER TABLE users ADD COLUMN national_id TEXT DEFAULT '';       -- 身分證號（Id No）
ALTER TABLE users ADD COLUMN birthday TEXT DEFAULT '';          -- 生日
ALTER TABLE users ADD COLUMN insurance_start TEXT DEFAULT '';   -- 加保日（Start Date）
ALTER TABLE users ADD COLUMN insurance_end TEXT DEFAULT '';     -- 退保日（End Date）
ALTER TABLE users ADD COLUMN bank_account TEXT DEFAULT '';      -- 薪轉帳號（Bank No）

-- 眷屬依附的主被保險人（員工 user id）。NULL = 本人為主被保險人。
ALTER TABLE users ADD COLUMN dependent_of INTEGER REFERENCES users(id);

CREATE INDEX IF NOT EXISTS idx_users_dependent_of ON users(dependent_of);
CREATE INDEX IF NOT EXISTS idx_users_staff_kind2 ON users(staff_kind, role);
