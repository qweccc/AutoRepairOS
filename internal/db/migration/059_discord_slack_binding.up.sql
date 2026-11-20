-- FR-CRM-06：Discord／Slack 客戶綁定欄位（鏡射 telegram_user_id 的唯一性規則）。
ALTER TABLE customers ADD COLUMN discord_user_id TEXT;
ALTER TABLE customers ADD COLUMN slack_user_id TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS uq_customers_discord ON customers(discord_user_id) WHERE discord_user_id IS NOT NULL AND discord_user_id <> '';
CREATE UNIQUE INDEX IF NOT EXISTS uq_customers_slack ON customers(slack_user_id) WHERE slack_user_id IS NOT NULL AND slack_user_id <> '';