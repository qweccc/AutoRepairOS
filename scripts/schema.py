import sqlite3, sys

db = sqlite3.connect(sys.argv[1] + '/autorepair_snapshot.db')
cur = db.execute("SELECT name, sql FROM sqlite_master WHERE type='table' ORDER BY name")
tables = []
for r in cur.fetchall():
    tables.append((r[0], r[1] if r[1] else ""))
print("=== TABLES ===")
for name, sql in tables:
    print("  " + name)
    if 'CREATE TABLE' in sql:
        for ln in sql.strip().splitlines():
            if ln.strip():
                print("    " + ln.strip())
print("\nDONE")
