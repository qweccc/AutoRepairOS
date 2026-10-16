CREATE TABLE notifications_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    work_order_id INTEGER,
    customer_id INTEGER,
    channel TEXT CHECK(channel IN ('line','sms','none','internal')) NOT NULL DEFAULT 'internal',
    kind TEXT NOT NULL,
    subject TEXT,
    body TEXT NOT NULL,
    scheduled_at DATETIME NOT NULL,
    sent_at DATETIME,
    status TEXT CHECK(status IN ('pending','processing','sent','failed','permanent_failure','cancelled')) DEFAULT 'pending',
    retry_count INTEGER DEFAULT 0,
    error_message TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    processing_at DATETIME,
    FOREIGN KEY (work_order_id) REFERENCES work_orders(id),
    FOREIGN KEY (customer_id) REFERENCES customers(id)
);

INSERT INTO notifications_new
    (id, work_order_id, customer_id, channel, kind, subject, body, scheduled_at,
     sent_at, status, retry_count, error_message, created_at)
SELECT id, work_order_id, customer_id, channel, kind, subject, body, scheduled_at,
       sent_at, status, retry_count, error_message, created_at
FROM notifications;

DROP TABLE notifications;
ALTER TABLE notifications_new RENAME TO notifications;

CREATE INDEX IF NOT EXISTS idx_notifications_pending ON notifications(status, scheduled_at);
CREATE INDEX IF NOT EXISTS idx_notifications_wo ON notifications(work_order_id);
CREATE INDEX IF NOT EXISTS idx_notifications_processing ON notifications(status, processing_at);
