-- 015: customers 表加 telegram_user_id 栏位
--
-- 客户通知偏好支援 Telegram，需要存 Telegram User ID。
-- 与 line_user_id 分开存，因为同一个人可能两个都有。

ALTER TABLE customers ADD COLUMN telegram_user_id TEXT DEFAULT '';

-- 索引：用 Telegram 通知时快速查
CREATE INDEX IF NOT EXISTS idx_customers_telegram ON customers(telegram_user_id);
