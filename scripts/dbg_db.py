import sqlite3, os
p="C:/Users/fr/AutoRepairOS/scripts/snap_full.db"
print("bytes:", os.path.getsize(p))
con=sqlite3.connect(p)
cur=con.cursor()
tables=[r[0] for r in cur.execute("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")]
print("TABLES (%d):" % len(tables))
for t in tables: print("  ",t)
print("\n=== work_orders status distribution ===")
for r in cur.execute("SELECT status, COUNT(*) n FROM work_orders GROUP BY status ORDER BY status"):
    print("  %-12s %d 筆" % (r[0], r[1]))
print("\n=== work_order_items count ===")
print("  ", cur.execute("SELECT COUNT(*) FROM work_order_items").fetchone()[0])
print("\n=== inventory_items ===")
for r in cur.execute("SELECT part_number,name,stock_quantity,min_safety_stock,COALESCE(supplier_id,0) FROM inventory_items ORDER BY part_number"):
    print("  %-12s 庫存=%s/%s | 供應商=%s" % (r[0], r[2], r[3], r[4]))
print("\n=== suppliers ===")
for r in cur.execute("SELECT id,name,phone FROM suppliers ORDER BY id"):
    print("  #%s | %s | %s" % (r[0], r[1], r[2]))
print("\n=== customers ===")
for r in cur.execute("SELECT id,name,phone,total_spent FROM customers ORDER BY id"):
    print("  #%s | %s | total=%s" % (r[0], r[1], r[3]))
