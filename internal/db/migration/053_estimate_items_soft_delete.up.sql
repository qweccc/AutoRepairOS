-- 估價單項目軟刪除（與 052 同一原則：讀取路徑一律走 active_* 視）。
--
-- 估價單只有草稿階段會刪項目，但保留歷史的價值與工單明細相同（避免
-- 「只是草稿」變成資料消失的藉口），故比照辦理。
ALTER TABLE estimate_items ADD COLUMN deleted_at TEXT;

CREATE VIEW IF NOT EXISTS active_estimate_items AS
    SELECT * FROM estimate_items WHERE deleted_at IS NULL;
