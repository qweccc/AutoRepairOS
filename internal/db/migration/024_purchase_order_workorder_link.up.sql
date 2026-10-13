-- 024: 進貨單關聯工單
--
-- 為什麼需要：
--   有些進貨是為了特定工單叫的（客戶指定零件、急件補料）。
--   加一個選填的 work_order_id，方便追蹤「這張工單叫了什麼料、花了多少」。
--
-- 設計：
--   work_order_id = 0 表示一般進貨（不對應特定工單，僅入庫）。
--   不建立外鍵約束：工單可能被刪除，且這是選填追蹤用途，寬鬆儲存即可。

ALTER TABLE purchase_orders ADD COLUMN work_order_id INTEGER DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_po_workorder ON purchase_orders(work_order_id);
