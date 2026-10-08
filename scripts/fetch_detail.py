import sys, urllib.request, sqlite3
cf = sys.argv[1]
out = sys.argv[2]
open(out, "w").close()
try:
    import re
    m = re.search(r"session_token=([^;]+)", open(cf, encoding="utf-8").read())
    tok = m.group(1) if m else ""
    req = urllib.request.Request("http://localhost:8093/work-orders/13",
        headers={"Cookie": "session_token=" + tok})
    body = urllib.request.urlopen(req, timeout=10).read().decode("utf-8", "replace")
    open(out, "a", encoding="utf-8").write(body)
    print("bytes:", len(body))
    print("has 保險理賠:", "保險理賠" in body)
    print("has 自費:", "自費" in body)
    print("has CLM-999:", "CLM-999" in body)
    print("has E2E Insurance:", "E2E Insurance" in body)
    print("has 2026-10-01:", "2026-10-01" in body)
    print("has 1500:", "1500" in body)
except Exception as e:
    print("ERR:", repr(e))
