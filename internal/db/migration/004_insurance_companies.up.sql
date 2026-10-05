-- 保險公司主檔
--
-- CAR35 對應：Company 表
--   Carp78 — 廠商/公司主檔（Address, Comm, Fax, No, Short, Tax, Telephone）
--   Carp92 — 公司別帳務彙總
--   Carp32 — 完整估價單含保險公司
--   Carp77 — 保險公司車輛查詢
--
-- 為何需要：保險理賠通常佔車行營收 5–7 成，沒有這張表就無法管理理賠案件。
-- 「short」對應 CAR35 的 Company.Short（簡稱，如「國泰」「富邦」），櫃檯日常輸入用簡稱。

CREATE TABLE IF NOT EXISTS insurance_companies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    -- 簡稱，櫃檯開單時輸入用；CAR35 的 Company.Short
    code TEXT NOT NULL UNIQUE,
    -- 公司全名
    name TEXT NOT NULL,
    -- 統一編號
    tax_id TEXT DEFAULT '',
    -- 客服電話（車行打報案電話用）
    claim_phone TEXT DEFAULT '',
    phone TEXT DEFAULT '',
    contact_person TEXT DEFAULT '',
    address TEXT DEFAULT '',
    -- 郵遞區號（理賠文件寄送用）
    postal_code TEXT DEFAULT '',
    notes TEXT DEFAULT '',
    -- 軟刪除：0 = 已停用。保險公司不真的刪，因為歷史工單會引用。
    active INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 列表頁最常見是「只看啟用的」
CREATE INDEX IF NOT EXISTS idx_insurance_companies_active
    ON insurance_companies(active);

-- 依簡稱或全名搜尋（LIKE 前綴無法用索引，但資料量小）
CREATE INDEX IF NOT EXISTS idx_insurance_companies_code
    ON insurance_companies(code);