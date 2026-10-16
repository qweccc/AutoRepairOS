ALTER TABLE vehicle_insurances ADD COLUMN policy_number TEXT NOT NULL DEFAULT '';
ALTER TABLE vehicle_insurances ADD COLUMN expiry_date TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_veh_ins_expiry ON vehicle_insurances(expiry_date);

ALTER TABLE work_orders ADD COLUMN claim_amount REAL NOT NULL DEFAULT 0;
ALTER TABLE work_orders ADD COLUMN claim_date TEXT NOT NULL DEFAULT '';
ALTER TABLE work_orders ADD COLUMN claim_note TEXT NOT NULL DEFAULT '';
