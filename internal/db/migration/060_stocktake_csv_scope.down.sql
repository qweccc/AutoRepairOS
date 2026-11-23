-- 回滾 060：移除 CSV 匯入的盤點草稿，並把 scope_type 收回為 all/category/bin。
DELETE FROM stocktake_items WHERE stocktake_id IN (SELECT id FROM stocktakes WHERE scope_type = 'csv');
DELETE FROM stocktakes WHERE scope_type = 'csv';

CREATE TABLE stocktakes_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    code TEXT NOT NULL,
    scope_type TEXT NOT NULL CHECK(scope_type IN ('all','category','bin')),
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
