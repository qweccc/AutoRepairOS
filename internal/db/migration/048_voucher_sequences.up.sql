CREATE TABLE IF NOT EXISTS voucher_sequences (
    prefix TEXT NOT NULL,
    last_seq INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (prefix)
);

-- 確保 receivables.voucher_no 唯一
CREATE UNIQUE INDEX IF NOT EXISTS idx_receivables_voucher_no ON receivables(voucher_no);
