import urllib.request, urllib.parse, http.cookiejar, re, time

BASE = "http://localhost:8090"
# 車牌需唯一以符合 DB UNIQUE 約束，重跑測試時用時間戳避免衝突
PLATE = f"TEST-{int(time.time()) % 100000}"
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

def post(p, d): return req(p, d)
def get(p): return req(p)

results = []
def check(name, cond, detail=""):
    results.append(cond)
    print(f"[{'PASS' if cond else 'FAIL'}] {name} {detail}")

st, _ = post("/login", {"username": "counter", "password": "Counter001!"})
check("counter 登入", st == 200)

st, body = post("/vehicles", {
    "customer_id": "1",
    "plate_number": PLATE,
    "brand_model": "Honda Civic",
    "manufacture_year": "2019",
    "current_mileage": "30000",
})
check("建立車輛", st == 200, body[:80])

st, html = get("/vehicles")
check("車輛列表含車牌", PLATE in html)

st, html = get("/vehicles")
vids = re.findall(r'/vehicles/(\d+)', html)
vid = sorted(set(vids), key=int)[-1] if vids else None
print("   vehicle ids:", sorted(set(vids)))

st, body = post("/work-orders", {
    "customer_id": "1",
    "vehicle_id": vid or "1",
    "type": "maintenance",
    "customer_complaint": "定期保養",
    "current_mileage": "30000",
})
check("建立工單", st == 200, body[:80])

st, html = get("/work-orders")
wids = sorted(set(re.findall(r'/work-orders/(\d+)', html)), key=int)
print("   work_order ids:", wids)
check("工單列表非空", len(wids) > 0)

if wids:
    wid = wids[-1]
    st, html = get(f"/work-orders/{wid}")
    check(f"工單 #{wid} 詳情頁", st == 200 and "保養" in html, f"status={st}")

st, html = get("/inventory")
check("庫存頁", st == 200)

st, _ = get("/logout")
check("登出", st == 200)

print("\n=== SUMMARY ===")
print(f"PASS={sum(results)} FAIL={len(results)-sum(results)}")
