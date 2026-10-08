import sqlite3, sys
con = sqlite3.connect(sys.argv[1])
tabs = con.execute("SELECT name FROM sqlite_master WHERE type='table'").fetchall()
print("tables:", [t[0] for t in tabs])
try:
    rows = con.execute("SELECT id, username, password FROM users").fetchall()
    print("users rows:", rows)
except Exception as e:
    print("users query err:", repr(e))
con.close()
