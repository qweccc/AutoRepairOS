-- 019: 預約排程、外包加工、設備管理
--
-- 為什麼需要：
--   SRS 5.9 預約與排程、5.11 外包加工與設備管理 完全沒有實作。
--   現有系統已有客戶、車輛、工單、庫存，但缺少：
--     1. 預約排程：車主預約進廠時間，避免工位衝突
--     2. 外包加工：工單中的外包項目（item_type='subcontract'）沒有獨立管理
--     3. 設備管理：設備保養紀錄與下次保養日期
--
-- 設計原則：
--   - appointments 獨立於 work_orders，因為預約時還不知道工單內容
--   - subcontracts 記錄外包廠商、加工內容、成本與收費
--   - equipment 記錄設備基本資料與保養排程

-- ============================================================
-- 1. 預約排程表 (appointments)
-- ============================================================
CREATE TABLE IF NOT EXISTS appointments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    customer_id INTEGER NOT NULL,
    vehicle_id INTEGER NOT NULL,
    appointment_date DATE NOT NULL,           -- 預約日期
    appointment_time TEXT,                    -- 時段（如 '09:00', '14:00'）
    service_type TEXT,                        -- 服務內容（如 '定期保養', '維修檢查'）
    status TEXT CHECK(status IN ('booked', 'confirmed', 'arrived', 'cancelled', 'completed')) DEFAULT 'booked',
    notes TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (customer_id) REFERENCES customers(id),
    FOREIGN KEY (vehicle_id) REFERENCES vehicles(id)
);

CREATE INDEX IF NOT EXISTS idx_appointments_date ON appointments(appointment_date);
CREATE INDEX IF NOT EXISTS idx_appointments_customer ON appointments(customer_id);
CREATE INDEX IF NOT EXISTS idx_appointments_vehicle ON appointments(vehicle_id);
CREATE INDEX IF NOT EXISTS idx_appointments_status ON appointments(status);

-- ============================================================
-- 2. 外包加工表 (subcontracts)
-- ============================================================
CREATE TABLE IF NOT EXISTS subcontracts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    work_order_id INTEGER NOT NULL,
    supplier_id INTEGER,                      -- 外包廠商（關聯 suppliers 表）
    description TEXT NOT NULL,                -- 加工內容
    cost_amount REAL DEFAULT 0,               -- 成本金額
    charge_amount REAL DEFAULT 0,             -- 收費金額
    status TEXT CHECK(status IN ('pending', 'in_progress', 'completed', 'cancelled')) DEFAULT 'pending',
    expected_date DATE,                       -- 預計完成日期
    completed_date DATE,                      -- 實際完成日期
    notes TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (work_order_id) REFERENCES work_orders(id),
    FOREIGN KEY (supplier_id) REFERENCES suppliers(id)
);

CREATE INDEX IF NOT EXISTS idx_subcontracts_wo ON subcontracts(work_order_id);
CREATE INDEX IF NOT EXISTS idx_subcontracts_supplier ON subcontracts(supplier_id);
CREATE INDEX IF NOT EXISTS idx_subcontracts_status ON subcontracts(status);

-- ============================================================
-- 3. 設備管理表 (equipment)
-- ============================================================
CREATE TABLE IF NOT EXISTS equipment (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,                       -- 設備名稱
    model TEXT,                               -- 型號
    serial_number TEXT,                       -- 序號
    location TEXT,                            -- 放置位置
    purchase_date DATE,                       -- 購買日期
    warranty_expiry DATE,                     -- 保固到期日
    status TEXT CHECK(status IN ('active', 'maintenance', 'retired')) DEFAULT 'active',
    notes TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_equipment_status ON equipment(status);

-- ============================================================
-- 4. 設備保養紀錄表 (equipment_maintenance)
-- ============================================================
CREATE TABLE IF NOT EXISTS equipment_maintenance (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    equipment_id INTEGER NOT NULL,
    maintenance_date DATE NOT NULL,           -- 保養日期
    maintenance_type TEXT,                    -- 保養類型（如 '日常保養', '定期檢修'）
    description TEXT,                         -- 保養內容
    cost REAL DEFAULT 0,                      -- 保養費用
    next_maintenance_date DATE,               -- 下次保養日期
    technician_id INTEGER,                    -- 執行人員
    notes TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (equipment_id) REFERENCES equipment(id),
    FOREIGN KEY (technician_id) REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_equip_maint_equip ON equipment_maintenance(equipment_id);
CREATE INDEX IF NOT EXISTS idx_equip_maint_date ON equipment_maintenance(maintenance_date);
CREATE INDEX IF NOT EXISTS idx_equip_maint_next ON equipment_maintenance(next_maintenance_date);
