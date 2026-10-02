# -*- coding: utf-8 -*-
# Phase 2 端到端測試：技師工時 / 佣金、收據列印、車損照片
# 注意：Windows 控制台對 Python print 中文會崩（cp1252），所以結果寫到 out.txt 再讀回。
import urllib.request, urllib.parse, urllib.error, http.cookiejar, re, uuid, time

TODAY = time.strftime("%Y-%m-%d")
THIS_MONTH = TODAY[:7]

BASE = "http://localhost:8099"
results = []
out = []


def req(op, path, data=None):
    body = urllib.parse.urlencode(data, encoding="utf-8").encode("utf-8") if data else None
    r = urllib.request.Request(BASE + path, data=body)
    try:
        resp = op.open(r)
        return resp.status, resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")


def check(name, cond, detail=""):
    results.append(cond)
    with open("out.txt", "a", encoding="utf-8") as f:
        f.write("[%s] %s %s\n" % ("PASS" if cond else "FAIL", name, detail))


op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
st, _ = req(op, "/login", {"username": "counter", "password": "Counter001!"})
check("counter 登入 200", st == 200)

# 測試容器可能是全新空 DB，不能假設已有客戶/車輛/工單。
# 先自建一條完整鏈路（客戶 → 車輛 → 工單）再往下測。
_ts = int(time.time())
st, _ = req(op, "/customers", {
    "name": "Phase2 客戶", "phone": f"09{_ts % 100000000:08d}",
    "notification_preference": "line",
})
st, html_c = req(op, "/customers")
_cids = sorted(set(re.findall(r'/customers/(\d+)', html_c)), key=int)
check("建立 Phase2 測試客戶", st == 200 and len(_cids) > 0)
_cid = _cids[0] if _cids else "1"

st, _ = req(op, "/vehicles", {
    "customer_id": _cid, "plate_number": f"P2-{_ts % 100000}",
    "brand_model": "Test Car", "manufacture_year": "2021",
    "current_mileage": "10000",
})
check("建立 Phase2 測試車輛", st == 200)

st, html_v = req(op, "/vehicles")
_vids = sorted(set(re.findall(r'/vehicles/(\d+)', html_v)), key=int)
_vid = _vids[-1] if _vids else "1"

st, _ = req(op, "/work-orders", {
    "vehicle_id": _vid, "customer_id": _cid,
    "current_mileage": "10000", "customer_complaint": "Phase2 測試",
})
check("建立 Phase2 測試工單", st == 200)

# 找一個真實工單
st, html = req(op, "/work-orders")
wids = sorted(set(re.findall(r'/work-orders/(\d+)', html)), key=int)
check("工單列表有資料", len(wids) > 0, f"ids={wids}")
wid = wids[-1]

# --- 1. 技師工時頁 + 建工時 ---
st, html = req(op, f"/work-orders/{wid}/worklogs")
check("技師工時頁 200", st == 200)

st, body = req(op, f"/work-orders/{wid}/worklogs", {
    "technician_id": "3",
    "work_date": TODAY,
    "hours": "3.5",
    "hourly_rate": "500",
    "commission_rate": "10",
    "notes": "Phase2 測試工時",
})
check("新增工時紀錄 200", st == 200, body[:80] if st != 200 else "")

st, html = req(op, f"/work-orders/{wid}/worklogs")
check("工時紀錄顯示", "Phase2 測試工時" in html)
check("工資計算 1750", "1750" in html)
check("佣金計算 175", "175" in html)

# --- 2. 技師佣金總覽 ---
st, html = req(op, "/technicians/commissions")
check("技師佣金統計頁 200", st == 200)
m = re.search(r'<th[^>]*>總佣金</th><td[^>]*>([0-9]+)</td>', html)
check("總佣金為 175 的倍數",
      m is not None and int(m.group(1)) > 0 and int(m.group(1)) % 175 == 0,
      f"總佣金={m.group(1) if m else 'n/a'}")
check("技師明細表存在", "tech" in html)

# --- 3. 收據列印 ---
st, html = req(op, f"/work-orders/{wid}/receipt")
check("收據頁 200", st == 200)
check("收據含店名", "AutoRepair OS" in html)
check("收據標示含稅總額", "含稅" in html)
check("收據含應付總額", "應付總額" in html)
check("收據含列印按鈕", "window.print()" in html)
check("收據含 print CSS", "@media print" in html)

# --- 4. 照片上傳 ---
st, html = req(op, f"/work-orders/{wid}/photos")
# 頁面標題為「工單照片」（含上傳表單與照片網格）
check("照片頁 200", st == 200 and ("工單照片" in html or "上傳照片" in html))
check("照片頁含上傳表單", "multipart/form-data" in html)

# 上傳測試圖片（最小 PNG）
png = bytes.fromhex(
    "89504e470d0a1a0a0000000d494844520000000100000001080600000"
    "01f15c4890000000a49444154789c6360000002000100ffff0300000600"
    "0557bfabd40000000049454e44ae426082"
)
boundary = "----Phase2Boundary" + uuid.uuid4().hex
parts = []
parts.append(f"--{boundary}\r\n".encode())
parts.append(b'Content-Disposition: form-data; name="photo_type"\r\n\r\ndamage\r\n')
parts.append(f"--{boundary}\r\n".encode())
parts.append(b'Content-Disposition: form-data; name="note"\r\n\r\n')
parts.append("Phase2 測試車損照片".encode("utf-8"))
parts.append(b"\r\n")
parts.append(f"--{boundary}\r\n".encode())
parts.append(b'Content-Disposition: form-data; name="photo"; filename="test.png"\r\n')
parts.append(b"Content-Type: image/png\r\n\r\n")
parts.append(png)
parts.append(f"\r\n--{boundary}--\r\n".encode())
payload = b"".join(parts)

r = urllib.request.Request(BASE + f"/work-orders/{wid}/photos", data=payload)
r.add_header("Content-Type", f"multipart/form-data; boundary={boundary}")
try:
    resp = op.open(r)
    st = resp.status
except urllib.error.HTTPError as e:
    st = e.code
check("上傳照片 200", st == 200, f"status={st}")

st, html = req(op, f"/work-orders/{wid}/photos")
check("照片顯示於頁面", "Phase2 測試車損照片" in html)
m = re.search(r'/photos/(wo\d+_\d+\.png)', html)
check("照片檔案已產生", m is not None, m.group(1) if m else "not found")
if m:
    fn = m.group(1)
    st, _ = req(op, f"/photos/{fn}")
    check("照片可存取 200", st == 200, f"status={st}")
