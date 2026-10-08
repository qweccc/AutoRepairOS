# -*- coding: utf-8 -*-
import os, shutil, sqlite3

ROOT = "C:\\Users\\fr\\AutoRepairOS"
MAIN = os.path.join(ROOT, "data", "autorepair.db")
ISO = os.path.join(ROOT, "data_iso", "autorepair_iso.db")

print("MAIN exists:", os.path.exists(MAIN), "size:", os.path.getsize(MAIN) if os.path.exists(MAIN) else "?")
print("ISO exists:", os.path.exists(ISO), "size:", os.path.getsize(ISO) if os.path.exists(ISO) else "?")

if os.path.exists(MAIN):
    shutil.copy(MAIN, ISO)
    print("copied fresh")

con = sqlite3.connect(ISO)
con.row_factory = sqlite3.Row
print("=== users table ===")
rows = con.execute("SELECT id, username, role FROM users").fetchall()
for r in rows:
    print(dict(r))
print("users count:", len(rows))
con.close()
