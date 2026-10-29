CREATE TABLE IF NOT EXISTS insurance_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    event_type TEXT NOT NULL,          -- 'enroll' | 'withdraw' | 'change'
    event_date TEXT NOT NULL,         -- YYYY-MM-DD
    note TEXT DEFAULT '',
    created_by INTEGER,
    created_at TEXT DEFAULT (datetime('now','localtime')),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_insurance_history_user ON insurance_history(user_id, event_date);
