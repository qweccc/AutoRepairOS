import sqlite3, sys
con = sqlite3.connect(sys.argv[1])
rows = con.execute("SELECT id, username, password FROM users").fetchall()
for r in rows:
    print(r)
con.close()
