import sqlite3, sys, os

path = sys.argv[1] if len(sys.argv) > 1 else "C:/Users/fr/AutoRepairOS/autorepair_live.db"
print("DB:", path, "size:", os.path.getsize(path))
try:
    con = sqlite3.connect(path)
    con.row_factory = sqlite3.Row
    cur = con.execute("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
    tables = [r[0] for r in cur.fetchall()]
    print("TABLES:", tables)
    if tables:
        for t in ["work_orders","customers","inventory_items","work_order_items","customers"]:
            if t in tables:
                n = con.execute("SELECT COUNT(*) FROM %s" % t).fetchone()[0]
                print("  ROWS %s: %d" % (t, n))
except Exception as e:
    print("ERR:", e)
