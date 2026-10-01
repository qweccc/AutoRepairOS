# -*- coding: utf-8 -*-
import urllib.request, urllib.parse, http.cookiejar, re

BASE = "http://localhost:8090"
cj = http.cookiejar.CookieJar()
op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))

def req(path, data=None):
    body = urllib.parse.urlencode(data, encoding="utf-8").encode("utf-8") if data else None
    r = urllib.request.Request(BASE + path, data=body)
    try:
        resp = op.open(r)
        return resp.status, resp.read().decode("utf-8", "replace"), resp.headers.get("Content-Type")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace"), e.headers.get("Content-Type")

# owner login
st, body, ct = req("/login", {"username": "owner", "password": "Owner001!"})
print("owner login:", st)
if st != 200:
    print("  body:", body[:200])

# work orders
st, html, _ = req("/work-orders")
wids = sorted(set(re.findall(r'/work-orders/(\d+)', html)), key=int)
print("work orders:", wids)
if not wids:
    print("NO WORK ORDERS - cannot proceed")
    raise SystemExit(1)
wid = wids[-1]
print("wid:", wid)

# worklogs page (triggers table query)
st, html, ct = req(f"/work-orders/{wid}/worklogs")
print("worklogs page status:", st, ct)
for kw in ["1750", "175", "查詢工時紀錄失敗", "error", "Error", "內政部", "database is locked", "SQLITE", "table"]:
    print(f"  contains {kw!r}:", kw in html)
