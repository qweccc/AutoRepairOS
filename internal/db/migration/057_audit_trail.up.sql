CREATE TABLE IF NOT EXISTS audit_log (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	actor_id INTEGER,
	action TEXT NOT NULL,
	entity_type TEXT NOT NULL,
	entity_id INTEGER NOT NULL,
	detail_json TEXT NOT NULL DEFAULT '{}',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_audit_log_entity ON audit_log(entity_type, entity_id, created_at);
ALTER TABLE receivables ADD COLUMN voided_by INTEGER;
ALTER TABLE receivables ADD COLUMN voided_at TEXT;
