import urllib.request, urllib.parse, http.cookiejar, re

BASE = "http://localhost:8090"
cj = http.cookiejar.CookieJar()
op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))

def post_form(path, data, headers=None):
    body = urllib.parse.urlencode(data).encode("utf-8")
    r = urllib.request.Request(BASE + path, method="POST", data=body)
    r.add_header("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
    if headers:
        for k, v in headers.items():
            r.add_header(k, v)
    try:
        resp = op.open(r, timeout=8)
        body = resp.read().decode("utf-8", "replace")
        return resp.status, body, resp.geturl()
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8", "replace")
        return e.code, body, e.url

def get(path, headers=None):
    r = urllib.request.Request(BASE + path)
    if headers:
        for k, v in headers.items():
            r.add_header(k, v)
    try:
        resp = op.open(r, timeout=8)
        body = resp.read().decode("utf-8", "replace")
        return resp.status, body, resp.geturl()
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8", "replace")
        return e.code, body, e.url

print("=== AUTH (owner) ===")
st, body, u = post_form("/login", {"username":"owner","password":"Owner001!"})
print("login:", st, "session:", any(c.name=="session_token" for c in cj))
hdr = {"Cookie":"***" + [c.value for c in cj if c.name=="session_token"][0]}

def extract_id(html, marker, around="edit"):
    """從 HTML 找出 marker 附近 /marker/ID 的編輯連結。"""
    # 找 href="/marker/ID/..." 
    m = re.search(rf'href="/{marker}/(\d+)/(?:{around}|delete)"', html)
    if m:
        return int(m.group(1))
    return None

print("\n=== 建零件（料號 OIL-100）===")
st, body, u = post_form("/inventory", {"part_number":"OIL-100","name":"機油測試油","bin_location":"A-09-9","selling_price":"800","stock_quantity":"10","min_safety_stock":"2"})
print("create part:", st, body.strip().split('<main>')[1].split('</main>')[0][:120])
part_id = extract_id(body, "inventory", "edit")
print("  part_id:", part_id)

print("\n=== 建客戶（陳大文）===")
st, body, u = post_form("/customers", {"name":"陳大文","phone":"0911111111","address":"台北市"})
print("create customer:", st, body.strip().split('<main>')[1].split('</main>')[0][:120])
cust_id = extract_id(body, "customers", "edit")
print("  customer_id:", cust_id)

print("\n=== 建車輛（ST-9999）===")
st, body, u = post_form("/vehicles", {"customer_id":cust_id,"plate_number":"ST-9999","brand_model":"Camry","manufacture_year":"2020","vin":"VIN9999"})
print("create vehicle:", st, body.strip().split('<main>')[1].split('</main>')[0][:120])
veh_id = extract_id(body, "vehicles", "edit")
print("  vehicle_id:", veh_id)

print("\n=== 建工單 ===")
st, body, u = post_form("/work-orders", {"vehicle_id":veh_id,"current_mileage":"50000","customer_complaint":"引擎異音","approval_status":"not_required"})
print("create workorder:", st, body.strip().split('<main>')[1].split('</main>')[0][:120])
wo_id = extract_id(body, "work-orders")
if not wo_id:
    # 從 redirect URL 抓 /work-orders/ID
    m = re.search(r'/work-orders/(\d+)(?:/|$)', u)
    wo_id = int(m.group(1)) if m else None
if not wo_id:
    m = re.search(r'(\d{2})</a></td><td><b>ST-', body)
    wo_id = int(m.group(1)) if m else None
print("  workorder_id:", wo_id)

print("\n=== 建工單項目 labor ===")
st, body, u = post_form(f"/work-orders/{wo_id}/items", {"item_type":"labor","description":"診斷檢查","quantity":"2","unit_price":"1500","primary_technician_id":"1"})
print("add labor:", st, body.strip().split('<main>')[1].split('</main>')[0][:120])

print("\n=== 建工單項目 part ===")
st, body, u = post_form(f"/work-orders/{wo_id}/items", {"item_type":"part","inventory_item_id":part_id,"description":"更換機油","quantity":"2","unit_price":"800"})
print("add part:", st, body.strip().split('<main>')[1].split('</main>')[0][:120])

print("\n=== 狀態流轉：pending -> in_progress -> waiting_parts -> completed -> paid ===")
for s in ["in_progress","waiting_parts","in_progress","completed"]:
    st, body, u = post_form(f"/work-orders/{wo_id}/status", {"status":s})
    print(f"  -> {s}: {st} {body.strip().split('<main>')[1].split('</main>')[0][:90]}")

print("\n[1] SOP toggle ==")
st, body, u = post_form(f"/work-orders/{wo_id}/sop", {})
print("  toggle sop:", st, body.strip().split('<main>')[1].split('</main>')[0][:90])

print("\n[2] 審批 approval_status=approved ==")
st, body, u = post_form(f"/work-orders/{wo_id}/approval", {"approval_status":"approved"})
print("  approval:", st, body.strip().split('<main>')[1].split('</main>')[0][:90])

print("\n[3] 結帳扣料（status -> paid）===")
st, body, u = post_form(f"/work-orders/{wo_id}/status", {"status":"paid"})
print("  -> paid:", st, body.strip().split('<main>')[1].split('</main>')[0][:90])

print("\n[4] 結帳後即時查庫存（OIL-100 應 10-2=8）===")
st, body, u = get(f"/inventory/{part_id}", hdr)
m = re.search(r'OIL-100.*?庫存</th>\s*<td[^>]*>([^<]+)', body, re.S)
print("  inventory row:", (body.split('OIL-100')[1].split('</tr>')[0])[:150] if 'OIL-100' in body else "NOT FOUND")

print("\n[5] 結帳後查工單（總額應 2*1500+2*800=6400）===")
st, body, u = get(f"/work-orders/{wo_id}", hdr)
amt = re.search(r'>\s*<td[^>]*class="num"\s*>\s*([0-9,]+)', body)
print("  total_amount shown:", body.split('應付總額')[1].split('</b>')[0][:80] if '應付總額' in body else "?")

print("\n[6] 客戶累計消費（陳大文 total_spent 應 += 6400）===")
st, body, u = get("/customers", hdr)
seg = body.split('陳大文')[1].split('</tr>')[0]
print("  陳大文 row:", seg[:200])

print("\n=== 一致性保護測試 ===")
print("\n[7] 冪等：重複結帳（應未重複扣料，庫存仍 8）===")
st, body, u = post_form(f"/work-orders/{wo_id}/status", {"status":"paid"})
st, body2, _ = get(f"/inventory/{part_id}", hdr)
seg2 = body2.split('OIL-100')[1].split('</tr>')[0]
print("  re-paid status:", st)
print("  inventory after re-checkout:", seg2[:150])

print("\n[8] 已結帳拒絕改狀態（應被擋）===")
st, body, u = post_form(f"/work-orders/{wo_id}/status", {"status":"pending"})
print("  reopen paid:", st, body.strip().split('<main>')[1].split('</main>')[0][:120])

print("\n[9] 已結帳拒絕改項目（應被擋）===")
st, body, u = post_form(f"/work-orders/{wo_id}/items", {"item_type":"labor","description":"未授權修改"})
print("  modify paid item:", st, body.strip().split('<main>')[1].split('</main>')[0][:120])

print("\n[10] 已結帳拒絕補工時（應被擋）===")
st, body, u = post_form(f"/work-orders/{wo_id}/worklogs", {"technician_id":"1","work_date":"2026-10-03","hours":"1","hourly_rate":"1500","commission_rate":"10"})
print("  add worklog on paid:", st, body.strip().split('<main>')[1].split('</main>')[0][:120])

print("\n=== Phase 2 入口行為檢查（只查存在性與保護）===")
print("\n[11] 技師工時列表 /work-orders/{id}/worklogs ==")
st, body, u = get(f"/work-orders/{wo_id}/worklogs", hdr)
print("  status:", st, "| 標題:", re.search(r'<h2>(.*?)</h2>', body, re.S).group(1).strip() if re.search(r'<h2>', body) else "?")

print("\n[12] 收據列印 /work-orders/{id}/receipt ==")
st, body, u = get(f"/work-orders/{wo_id}/receipt", hdr)
print("  status:", st, "| 應付總額:", re.search(r'應付總額[^<]*<b[^>]*>([^<]+)', body).group(1).strip() if '應付總額' in body else "?")

print("\n[13] 照片列表 /work-orders/{id}/photos ==")
st, body, u = get(f"/work-orders/{wo_id}/photos", hdr)
print("  status:", st, "| 標題:", re.search(r'<h2>(.*?)</h2>', body, re.S).group(1).strip() if re.search(r'<h2>', body) else "?")

print("\nDONE")
