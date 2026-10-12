-- 回滚 015：移除 customers.telegram_user_id 栏位。

-- DROP INDEX IF EXISTS idx_customers_telegram;
-- ALTER TABLE customers DROP COLUMN telegram_user_id;
