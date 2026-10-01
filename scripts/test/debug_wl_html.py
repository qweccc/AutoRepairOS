import urllib.request, urllib.parse, http.cookiejar, re

BASE = "http://localhost:8090"
cj = http.cookiejar.CookieJar()
op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))

def req(path, data=None):
    body = urllib.parse.urlencode(data, encoding="utf-8").encode("utf-8") if data else None
    r = urllib.request.Request(BASE + path, data=body)
    try:
        resp = op.open(r)
        return resp.status, resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")

def dump(s):
    with open("_wl_debug.txt", "a", encoding="utf-8") as f:
        f.write(s + "\n")

st, _ = req("/login", {"username": "owner", "password": "Owner001!"})
st, html = req("/work-orders")
wids = sorted(set(re.findall(r'/work-orders/(\d+)', html)), key=int)
wid = wids[-1]

import datetime
today = datetime.date.today().isoformat()
st, _ = req(f"/work-orders/{wid}/worklogs", {
    "technician_id": "3", "work_date": today, "hours": "3.5",
    "hourly_rate": "500", "commission_rate": "10", "notes": "DB test",
})
st, html = req(f"/work-orders/{wid}/worklogs")

dump("H2 markers:")
for m in re.finditer(r'<h2>(.*?)</h2>', html, re.S):
    dump("  " + m.group(1).strip())

blocks = re.findall(r'<table>(.*?)</table>', html, re.S)
dump("NUM TABLES: " + str(len(blocks)))
for i, blk in enumerate(blocks):
    dump("--- TABLE " + str(i) + " (len " + str(len(blk)) + ") ---")
    dump(blk[:2500])

with open("_wl_full.html", "w", encoding="utf-8") as f:
    f.write(html)
