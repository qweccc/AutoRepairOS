DROP INDEX IF EXISTS idx_audit_log_entity;
DROP TABLE IF EXISTS audit_log;
ALTER TABLE receivables DROP COLUMN voided_by;
ALTER TABLE receivables DROP COLUMN voided_at;
