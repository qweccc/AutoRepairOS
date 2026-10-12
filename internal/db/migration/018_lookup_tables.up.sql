-- 018: 代码对照表（lookup tables）
--
-- 为什么需要：
--   旧系统迁移过来的资料使用代码（如保险 company_no='07'、部门 branch='A'），
--   但新系统没有统一的代码对照表，导致：
--     1. 投保纪录显示「07」而非「富邦」
--     2. 部门名称硬编码在 Go 代码里，改一次要重编译
--     3. 银行、零件类别、险种等自由文字无法统计
--
-- 设计：
--   统一的 lookup 表结构，用 category 区分不同类型，
--   所有代码对照都在一个页面维护。

CREATE TABLE IF NOT EXISTS lookup_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    category TEXT NOT NULL,          -- 'bank', 'category', 'insurance_class', 'branch', 'insurance_company'
    code TEXT NOT NULL,              -- 代码（如 '07', 'A', '引擎'）
    label TEXT NOT NULL,             -- 名称（如 '富邦', '引擎', '強制險'）
    sort_order INTEGER DEFAULT 0,    -- 排序
    active INTEGER DEFAULT 1,        -- 1=启用 0=停用
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(category, code)
);

CREATE INDEX IF NOT EXISTS idx_lookup_category ON lookup_items(category);
CREATE INDEX IF NOT EXISTS idx_lookup_active ON lookup_items(active);

-- 初始化：部门代码（取代 branchLabel 硬编码）
INSERT OR IGNORE INTO lookup_items (category, code, label, sort_order) VALUES
    ('branch', 'A', '引擎', 1),
    ('branch', 'B', '電機', 2),
    ('branch', 'C', '鈑金', 3),
    ('branch', 'D', '塗裝', 4),
    ('branch', 'E', '行政', 5),
    ('branch', 'F', '零件', 6),
    ('branch', 'G', '眷屬', 7),
    ('branch', 'H', '廠商', 8);

-- 初始化：保险险种
INSERT OR IGNORE INTO lookup_items (category, code, label, sort_order) VALUES
    ('insurance_class', 'A', '強制險', 1),
    ('insurance_class', 'B', '任意險', 2),
    ('insurance_class', 'C', '車體險', 3),
    ('insurance_class', 'D', '竊盜險', 4),
    ('insurance_class', 'E', '責任險', 5);

-- 初始化：零件类别
INSERT OR IGNORE INTO lookup_items (category, code, label, sort_order) VALUES
    ('category', '引擎', '引擎', 1),
    ('category', '電機音響', '電機音響', 2),
    ('category', '車身鈑金', '車身鈑金', 3),
    ('category', '傳動轉向', '轉向', 4),
    ('category', '變速箱', '變速箱', 5),
    ('category', '烤漆', '烤漆', 6),
    ('category', '輪胎', '輪胎', 7),
    ('category', '其他', '其他', 8);

-- 初始化：银行（常见台湾银行）
INSERT OR IGNORE INTO lookup_items (category, code, label, sort_order) VALUES
    ('bank', '台灣銀行', '台灣銀行', 1),
    ('bank', '土地銀行', '土地銀行', 2),
    ('bank', '合作金庫', '合作金庫', 3),
    ('bank', '第一銀行', '第一銀行', 4),
    ('bank', '華南銀行', '華南銀行', 5),
    ('bank', '彰化銀行', '彰化銀行', 6),
    ('bank', '兆豐銀行', '兆豐銀行', 7),
    ('bank', '台灣企銀', '台灣企銀', 8),
    ('bank', '國泰世華', '國泰世華', 9),
    ('bank', '中國信託', '中國信託', 10),
    ('bank', '玉山銀行', '玉山銀行', 11),
    ('bank', '台北富邦', '台北富邦', 12),
    ('bank', '永豐銀行', '永豐銀行', 13),
    ('bank', '元大銀行', '元大銀行', 14),
    ('bank', '新光銀行', '新光銀行', 15),
    ('bank', '其他', '其他', 99);
