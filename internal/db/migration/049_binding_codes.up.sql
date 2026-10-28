CREATE TABLE IF NOT EXISTS binding_codes (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    platform     TEXT NOT NULL,          -- 'line' 或 'telegram'
    customer_id  INTEGER NOT NULL,
    code_hash    TEXT NOT NULL,          -- SHA-256(code)，不存明碼
    expires_at   TEXT NOT NULL,
    used_at      TEXT,
    attempts     INTEGER NOT NULL DEFAULT 0,
    created_by   INTEGER,
    created_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_binding_codes_customer
    ON binding_codes(customer_id, used_at);
CREATE INDEX IF NOT EXISTS idx_binding_codes_platform
    ON binding_codes(platform, used_at);
