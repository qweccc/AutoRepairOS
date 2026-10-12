-- 021_bay_and_mileage_history
--
-- 補兩項 SRS 缺口：
--   FR-APP-02 / FR-APP-04  預約工位（頂高機）與同時段衝突檢查
--   FR-VH-03              車輛里程獨立履歷
--
-- 純 ADD COLUMN / CREATE TABLE，既有資料不受影響。
-- 已存在的預約 bay 為空 = 「未指定工位」，不參與衝突檢查。

-- FR-APP-02：預約工位。用代碼而非自由輸入，才能做衝突檢查與調度。
-- 車廠工位通常是頂高機／鈑金校正／烤漆房等固定設備，數量有限。
ALTER TABLE appointments ADD COLUMN bay TEXT NOT NULL DEFAULT '';

-- FR-VH-03：車輛里程履歷。
--
-- 為什麼需要獨立表而不是只靠 work_orders.current_mileage：
--   1. 里程也會在「非進廠」時更新（客戶電話告知、路邊回報）
--   2. 需要記錄來源，才能分辨「這個數字是工單帶進來的還是手key的」
--   3. 需要偵測回退（碼表更換、key 錯數字），回退的資料會污染
--      「下次保養里程」的推算
-- recorded_by 記操作人，讓異常數據可以回溯到人。
CREATE TABLE IF NOT EXISTS vehicle_mileages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    vehicle_id INTEGER NOT NULL REFERENCES vehicles(id),
    mileage INTEGER NOT NULL,
    -- 來源：work_order = 進廠工單帶入 / manual = 手動登錄 / import = 舊系統匯入
    source TEXT NOT NULL DEFAULT 'manual',
    -- 關聯工單（source='work_order' 時有值），供追溯
    work_order_id INTEGER REFERENCES work_orders(id),
    notes TEXT DEFAULT '',
    recorded_by INTEGER REFERENCES users(id),
    created_at TEXT NOT NULL DEFAULT (datetime('now','localtime'))
);

CREATE INDEX IF NOT EXISTS idx_vm_vehicle ON vehicle_mileages(vehicle_id, created_at DESC);
