DROP INDEX IF EXISTS uq_customers_discord;
DROP INDEX IF EXISTS uq_customers_slack;
ALTER TABLE customers DROP COLUMN discord_user_id;
ALTER TABLE customers DROP COLUMN slack_user_id;