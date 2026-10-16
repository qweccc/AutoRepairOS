ALTER TABLE receivables
    ADD COLUMN transfer_amount REAL NOT NULL DEFAULT 0;

ALTER TABLE receivables
    ADD COLUMN transfer_reference TEXT NOT NULL DEFAULT '';
