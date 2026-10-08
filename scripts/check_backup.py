import sqlite3, os, subprocess

snap="C:/Users/fr/AutoRepairOS/scripts/snap.db"
if os.path.exists(snap): os.remove(snap)
subprocess.run(["docker","cp","autorepairos_autorepairos-data:/app/data/autorepair.db",snap],check=True,capture_output=True)
print("copied. bytes:", os.path.getsize(snap))
con=sqlite3.connect(snap)
con.row_factory=sqlite3.Row
cur=con.cursor()

print("\n=== scheduled_tasks ===")
for r in cur.execute("SELECT task_key,name,interval_minutes,enabled,next_run_at,last_run_at,run_count,last_error FROM scheduled_tasks ORDER BY name"):
    print(dict(r))

print("\n=== backup_records ===")
for r in cur.execute("SELECT backup_date,db_backup_path,db_size,photo_count,photo_size,status,error_message FROM backup_records ORDER BY backup_date DESC"):
    print(dict(r))

print("\n=== notifications (recent) ===")
for r in cur.execute("SELECT id,kind,channel,status,sent_at,created_at,subject FROM notifications ORDER BY id DESC LIMIT 15"):
    print(dict(r))

print("\n=== tables containing backup/backup_records ===")
for r in cur.execute("SELECT name FROM sqlite_master WHERE type='table' AND (name LIKE '%backup%' OR name LIKE '%sched%' OR name LIKE '%notif%' OR name LIKE '%task%' OR name LIKE '%maintenance%' OR name LIKE '%photo%' OR name LIKE '%work_order%' OR name LIKE '%customer%' OR name LIKE '%vehicle%' OR name LIKE '%inventory%' OR name LIKE '%technician%' OR name LIKE '%worklog%' OR name LIKE '%report%' OR name LIKE '%line%' OR name LIKE '%device%' OR name LIKE '%user%' OR name LIKE '%role%' OR name LIKE '%permission%' OR name LIKE '%session%' OR name LIKE '%log%') ORDER BY name"):
    print(dict(r))

os.remove(snap)
