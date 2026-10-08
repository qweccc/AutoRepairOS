-- 收款沖銷（應收帳款收款紀錄）
--
-- 對應舊系統 CAR35 的「應收帳款沖銷作業」（Customer Pay 表，90,762 筆）。
-- 這是舊系統金額最大的缺口：現金 3.48 億、刷卡 4,417 萬、支票 714 萬，
-- 而新系統在此之前完全沒有收款紀錄 —— 結帳只能把工單狀態改成 paid，
-- 看不出「收了多少、用什麼方式收、有沒有折讓、票什麼時候到期」。
--
-- 為什麼不把這些欄位直接加在 work_orders 上：
--   1. 一張工單可以分次收款（舊系統 90,762 筆對 87,625 張工單，差 3,137 筆）
--   2. 一次收款可以沖銷多張工單（車行月結的常見情境）
--   3. 收款紀錄有獨立單號（voucher_no），需要單獨查詢與列印
--   因此是獨立實體，用 work_order_id 可空關聯（手動收款不必綁工單）。
--
-- 金額單位：舊系統以「元」儲存（實測 Cash Total 500 = 500 元，
-- 與工單金額同級），非「分」——這點與 Spend List 不同，勿混淆。

CREATE TABLE IF NOT EXISTS receivables (
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    -- 收款單號。舊系統 Customer Pay.No，唯一。
    -- 注意舊資料的格式很雜（'.0003001'、'+9/00001'），所以不設 UNIQUE，
    -- 由應用層在「新增」時檢查；遷移進來的歷史單號照原樣保留。
    voucher_no TEXT NOT NULL DEFAULT '',

    customer_id INTEGER NOT NULL REFERENCES customers(id),
    vehicle_id INTEGER REFERENCES vehicles(id),

    -- 關聯工單。可空：手動收款（如月結一次收）不必綁特定工單。
    -- 舊系統 Maintain No 有 90,759 筆可對到工單。
    work_order_id INTEGER REFERENCES work_orders(id),

    -- 收款日期（舊系統 Pay Date）。這是「錢進來的日子」，
    -- 與工單的結帳日不同，兩者都要能查。
    pay_date TEXT NOT NULL DEFAULT '',

    -- 金額拆分。
    --   detail_total  ／ real_total ：明細合計／實收合計（折讓後）
    --   discount      ：折扣金額
    --   tax           ：稅額
    --   pay_total     ：本次應收總額（= 應收 − 折扣 − 折讓 + 稅）
    detail_total REAL NOT NULL DEFAULT 0,
    real_total   REAL NOT NULL DEFAULT 0,
    discount     REAL NOT NULL DEFAULT 0,
    tax          REAL NOT NULL DEFAULT 0,
    pay_total    REAL NOT NULL DEFAULT 0,

    -- 付款方式拆分。舊系統在同一筆紀錄裡記三種金額（可並存）。
    -- 實測使用率：現金 83,094 筆、信用卡 7,126 筆、支票 741 筆。
    cash         REAL NOT NULL DEFAULT 0,
    card         REAL NOT NULL DEFAULT 0,
    check_amount REAL NOT NULL DEFAULT 0,

    -- 信用卡與票據細節（使用率低但屬必要欄位：報帳與對帳要用）
    card_no      TEXT DEFAULT '',   -- 卡號（舊系統 266 筆）
    check_no     TEXT DEFAULT '',   -- 票號（23 筆）
    check_date   TEXT DEFAULT '',   -- 票據到期日（19 筆）
    bank         TEXT DEFAULT '',   -- 行庫（19 筆）

    notes        TEXT DEFAULT '',

    -- 舊系統的作廢旗標（Delete Flag，9 筆）。作廢不真刪：
    -- 帳務紀錄需要留痕，且真刪會讓對帳金額對不上。
    voided       INTEGER NOT NULL DEFAULT 0,

    -- 來源標記：'legacy' = 從 CAR35 遷移，'manual' = 新系統建立。
    -- 讓日後能分辨「這筆是舊帳」還是「新收的」，也方便重跑遷移時只清 legacy。
    source       TEXT NOT NULL DEFAULT 'manual',

    -- 舊系統單號原文（含 . 與 + 等雜訊），保留以供追溯
    legacy_no    TEXT DEFAULT '',

    created_by INTEGER REFERENCES users(id),
    created_at TEXT NOT NULL DEFAULT (datetime('now','localtime'))
);

CREATE INDEX IF NOT EXISTS idx_receivables_customer ON receivables(customer_id);
CREATE INDEX IF NOT EXISTS idx_receivables_work_order ON receivables(work_order_id);
CREATE INDEX IF NOT EXISTS idx_receivables_pay_date ON receivables(pay_date);
CREATE INDEX IF NOT EXISTS idx_receivables_voucher ON receivables(voucher_no);
CREATE INDEX IF NOT EXISTS idx_receivables_source ON receivables(source);

-- 應收帳款明細（分期／多筆收款）。
--
-- 為什麼需要這張而不是只用 receivables：
--   車行常見「先收一半、尾款下個月」，或月結客戶一次付清多張工單。
--   receivables 記「一次收款事件」，receivables_payments 記「這筆事件
--   沖銷了哪些工單、各多少」，這樣才能算出「這張工單還欠多少」。
--
-- 舊系統沒有對應表（它用 work_order_id 一對一簡化處理），
-- 這是新系統的加強：允許一對多。
CREATE TABLE IF NOT EXISTS receivable_payments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    receivable_id INTEGER NOT NULL REFERENCES receivables(id) ON DELETE CASCADE,
    work_order_id INTEGER REFERENCES work_orders(id),
    amount REAL NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now','localtime'))
);

CREATE INDEX IF NOT EXISTS idx_recv_pay_receivable ON receivable_payments(receivable_id);
CREATE INDEX IF NOT EXISTS idx_recv_pay_work_order ON receivable_payments(work_order_id);
