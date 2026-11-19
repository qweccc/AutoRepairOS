-- FR-IN-06 庫存盤點：盤點單＋盤點明細（建單時帳面快照，實盤量逐筆填入）。
CREATE TABLE IF NOT EXISTS stocktakes (
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
CREATE INDEX IF NOT EXISTS idx_stocktakes_status ON stocktakes(status, created_at);

CREATE TABLE IF NOT EXISTS stocktake_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    stocktake_id INTEGER NOT NULL REFERENCES stocktakes(id),
    inventory_item_id INTEGER NOT NULL,
    part_number TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL DEFAULT '',
    bin_location TEXT NOT NULL DEFAULT '',
    system_qty INTEGER NOT NULL DEFAULT 0,
    counted_qty INTEGER,
    counted_by INTEGER,
    counted_at DATETIME,
    UNIQUE(stocktake_id, inventory_item_id)
);
CREATE INDEX IF NOT EXISTS idx_stocktake_items_st ON stocktake_items(stocktake_id);
