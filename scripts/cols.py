import sqlite3, sys
path = sys.argv[1] if len(sys.argv) > 1 else "C:/Users/fr/AutoRepairOS/autorepair_live.db"
con = sqlite3.connect(path)
for t in ["work_orders","work_order_items","customers","inventory_items","work_order_photos","technician_worklogs"]:
    cols = [r[1] for r in con.execute("PRAGMA table_info(%s)" % t)]
    print("### %s:" % t)
    print("   ", cols)
