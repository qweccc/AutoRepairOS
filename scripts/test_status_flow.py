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

def post(p, d): return req(p, d)
def get(p): return req(p)

results = []
def check(name, cond, detail=""):
    results.append(cond)
    print(f"[{'PASS' if cond else 'FAIL'}] {name} {detail}")

post("/login", {"username": "counter", "password": "Counter001!"})

st, html = get("/work-orders")
wids = sorted(set(re.findall(r'/work-orders/(\d+)', html)), key=int)
wid = wids[-1]
print("testing work order:", wid)

# 狀態流轉：received -> in_progress -> waiting_parts -> ready -> completed
for s in ["in_progress", "waiting_parts", "in_progress", "completed", "paid"]:
    st, body = post(f"/work-orders/{wid}/status", {"status": s})
    ok = st == 200
    check(f"status -> {s}", ok, body[:60] if not ok else "")

# 檢查詳情頁是否顯示 completed
st, html = get(f"/work-orders/{wid}")
check("工單狀態 completed 顯示", "已完成" in html or "completed" in html.lower())

# 審批流程
st, body = post(f"/work-orders/{wid}/approval", {"approval_status": "pending"})
check("設定 approval=pending", st == 200, body[:60] if st != 200 else "")
st, body = post(f"/work-orders/{wid}/approval", {"approval_status": "approved"})
check("設定 approval=approved", st == 200, body[:60] if st != 200 else "")

# 加工作項目（工时/配件）
st, body = post(f"/work-orders/{wid}/items", {
    "description": "更換機油",
    "quantity": "1",
    "item_type": "part",
    "unit_price": "450",
})
check("新增工單項目", st == 200, body[:60] if st != 200 else "")

st, html = get(f"/work-orders/{wid}")
check("工單含項目「更換機油」", "更換機油" in html)

# SOP 切換
st, body = post(f"/work-orders/{wid}/sop", {"sop_key": "oil_change"})
check("SOP 切換", st == 200, body[:60] if st != 200 else "")

print("\n=== SUMMARY ===")
print(f"PASS={sum(results)} FAIL={len(results)-sum(results)}")
