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
        return resp.status, resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")

# login
st, _ = req("/login", {"username": "counter", "password": "***"})
print("login", st)

# list work orders
st, html = req("/work-orders")
wids = sorted(set(re.findall(r'/work-orders/(\d+)', html)), key=int)
print("work orders:", wids)
wid = wids[-1]
print("wid:", wid)

# inspect this work order
st, html = req(f"/work-orders/{wid}")
m = re.search(r'vehicle_id[^>]*>\s*(\d+)', html)
print("vehicle_id match:", m.group(1) if m else None)

# add worklog
st, body = req(f"/work-orders/{wid}/worklogs", {
    "technician_id": "3",
    "work_date": "2026-09-30",
    "hours": "3.5",
    "hourly_rate": "500",
    "commission_rate": "10",
    "notes": "Phase2 測試工時",
})
print("POST worklogs", st, body[:120])

# get worklogs page
st, html = req(f"/work-orders/{wid}/worklogs")
print("GET worklogs", st)
for kw in ["Phase2 測試工時", "1750", "175", "查詢工時紀錄失敗", "error", "Error", "內政部"]:
    print(f"  contains {kw!r}:", kw in html)
