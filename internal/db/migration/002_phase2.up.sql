-- Phase 2: 技師工時/佣金 + 工單照片

CREATE TABLE IF NOT EXISTS technician_worklogs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    work_order_id INTEGER NOT NULL,
    technician_id INTEGER NOT NULL,
    work_date TEXT NOT NULL,
    hours REAL NOT NULL DEFAULT 0,
    hourly_rate REAL DEFAULT 0,
    labor_amount REAL DEFAULT 0,
    commission_rate REAL DEFAULT 0,
    commission_amount REAL DEFAULT 0,
    notes TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (work_order_id) REFERENCES work_orders(id),
    FOREIGN KEY (technician_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS work_order_photos (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    work_order_id INTEGER NOT NULL,
    item_id INTEGER,
    filename TEXT NOT NULL,
    original_name TEXT,
    mime_type TEXT,
    file_size INTEGER DEFAULT 0,
    photo_type TEXT CHECK(photo_type IN ('damage','repair','part','other')) DEFAULT 'damage',
    note TEXT,
    uploaded_by INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (work_order_id) REFERENCES work_orders(id),
    FOREIGN KEY (item_id) REFERENCES work_order_items(id),
    FOREIGN KEY (uploaded_by) REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_tech_worklogs_wo ON technician_worklogs(work_order_id);
CREATE INDEX IF NOT EXISTS idx_tech_worklogs_tech ON technician_worklogs(technician_id);
CREATE INDEX IF NOT EXISTS idx_tech_worklogs_date ON technician_worklogs(work_date);
CREATE INDEX IF NOT EXISTS idx_wo_photos_wo ON work_order_photos(work_order_id);
