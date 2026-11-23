-- 2026-11-23：盤點單新增 CSV 匯入（scope_type='csv'）。
-- stocktakes.scope_type 原 CHECK 只允許 all/category/bin， SQLite 無法直接
-- ALTER CHECK，故重建表格（沿用 030/032 的 CREATE→COPY→DROP→RENAME 手法）。
CREATE TABLE stocktakes_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    code TEXT NOT NULL,
    scope_type TEXT NOT NULL CHECK(scope_type IN ('all','category','bin','csv')),
    scope_value TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'counting' CHECK(status IN ('counting','completed','cancelled')),
    note TEXT NOT NULL DEFAULT '',
    item_count INTEGER NOT NULL DEFAULT 0,
    created_by INTEGER,
    completed_by INTEGER,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME
);

INSERT INTO stocktakes_new
    (id, code, scope_type, scope_value, status, note, item_count,
     created_by, completed_by, created_at, completed_at)
SELECT id, code, scope_type, scope_value, status, note, item_count,
       created_by, completed_by, created_at, completed_at
FROM stocktakes;

DROP TABLE stocktakes;
ALTER TABLE stocktakes_new RENAME TO stocktakes;

CREATE INDEX IF NOT EXISTS idx_stocktakes_status ON stocktakes(status, created_at);
