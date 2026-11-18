-- FR-FIN-01：收款支援「賒帳／月結」——除了現金/刷卡/票據/轉帳，
-- 允許把款項掛在客戶帳上（之後再沖銷）。
ALTER TABLE receivables ADD COLUMN credit_amount REAL NOT NULL DEFAULT 0;
