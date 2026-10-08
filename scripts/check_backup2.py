import sqlite3, subprocess

# 從容器內 cat 出 DB 二進制
subprocess.run(["docker","exec","autorepairos-autorepairos-1","cat","/app/data/autorepair.db","-"],check=True,capture_output=True)

import io, sys
binf=io.BytesIO(sys.stdin.buffer.read())
con=sqlite3.connect(binf.read())
binf.close()
con.row_factory=sqlite3.Row
cur=con.cursor()

print("=== scheduled_tasks ===")
for r in cur.execute("SELECT task_key,name,interval_minutes,enabled,next_run_at,last_run_at,run_count,last_error FROM scheduled_tasks ORDER BY name"):
    print(dict(r))

print("\n=== backup_records ===")
for r in cur.execute("SELECT backup_date,db_backup_path,db_size,photo_count,photo_size,status,error_message FROM backup_records ORDER BY backup_date DESC"):
    print(dict(r))

print("\n=== notifications (recent 15) ===")
for r in cur.execute("SELECT id,kind,channel,status,sent_at,created_at,subject FROM notifications ORDER BY id DESC LIMIT 15"):
    print(dict(r))

os_close=con
