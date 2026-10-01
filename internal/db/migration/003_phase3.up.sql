-- Phase 3: 排程任務 + 通知佇列 + 備份紀錄

CREATE TABLE IF NOT EXISTS notifications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    work_order_id INTEGER,
    customer_id INTEGER,
    channel TEXT CHECK(channel IN ('line','sms','none','internal')) NOT NULL DEFAULT 'internal',
    kind TEXT NOT NULL,
    subject TEXT,
    body TEXT NOT NULL,
    scheduled_at DATETIME NOT NULL,
    sent_at DATETIME,
    status TEXT CHECK(status IN ('pending','sent','failed','cancelled')) DEFAULT 'pending',
    retry_count INTEGER DEFAULT 0,
    error_message TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (work_order_id) REFERENCES work_orders(id),
    FOREIGN KEY (customer_id) REFERENCES customers(id)
);

CREATE TABLE IF NOT EXISTS scheduled_tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_key TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    cron_expr TEXT,
    interval_minutes INTEGER,
    enabled BOOLEAN DEFAULT 1,
    last_run_at DATETIME,
    next_run_at DATETIME,
    run_count INTEGER DEFAULT 0,
    last_error TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS backup_records (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    backup_date TEXT NOT NULL,
    db_backup_path TEXT,
    db_size INTEGER DEFAULT 0,
    photo_count INTEGER DEFAULT 0,
    photo_size INTEGER DEFAULT 0,
    status TEXT CHECK(status IN ('success','failed')) DEFAULT 'success',
    error_message TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_notifications_pending ON notifications(status, scheduled_at);
CREATE INDEX IF NOT EXISTS idx_notifications_wo ON notifications(work_order_id);
CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_key ON scheduled_tasks(task_key);
