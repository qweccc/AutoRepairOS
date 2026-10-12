-- 回滾 014：移除 users.active 欄位。
--
-- SQLite 3.35.0+ 支援 DROP COLUMN。
-- 若版本太舊，需改用重建表格方式（見下方註解）。

-- DROP INDEX IF EXISTS idx_users_active;
-- ALTER TABLE users DROP COLUMN active;

-- 若 SQLite 版本不支援 DROP COLUMN，改用以下方式：
-- 1. 建立新表格（不含 active）
-- 2. 複製資料
-- 3. 刪除舊表格
-- 4. 重新命名
-- 5. 重建索引與外鍵
--
-- 本專案使用 modernc.org/sqlite（純 Go 實作），版本通常夠新，
-- 直接 DROP COLUMN 即可。
