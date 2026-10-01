import sqlite3, os
db='/app/data/autorepair.db'
if not os.path.exists(db):
    print('DB not found'); raise SystemExit(1)
con=sqlite3.connect(db)
con.row_factory=sqlite3.Row

def d():
    for r in con.execute("SELECT id, username, role FROM users ORDER BY id"): print(dict(r))
print("=== USERS ==="); d()

def d2():
    for r in con.execute("SELECT id, vehicle_id, status FROM work_orders ORDER BY id"): print(dict(r))
print("=== WORK_ORDERS ==="); d2()

def d3():
    for r in con.execute("SELECT id FROM vehicles ORDER BY id"): print(dict(r))
print("=== VEHICLES ids ==="); d3()

def d4():
    bad=con.execute("SELECT wo.id, wo.vehicle_id, v.id FROM work_orders wo LEFT JOIN vehicles v ON v.id=wo.vehicle_id WHERE v.id IS NULL").fetchall()
    print("work_orders with missing vehicle:", [dict(r) for r in bad])
d4()

def d5():
    for r in con.execute("SELECT id, work_order_id, technician_id, hours, labor_amount, commission_amount, work_date FROM technician_worklogs ORDER BY id"): print(dict(r))
print("=== WORKLOGS ==="); d5()
