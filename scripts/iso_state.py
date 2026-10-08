import sqlite3, sys
con = sqlite3.connect(sys.argv[1])
print("vehicles:", con.execute("SELECT id, plate_number FROM vehicles").fetchall())
print("insurance:", con.execute("SELECT id, code, name, active FROM insurance_companies").fetchall())
print("wos:", con.execute("SELECT id, is_insurance, insurance_company_id, claim_no FROM work_orders").fetchall())
con.close()
