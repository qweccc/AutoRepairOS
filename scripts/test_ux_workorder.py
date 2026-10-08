import urllib.request, urllib.parse, http.cookiejar, re, sys, time
sys.stdout.reconfigure(encoding="utf-8", errors="replace")

BASE = "http://localhost:8090"
results = []

def check(name, cond, detail=""):
    results.append(cond)
    print(f"[{'PASS' if cond else 'FAIL'}] {name} {detail}")

def sess(user="counter", pwd="Counter001!"):
    cj = http.cookiejar.CookieJar()
    op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))
    body = urllib.parse.urlencode({"username": user, "password": pwd}).encode()
    try:
        op.open(urllib.request.Request(BASE + "/login", data=body))
    except urllib.error.HTTPError:
        pass
    return op

def req(op, path, data=None):
    body = urllib.parse.urlencode(data, encoding="utf-8").encode("utf-8") if data else None
    r = urllib.request.Request(BASE + path, data=body)
    try:
        resp = op.open(r)
        return resp.status, resp.geturl(), resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, path, e.read().decode("utf-8", "replace")

print("=" * 58)
print("開工單 UX 流程驗證")
print("=" * 58)

ts = int(time.time())

# 情境 A：客戶走進來報車牌 → 查客戶 → 開工單
print("\n【情境 A】客戶報車牌，櫃檯查詢後開工單")
op = sess()

st, _, _ = req(op, "/customers", {"name": "UX 測試客戶", "phone": f"0911{ts % 100000:06d}"})
check("A1 建立客戶", st == 200)

st, _, html = req(op, "/customers?q=UX")
cids = sorted(set(re.findall(r'work-orders/new\?customer=(\d+)', html)), key=int)
check("A2 客戶頁有開工單入口", len(cids) > 0, f"customer_ids={cids[:3]}")
cid = cids[0] if cids else "1"

st, _, html = req(op, f"/work-orders/new?customer={cid}")
check("A3 從客戶頁進入開工單頁", st == 200)
check("A4 頁面提示可搜尋", "搜尋車牌" in html or "選擇車輛" in html)

# 先建車輛才能開工單
plate = f"UX-{ts % 100000}"
st, _, _ = req(op, "/vehicles", {
    "customer_id": cid, "plate_number": plate, "brand_model": "Toyota Corolla",
    "manufacture_year": "2022", "current_mileage": "45000",
})
check("A5 建立車輛", st == 200)

st, _, html = req(op, f"/work-orders/new?customer={cid}")
check("A6 車輛已出現在選單", plate in html, f"找 {plate}")

m = re.search(r'<option value="(\d+)"[^>]*>' + plate, html)
vid = m.group(1) if m else ""
check("A7 可取得車輛 id", vid != "", f"vehicle_id={vid}")

st, url, _ = req(op, "/work-orders", {
    "vehicle_id": vid, "current_mileage": "45000",
    "customer_complaint": "定期保養", "approval_status": "not_required",
    "technician_id": "0",
})
check("A8 建立工單成功", st == 200 and "/work-orders/" in url, url)

# 情境 B：從車輛頁直接開工單（原本 404 的連結）
print("\n【情境 B】從車輛頁點開工單（原本 404）")
st, _, html = req(op, "/vehicles")
links = re.findall(r'href="/work-orders/new\?vehicle=(\d+)"', html)
check("B1 車輛頁有開工單連結", len(links) > 0, f"{len(links)} 筆")
if links:
    st, _, html = req(op, f"/work-orders/new?vehicle={links[0]}")
    check("B2 點擊不再是 404", st == 200, f"HTTP {st}")
    check("B3 自動預選該車輛", 'selected' in html)

# 情境 C：技師不應該能開工單
print("\n【情境 C】權限：技師不能開工單")
tech = sess("tech", "Tech001!")
st, _, body = req(tech, "/work-orders/new")
check("C1 技師進開工單頁被拒", st == 403, f"HTTP {st}")
st, _, _ = req(tech, "/work-orders/new?vehicle=1")
check("C2 帶參數也一樣被拒", st == 403, f"HTTP {st}")

# 情境 D：查無車牌要有指引
print("\n【情境 D】查無車輛時的錯誤指引")
st, _, html = req(op, "/work-orders/new?plate=NOSUCH-XYZ")
check("D1 查無車牌顯示錯誤", st == 200 and "查無車牌" in html)
check("D2 指引去新增車輛", "車輛" in html and "新增" in html)

# 情境 E：工單頁入口
print("\n【情境 E】工單頁入口")
st, _, html = req(op, "/work-orders")
check("E1 有開立新工單按鈕", "開立新工單" in html)
check("E2 舊的內嵌表單已移除", "建立工單</h2>" not in html)

print("\n" + "=" * 58)
p = sum(results)
print(f"PASS={p} FAIL={len(results)-p}")
