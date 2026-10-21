-- 045: 預約與工單的關聯（預約「已進廠」直接開單）
--
-- 為什麼需要：櫃檯已經知道車到了、預約資料（客戶、車輛、日期、服務內容）也
-- 填好了，卻要在開工單時重新打一次車牌與客戶——既慢又容易選錯車。
-- 兩張表原本毫無關聯，所以連「這張預約開過單了沒」都查不出來。
--
-- 用 nullable 外鍵而不是 NOT NULL：既有工單沒有預約來源，逼它們補值會讓
-- migration 直接失敗。沒有預約來源的工單 appointment_id 就是 NULL。
--
-- 注意 SQLite 的 ALTER TABLE ADD COLUMN 沒有 IF NOT EXISTS 語法，重複執行會
-- 報 duplicate column name；migration runner 以 schema_migrations 記錄版本、
-- 只會執行一次，因此不需要（也不能）加防重複的寫法。
ALTER TABLE work_orders ADD COLUMN appointment_id INTEGER REFERENCES appointments(id);

-- 查「這張預約開過哪些工單」用得到（例如預約列表顯示已開單）。
CREATE INDEX IF NOT EXISTS idx_work_orders_appointment ON work_orders(appointment_id);
