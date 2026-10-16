-- T2.7: 技師常用片語庫（FR-WO-07）
--
-- technician_phrases 技師常用維修描述片語，供快速輸入使用
-- 避免技師油手打字不便，提供點選帶入功能

CREATE TABLE IF NOT EXISTS technician_phrases (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    phrase      TEXT    NOT NULL,              -- 片語內容（例：煞車皮剩2mm、機油滲漏）
    category    TEXT    NOT NULL DEFAULT '',    -- 分類（例：煞車、引擎、底盤、電路）
    sort_order  INTEGER NOT NULL DEFAULT 0,    -- 排序
    active      INTEGER NOT NULL DEFAULT 1,    -- 啟用/停用
    created_at  TEXT    NOT NULL DEFAULT (datetime('now','localtime')),
    deleted_at  TEXT
);

-- 索引：常用查詢加速
CREATE INDEX IF NOT EXISTS idx_tech_phrases_category ON technician_phrases(category) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_tech_phrases_active ON technician_phrases(active) WHERE deleted_at IS NULL;

-- Active view（與 T0.7 軟刪除一致）
CREATE VIEW IF NOT EXISTS active_technician_phrases AS
    SELECT * FROM technician_phrases WHERE deleted_at IS NULL;

-- 預設片語（常見維修描述）
INSERT OR IGNORE INTO technician_phrases (id, phrase, category, sort_order)
VALUES
    (1, '煞車皮剩2mm', '煞車', 1),
    (2, '煞車片異常磨損', '煞車', 2),
    (3, '煞車油變黑', '煞車', 3),
    (4, '機油滲漏', '引擎', 10),
    (5, '機油變質', '引擎', 11),
    (6, '機油不足', '引擎', 12),
    (7, '水箱漏水', '冷卻', 20),
    (8, '冷卻液不足', '冷卻', 21),
    (9, '風扇不轉', '冷卻', 22),
    (10, '輪胎磨損不均', '輪胎', 30),
    (11, '輪胎氣壓不足', '輪胎', 31),
    (12, '輪胎側壁裂痕', '輪胎', 32),
    (13, '避震器漏油', '底盤', 40),
    (14, '球頭鬆動', '底盤', 41),
    (15, '方向機異常', '底盤', 42),
    (16, '電瓶電壓不足', '電路', 50),
    (17, '發電機不充電', '電路', 51),
    (18, '起動機無反應', '電路', 52),
    (19, '空調不冷', '空調', 60),
    (20, '空調異音', '空調', 61),
    (21, '冷媒不足', '空調', 62),
    (22, '變速箱換檯異常', '變速箱', 70),
    (23, '變速箱漏油', '變速箱', 71),
    (24, '離合器打滑', '離合器', 80),
    (25, '離合器異音', '離合器', 81);
