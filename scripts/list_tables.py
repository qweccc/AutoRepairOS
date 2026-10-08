import subprocess, sqlite3, os

snap="C:/Users/fr/AutoRepairOS/scripts/snap2.db"
if os.path.exists(snap): os.remove(snap)
subprocess.run(["docker","exec","autorepairos-autorepairos-1","cat","/app/data/autorepair.db",snap],check=True,capture_output=True)
print("snapshot bytes:", os.path.getsize(snap))

con=sqlite3.connect(snap)
cur=con.cursor()
try:
    tables=[r[0] for r in cur.execute("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")]
    print("\nTABLES (%d):"%len(tables))
    for t in tables: print("  ",t)
    print("\nscheduled_tasks:", "scheduled_tasks" in tables)
    print("backup_records:", "backup_records" in tables)
    print("notifications:", "notifications" in tables)
except Exception as e:
    print("QUERY ERR:", e)
finally:
    os.remove(snap)
