import sys, sqlite3, os
try:
    import bcrypt
except Exception:
    bcrypt=None
con=sqlite3.connect(sys.argv[1])
rows=con.execute("SELECT username,password FROM users WHERE username='owner'").fetchall()
for u,p in rows:
    print("user:",u)
    print("hash:",p)
    if bcrypt:
        for cand in ["Owner001!","Owner001","owner","owner001","owner001!","Owner001@"]:
            ok=bcrypt.checkpw(cand,p.encode("utf-8"))
            if ok:
                print("MATCH:",repr(cand))
    con.close(); break
