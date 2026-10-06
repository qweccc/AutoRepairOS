-- 下次保養拆成「公里數」與「日期」兩條獨立基準
--
-- 為什麼需要：舊系統 CAR35 的 Car 表把保養資訊拆在幾個欄位
--   Last Maintain Date (95%)  最後保養日
--   Last Mileage       (53%)  最後保養時的里程
--   Maintain Month     (0.2%) 保養間隔月數（幾乎沒填）
-- 而「下次保養」是兩條獨立的觸發條件：
--   里程到 → 保養（車跑得多，時間還沒到）
--   日期到 → 保養（車少跑，里程還沒到）
-- 先到者為準。舊版把兩者併成一個顯示字串，看不出哪個先到，
-- 也無法從最後保養紀錄推算。
--
-- 設計：
--   last_maintenance_date / last_maintenance_mileage  = 基準（上次做了什麼）
--   next_maintenance_interval_months / _km            = 間隔（多久做一次）
--   next_maintenance_date / _mileage                  = 下次到期（可由基準+間隔推算）
--
-- 匯入時以工單歷史補上基準：最後一張已結帳工單的日期與里程最可靠
-- （舊系統的 Last Maintain Date 停在 2023 且與工單不一致）。

ALTER TABLE vehicles ADD COLUMN last_maintenance_date TEXT DEFAULT '';
ALTER TABLE vehicles ADD COLUMN last_maintenance_mileage INTEGER;
ALTER TABLE vehicles ADD COLUMN next_maintenance_interval_months INTEGER NOT NULL DEFAULT 6;
ALTER TABLE vehicles ADD COLUMN next_maintenance_interval_km INTEGER NOT NULL DEFAULT 5000;

CREATE INDEX IF NOT EXISTS idx_vehicles_next_maint_date ON vehicles(next_maintenance_date);
CREATE INDEX IF NOT EXISTS idx_vehicles_next_maint_km ON vehicles(next_maintenance_mileage);
