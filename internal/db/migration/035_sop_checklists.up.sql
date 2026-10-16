-- T2.6: SOP 檢查表（逐項 SOP 清單與完工限制 FR-WO-08）
--
-- sop_templates      SOP 模板（依維修類型分類）
-- sop_items          模板內的檢查項目
-- work_order_sop_records  工單逐項勾選紀錄（含完成者與完成時間）

CREATE TABLE IF NOT EXISTS sop_templates (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL,              -- 模板名稱（例：一般保養、大修）
    repair_type TEXT    NOT NULL DEFAULT '',    -- 維修類型（maintenance/overhaul/bodywork/…, 空字串=通用）
    active      INTEGER NOT NULL DEFAULT 1,    -- 啟用/停用
    sort_order  INTEGER NOT NULL DEFAULT 0,    -- 排序
    created_at  TEXT    NOT NULL DEFAULT (datetime('now','localtime')),
    deleted_at  TEXT
);

CREATE TABLE IF NOT EXISTS sop_items (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    template_id   INTEGER NOT NULL REFERENCES sop_templates(id),
    description   TEXT    NOT NULL,            -- 檢查項目描述
    required      INTEGER NOT NULL DEFAULT 1,  -- 1=必填（完工前必須完成） 0=選填
    sort_order    INTEGER NOT NULL DEFAULT 0,  -- 排序
    created_at    TEXT    NOT NULL DEFAULT (datetime('now','localtime')),
    deleted_at    TEXT
);

CREATE TABLE IF NOT EXISTS work_order_sop_records (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    work_order_id   INTEGER NOT NULL REFERENCES work_orders(id),
    sop_item_id     INTEGER NOT NULL REFERENCES sop_items(id),
    template_id     INTEGER NOT NULL REFERENCES sop_templates(id),
    description     TEXT    NOT NULL DEFAULT '', -- 快照項目描述（模板日後改動不影響歷史紀錄）
    required        INTEGER NOT NULL DEFAULT 1,  -- 快照是否必填
    completed       INTEGER NOT NULL DEFAULT 0,  -- 0=未完成 1=已完成
    completed_by    INTEGER,                     -- 完成者 user_id
    completed_at    TEXT,                        -- 完成時間
    sort_order      INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT    NOT NULL DEFAULT (datetime('now','localtime')),
    UNIQUE(work_order_id, sop_item_id)
);

-- 工單掛 SOP 模板的關聯：一張工單可套用多個模板
CREATE TABLE IF NOT EXISTS work_order_sop_templates (
    work_order_id  INTEGER NOT NULL REFERENCES work_orders(id),
    template_id    INTEGER NOT NULL REFERENCES sop_templates(id),
    created_at     TEXT    NOT NULL DEFAULT (datetime('now','localtime')),
    PRIMARY KEY (work_order_id, template_id)
);

-- 索引：常用查詢加速
CREATE INDEX IF NOT EXISTS idx_sop_items_template ON sop_items(template_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_wo_sop_records_wo ON work_order_sop_records(work_order_id);
CREATE INDEX IF NOT EXISTS idx_wo_sop_records_completed ON work_order_sop_records(work_order_id, required, completed);

-- Active views（與 T0.7 軟刪除一致）
CREATE VIEW IF NOT EXISTS active_sop_templates AS
    SELECT * FROM sop_templates WHERE deleted_at IS NULL;
CREATE VIEW IF NOT EXISTS active_sop_items AS
    SELECT * FROM sop_items WHERE deleted_at IS NULL;

-- 預設 SOP 模板（保養/大修）
INSERT OR IGNORE INTO sop_templates (id, name, repair_type, sort_order)
VALUES
    (1, '一般保養 SOP', 'maintenance', 1),
    (2, '大修 SOP', 'overhaul', 2);

-- 一般保養 SOP 項目
INSERT OR IGNORE INTO sop_items (id, template_id, description, required, sort_order)
VALUES
    (1, 1, '確認機油型號與容量', 1, 1),
    (2, 1, '更換機油濾芯', 1, 2),
    (3, 1, '檢查煞車片厚度', 1, 3),
    (4, 1, '檢查輪胎磨損與胎壓', 1, 4),
    (5, 1, '檢查冷卻液液面', 1, 5),
    (6, 1, '檢查雨刷狀況', 0, 6),
    (7, 1, '檢查燈光系統', 0, 7),
    (8, 1, '路試確認', 1, 8);

-- 大修 SOP 項目
INSERT OR IGNORE INTO sop_items (id, template_id, description, required, sort_order)
VALUES
    (9, 2, '拆卸引擎蓋與護板', 1, 1),
    (10, 2, '測量汽缸壓縮比', 1, 2),
    (11, 2, '檢查正時鍊條/皮帶', 1, 3),
    (12, 2, '更換墊片與油封', 1, 4),
    (13, 2, '重組與扭力校正', 1, 5),
    (14, 2, '引擎電腦診斷清碼', 1, 6),
    (15, 2, '路試確認（含高速）', 1, 7),
    (16, 2, '外觀復原檢查', 0, 8);
