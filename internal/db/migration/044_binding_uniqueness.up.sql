-- 044: LINE / Telegram 綁定唯一性（跨客戶資料外洩修復）
--
-- 為什麼需要：客戶表單原本可以直接 UPDATE customers.line_user_id，繞過
-- bindLineUser 的唯一性檢查。同一個 LINE 帳號被綁到兩個客戶時，
--   customerPassbookSummary: SELECT id FROM customers WHERE line_user_id = ?
-- 會任取一筆（QueryRow 不保證取哪一列），於是 A 客戶傳「履歷」會看到
-- B 客戶的維修項目與金額——這是跨客戶資料外洩，不只是通知送錯人。
--
-- 自動綁定路徑（bindLineUser）已有雙向檢查，缺的是資料庫層的保證：
-- 只要有任何一條寫入路徑忘記檢查（表單、未來的 API、手動 SQL），
-- 就會再次產生重複綁定。約束放在 DB 才是真的擋住。
--
-- 用 partial index（WHERE ...）而不是單純 UNIQUE：未綁定的客戶是 NULL 或空字串，
-- 不該互相衝突（否則整間車行只能有一個未綁定客戶）。
CREATE UNIQUE INDEX IF NOT EXISTS idx_customers_line_user_unique
    ON customers(line_user_id)
    WHERE line_user_id IS NOT NULL AND line_user_id <> '';

-- Telegram 同一個道理：同一個 chat id 綁到兩個客戶，通知會送錯人。
CREATE UNIQUE INDEX IF NOT EXISTS idx_customers_telegram_user_unique
    ON customers(telegram_user_id)
    WHERE telegram_user_id IS NOT NULL AND telegram_user_id <> '';
