-- 補齊舊系統有資料但未遷移的表
--
-- 舊系統 CAR35 有幾張表帶著實質歷史資料，先前遷移只做了營運主流程
-- （客戶/車輛/工單/庫存/人員），這幾張漏掉：
--
--   Car Insurance List   2,667 筆  車輛投保紀錄（險種、發照日、卡號、保險公司）
--   Car Check List      21,176 筆  車輛定檢排程（排定日、完成日、代檢廠）
--   Maintain Record *       308 筆  保養套餐範本（5千公里定期保養…）
--   Spend List           3,390 筆  零用金／雜支支出
--
-- 金額單位：舊系統以「分」儲存（見 Spend List 的 20000000 → 200,000 元），
-- 依指示先按原樣遷移，不換算；欄位註解標明單位以便日後處理。

-- 車輛投保紀錄（CAR35 Car Insurance List）
CREATE TABLE IF NOT EXISTS vehicle_insurances (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    vehicle_id INTEGER NOT NULL REFERENCES vehicles(id),
    insurance_company_id INTEGER REFERENCES insurance_companies(id),
    company_no TEXT DEFAULT '',              -- 舊系統保險公司編號（無法對應時保留）
    insurance_class TEXT DEFAULT '',         -- 險種代碼（A 強制險 / B 任意險 / …）
    insurance_class_name TEXT DEFAULT '',    -- 險種中文（由 Insurance Class 查表補）
    license_date TEXT DEFAULT '',            -- 發照日（民國年已轉西元）
    card_no TEXT DEFAULT '',                 -- 保險卡號
    amount_raw TEXT DEFAULT '',              -- 舊系統 Remark 欄（多為保額描述，如 200/400/30萬）
    notes TEXT DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now','localtime'))
);

CREATE INDEX IF NOT EXISTS idx_veh_ins_vehicle ON vehicle_insurances(vehicle_id);
CREATE INDEX IF NOT EXISTS idx_veh_ins_company ON vehicle_insurances(insurance_company_id);
CREATE INDEX IF NOT EXISTS idx_veh_ins_date ON vehicle_insurances(license_date);

-- 車輛定檢排程（CAR35 Car Check List）
--
-- 注意：Check Date 只有 3 筆有值、Firm No 全空 —— 舊系統只記錄「排定日」，
-- 實際完成幾乎沒回填。因此 check_date 為空代表「尚未完成」。
CREATE TABLE IF NOT EXISTS vehicle_inspections (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    vehicle_id INTEGER NOT NULL REFERENCES vehicles(id),
    order_date TEXT DEFAULT '',              -- 排定檢驗日
    check_date TEXT DEFAULT '',              -- 實際完成日（多為空）
    firm_no TEXT DEFAULT '',                 -- 代檢廠編號（舊資料全空）
    notes TEXT DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now','localtime'))
);

CREATE INDEX IF NOT EXISTS idx_veh_insp_vehicle ON vehicle_inspections(vehicle_id);
CREATE INDEX IF NOT EXISTS idx_veh_insp_order ON vehicle_inspections(order_date);

-- 保養套餐範本（CAR35 Maintain Record Header / Detail）
--
-- 為什麼有用：開工單時可一鍵套用「5千公里定期保養」的標準項目，
-- 不必每次手動加。舊系統有 21 個里程級距 + 過戶/理賠等特殊單。
CREATE TABLE IF NOT EXISTS maintenance_templates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    code TEXT NOT NULL UNIQUE,               -- 舊系統編號（A0002 / G000 …）
    name TEXT NOT NULL,                      -- 套餐名稱（5千公里定期保養）
    notes TEXT DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now','localtime'))
);

CREATE TABLE IF NOT EXISTS maintenance_template_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    template_id INTEGER NOT NULL REFERENCES maintenance_templates(id),
    sequence_no INTEGER NOT NULL DEFAULT 0,  -- 舊系統的項次
    part_number TEXT DEFAULT '',             -- 對應 inventory_items.part_number
    description TEXT DEFAULT '',
    quantity REAL NOT NULL DEFAULT 1,
    unit_price REAL NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_mt_items_template ON maintenance_template_items(template_id);

-- 零用金／雜支支出（CAR35 Spend List）
--
-- 金額單位：沿用舊系統原值（分），不換算 —— 依指示「先按本來單位」。
-- amount_raw 保留原始值；顯示時可再決定是否除以 100。
CREATE TABLE IF NOT EXISTS expenses (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    sequence_no INTEGER NOT NULL DEFAULT 0,
    spend_date TEXT DEFAULT '',              -- 支出日（民國年已轉西元）
    amount_in REAL NOT NULL DEFAULT 0,       -- 收入/進項（原單位）
    amount_out REAL NOT NULL DEFAULT 0,      -- 支出/出項（原單位）
    part_number TEXT DEFAULT '',             -- 舊系統 Material No（多為科目代碼）
    notes TEXT DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now','localtime'))
);

CREATE INDEX IF NOT EXISTS idx_expenses_date ON expenses(spend_date);
