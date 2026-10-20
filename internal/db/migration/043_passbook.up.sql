-- 043: LINE 數位維修履歷與保養收據（SRS FR-VH-05）。
--
-- 綁定 LINE 的車主可透過公開端點查詢愛車歷次維修/保養收據與履歷，
-- 不需要登入。身份驗證靠 customers.line_user_id 綁定（與 T2.11 估價授權
-- 同一套機制），因此 passbook 的公開 handler 不需要 RequireLogin 等 middleware。
--
-- 每筆 passbook 是一張「收據/履歷紀錄」，由櫃檯在工單完工/結帳時建立，
-- 記錄該工單的服務類型、零件與工資成本、稅與折扣、總金額與狀態。
CREATE TABLE IF NOT EXISTS passbook (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    customer_id INTEGER NOT NULL REFERENCES customers(id),
    vehicle_id INTEGER NOT NULL REFERENCES vehicles(id),
    work_order_id INTEGER REFERENCES work_orders(id),
    service_type TEXT NOT NULL DEFAULT 'repair'
        CHECK (service_type IN ('inspection','maintenance','repair','parts')),
    description TEXT NOT NULL DEFAULT '',
    parts_cost REAL NOT NULL DEFAULT 0,
    labor_cost REAL NOT NULL DEFAULT 0,
    subcontract_cost REAL NOT NULL DEFAULT 0,
    subtotal REAL NOT NULL DEFAULT 0,
    tax REAL NOT NULL DEFAULT 0,
    discount_amount REAL NOT NULL DEFAULT 0,
    total_amount REAL NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','approved','paid','rejected')),
    notes TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT (datetime('now','localtime')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now','localtime'))
);

CREATE INDEX IF NOT EXISTS idx_passbook_customer ON passbook(customer_id);
CREATE INDEX IF NOT EXISTS idx_passbook_vehicle ON passbook(vehicle_id);
CREATE INDEX IF NOT EXISTS idx_passbook_work_order ON passbook(work_order_id);
CREATE INDEX IF NOT EXISTS idx_passbook_status ON passbook(status);
