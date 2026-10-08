import urllib.request, urllib.parse, http.cookiejar, re, time

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

# 已結帳（paid）的工單不可再變更狀態（避免帳務與庫存對不上），
# 所以每次測試都要自建一張全新的工單，不能沿用清單裡最後一張。
_ts = int(time.time())
st, _ = post("/customers", {
    "name": "狀態流轉測試", "phone": f"09{_ts % 100000000:08d}",
    "notification_preference": "line",
})
st, html_c = get("/customers")
cids = sorted(set(re.findall(r'/customers/(\d+)', html_c)), key=int)
cid = cids[0] if cids else "1"

post("/vehicles", {
    "customer_id": cid, "plate_number": f"ST-{_ts % 100000}",
    "brand_model": "Test Car", "current_mileage": "10000",
})
st, html_v = get("/vehicles")
vids = sorted(set(re.findall(r'/vehicles/(\d+)', html_v)), key=int)
vid = vids[-1] if vids else "1"

st, _ = post("/work-orders", {
    "vehicle_id": vid, "customer_id": cid,
    "current_mileage": "10000", "customer_complaint": "狀態流轉測試",
})
check("建立測試工單", st == 200)

st, html = get("/work-orders")
wids = sorted(set(re.findall(r'/work-orders/(\d+)', html)), key=int)
wid = wids[-1]
print("testing work order:", wid)

# 已結帳（paid）後工單內容就鎖定了，所以先把審批/項目/SOP 做完，
# 最後才走狀態流轉到 paid。
st, body = post(f"/work-orders/{wid}/approval", {"approval_status": "pending"})
check("設定 approval=pending", st == 200, body[:60] if st != 200 else "")
st, body = post(f"/work-orders/{wid}/approval", {"approval_status": "approved"})
check("設定 approval=approved", st == 200, body[:60] if st != 200 else "")

# 加工作項目（工時/配件）
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

# 狀態流轉：pending -> in_progress -> waiting_parts -> in_progress -> completed -> paid
for s in ["in_progress", "waiting_parts", "in_progress", "completed", "paid"]:
    st, body = post(f"/work-orders/{wid}/status", {"status": s})
    ok = st == 200
    check(f"status -> {s}", ok, body[:60] if not ok else "")

# 檢查詳情頁是否顯示已完成
st, html = get(f"/work-orders/{wid}")
check("工單狀態 completed 顯示", "已完成" in html or "completed" in html.lower())

# 已結帳後再改內容應被拒（帳務與庫存一致性保護）
st, body = post(f"/work-orders/{wid}/sop", {"sop_key": "oil_change"})
check("已結帳工單拒絕變更內容", st != 200, f"status={st}")

print("\n=== SUMMARY ===")
print(f"PASS={sum(results)} FAIL={len(results)-sum(results)}")
