DROP INDEX IF EXISTS idx_veh_ins_expiry;
ALTER TABLE work_orders DROP COLUMN claim_note;
ALTER TABLE work_orders DROP COLUMN claim_date;
ALTER TABLE work_orders DROP COLUMN claim_amount;
ALTER TABLE vehicle_insurances DROP COLUMN expiry_date;
ALTER TABLE vehicle_insurances DROP COLUMN policy_number;
