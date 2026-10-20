-- T2.11: LINE 追加維修授權（FR-WO-06、FR-NOTI-05）
--
-- approval_requests 授權紀錄表：追加工單項目的 LINE 授權連結
-- 用 token 驗證、時效控制、防重複攻擊

CREATE TABLE IF NOT EXISTS approval_requests (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    work_order_id   INTEGER NOT NULL REFERENCES work_orders(id),
    customer_id     INTEGER NOT NULL REFERENCES customers(id),
    token           TEXT    NOT NULL UNIQUE,  -- 不可猜測的隨機 token（密碼學）
    item_summary    TEXT    NOT NULL DEFAULT '',  -- 追加項目摘要或快照（JSON）
    status          TEXT    NOT NULL DEFAULT 'pending',  -- pending/approved/rejected/expired
    expires_at      TEXT    NOT NULL,              -- 過期時間（ISO 8601）
    created_at      TEXT    NOT NULL DEFAULT (datetime('now','localtime')),
    decided_at      TEXT,                           -- 決定時間
    decision_source TEXT    NOT NULL DEFAULT 'line',  -- LINE/other/web
    notes           TEXT,                           -- 備註
    deleted_at      TEXT
);

-- 索引：常用查詢加速
CREATE INDEX IF NOT EXISTS idx_approval_requests_wo ON approval_requests(work_order_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_approval_requests_customer ON approval_requests(customer_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_approval_requests_token ON approval_requests(token) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_approval_requests_status ON approval_requests(status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_approval_requests_expires ON approval_requests(expires_at) WHERE deleted_at IS NULL;

-- Active view（與 T0.7 軟刪除一致）
CREATE VIEW IF NOT EXISTS active_approval_requests AS
    SELECT * FROM approval_requests WHERE deleted_at IS NULL;
