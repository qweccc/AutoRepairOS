-- T2.8: 照片標記（Canvas 劃線/圈選/箭頭/文字標註）
-- 儲存標記座標、類型、備註，可關聯工單照片
CREATE TABLE IF NOT EXISTS photo_annotations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    photo_id INTEGER NOT NULL,
    work_order_id INTEGER NOT NULL,
    annotation_type TEXT NOT NULL,          -- line, rect, arrow, text
    x1 REAL NOT NULL, y1 REAL NOT NULL,
    x2 REAL DEFAULT 0, y2 REAL DEFAULT 0,
    color TEXT DEFAULT '#ff0000',
    note TEXT DEFAULT '',
    created_by INTEGER,
    created_at TEXT DEFAULT (datetime('now', 'localtime')),
    deleted_at TEXT,
    FOREIGN KEY (photo_id) REFERENCES work_order_photos(id)
);

CREATE INDEX IF NOT EXISTS idx_photo_annotations_photo ON photo_annotations(photo_id);
CREATE INDEX IF NOT EXISTS idx_photo_annotations_wo ON photo_annotations(work_order_id);

-- 為 work_order_photos 加 annotation_count（非必要，查詢時計算即可）
-- 不加欄位，避免遷移複雜

-- 為舊傷預檢圖建立 walkaround_marks 表（車身示意圖標記）
CREATE TABLE IF NOT EXISTS walkaround_marks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    work_order_id INTEGER NOT NULL,
    vehicle_id INTEGER NOT NULL,
    mark_type TEXT NOT NULL,                -- scratch, dent, crack, rust, light_damage, panel_repair
    x REAL NOT NULL, y REAL NOT NULL,      -- 車身示意圖上的座標
    note TEXT DEFAULT '',
    photo_id INTEGER,                       -- 可關聯照片
    created_by INTEGER,
    created_at TEXT DEFAULT (datetime('now', 'localtime')),
    deleted_at TEXT,
    FOREIGN KEY (work_order_id) REFERENCES work_orders(id)
);

CREATE INDEX IF NOT EXISTS idx_walkaround_wo ON walkaround_marks(work_order_id);

-- 種子資料：舊傷類型
INSERT OR IGNORE INTO lookup_items (category, code, label, sort_order) VALUES
    ('walkaround_mark_type', 'scratch', '刮痕', 1),
    ('walkaround_mark_type', 'dent', '凹痕', 2),
    ('walkaround_mark_type', 'crack', '裂紋', 3),
    ('walkaround_mark_type', 'rust', '鏽蝕', 4),
    ('walkaround_mark_type', 'light_damage', '燈具損壞', 5),
    ('walkaround_mark_type', 'panel_repair', '鈑金修復', 6);

CREATE VIEW IF NOT EXISTS active_photo_annotations AS
    SELECT * FROM photo_annotations WHERE deleted_at IS NULL;
CREATE VIEW IF NOT EXISTS active_walkaround_marks AS
    SELECT * FROM walkaround_marks WHERE deleted_at IS NULL;
