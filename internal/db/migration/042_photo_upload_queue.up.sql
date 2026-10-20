-- 042: 持久化照片上傳佇列，讓暫存失敗可重試且可追蹤。
CREATE TABLE IF NOT EXISTS photo_upload_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    work_order_id INTEGER NOT NULL REFERENCES work_orders(id),
    item_id INTEGER REFERENCES work_order_items(id),
    file_name TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    file_size INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','uploading','done','failed')),
    retry_count INTEGER NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    stored_name TEXT NOT NULL,
    photo_type TEXT NOT NULL DEFAULT 'damage'
        CHECK (photo_type IN ('damage','repair','part','other')),
    note TEXT NOT NULL DEFAULT '',
    photo_id INTEGER REFERENCES work_order_photos(id),
    created_at TEXT NOT NULL DEFAULT (datetime('now','localtime')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now','localtime'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_photo_upload_queue_dedupe
    ON photo_upload_queue(work_order_id, COALESCE(item_id, 0), file_name);
CREATE INDEX IF NOT EXISTS idx_photo_upload_queue_status
    ON photo_upload_queue(status, work_order_id, id);
