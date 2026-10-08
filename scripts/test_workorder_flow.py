#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""工單流程端到端測試：開單 → 估價 → 核准 → 維修 → 完工 → 結帳 → 收據。

對線上 container（預設 http://localhost:8090）執行，驗收重點是流程是否順暢：
每一步的 HTTP 狀態、金額計算是否正確、資料是否如預期落地。

用法：python scripts/test_workorder_flow.py [base_url]

注意兩個容易踩的坑：
  - urllib 預設會跟隨 302，會讓「302」斷言失效，所以自訂 NoRedirect。
  - 空 dict 會被 `if data else None` 判成 GET，POST 空 body 要用 `is not None`。
"""
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import http.cookiejar

sys.stdout.reconfigure(encoding="utf-8")

BASE = "http://localhost:8090"
ts = int(time.time()) % 100000

results = []


class NoRedirect(urllib.request.HTTPRedirectHandler):
    """不自動跟隨 3xx，才能斷言 handler 真的回 302 而非最終頁面的 200。"""
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def client(user, pwd):
    cj = http.cookiejar.CookieJar()
    op = urllib.request.build_opener(
        urllib.request.HTTPCookieProcessor(cj), NoRedirect())
    op.addheaders = [("Content-Type", "application/x-www-form-urlencoded")]
    body = urllib.parse.urlencode({"username": user, "password": pwd}).encode()
    try:
        r = op.open(BASE + "/login", body)
        r.read()
    except urllib.error.HTTPError as e:
        e.read()  # 302 預期
    return op


def req(op, path, data=None):
    body = urllib.parse.urlencode(data, encoding="utf-8").encode() if data is not None else None
    r = urllib.request.Request(BASE + path, data=body)
    try:
        resp = op.open(r)
        return resp.status, resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")


def post(op, path, d):
    return req(op, path, d)


def get(op, path):
    return req(op, path)


def check(name, cond, detail=""):
    results.append(bool(cond))
    mark = "PASS" if cond else "FAIL"
    print(f"[{mark}] {name}" + (f"  {detail}" if detail else ""))
    return bool(cond)


print("=" * 60)
print(f"工單流程端到端測試  {time.strftime('%Y-%m-%d %H:%M:%S')}")
print("=" * 60)

counter = client("counter", "Counter001!")
tech = client("tech", "Tech001!")
owner = client("owner", "Owner001!")

# ---------- 1. 開單前準備 ----------
print("\n--- 1. 建立客戶與車輛 ---")
st, _ = post(counter, "/customers", {
    "name": f"流程測試客戶{ts}", "phone": f"09{ts:07d}",
    "address": "台北市信義區", "notification_preference": "line",
})
check("建立客戶 302", st == 302, f"got={st}")

st, html = get(counter, "/customers")
cids = sorted(set(re.findall(r'/customers/(\d+)/edit', html)), key=int)
check("客戶出現在列表", len(cids) > 0, f"ids={cids[:3]}")
cid = cids[-1]

plate = f"FLOW-{ts}"
st, _ = post(counter, "/vehicles", {
    "customer_id": cid, "plate_number": plate, "brand_model": "Toyota Corolla Altis",
    "manufacture_year": "2020", "vin": "JTDBR32E000000",
    "next_maintenance_date": "2026-12-01", "next_maintenance_mileage": "80000",
})
check("建立車輛 302", st == 302, f"got={st}")

st, html = get(counter, "/vehicles")
vids = sorted(set(re.findall(r'/vehicles/(\d+)/edit', html)), key=int)
check("車輛出現在列表", len(vids) > 0)
vid = vids[-1]

# ---------- 2. 開工單 ----------
print("\n--- 2. 開立工單 ---")
st, _ = post(counter, "/work-orders", {
    "vehicle_id": vid, "technician_id": "3", "current_mileage": "45000",
    "approval_status": "pending", "customer_complaint": "煞車有異音，冷氣不冷",
})
check("建立工單 302", st == 302, f"got={st}")

st, html = get(counter, "/work-orders")
wo_ids = sorted(set(re.findall(r'/work-orders/(\d+)"', html)), key=int)
check("工單出現在列表", len(wo_ids) > 0, f"latest={wo_ids[-1] if wo_ids else '?'}")
wo = wo_ids[-1]
base = f"/work-orders/{wo}"

st, html = get(counter, base)
check("工單詳情頁 200", st == 200)
check("詳情頁顯示車牌", plate in html)
check("詳情頁顯示客戶反映", "煞車有異音" in html)
check("詳情頁初始狀態為待處理", "待處理" in html)

# ---------- 3. 估價（加入項目）----------
print("\n--- 3. 加入工資與零件 ---")
st, _ = post(counter, f"{base}/items", {
    "item_type": "labor", "description": "更換前煞車片工資",
    "quantity": "1", "unit_price": "800",
})
check("加入工資項目 302", st == 302, f"got={st}")

st, html = get(counter, "/inventory?low=1")
inv_ids = sorted(set(re.findall(r'/inventory/(\d+)/edit', html)), key=int)
if not inv_ids:
    st, html = get(counter, "/inventory")
    inv_ids = sorted(set(re.findall(r'/inventory/(\d+)/edit', html)), key=int)
inv = inv_ids[0]
inv = str(inv)  # regex 結果是字串，格式化前先統一

# 舊資料庫的零件庫存多為負數（見負庫存補正頁），挑「列表第一筆」可能撿到
# 庫存 < 2 的零件，結帳時會被「庫存不足」擋下（400），測試就與程式無關地失敗。
# 因此改為自建一筆庫存充足的測試零件，售價/庫存都已知。
probe_name = f"FLOW-PART-{ts}"
probe_price = 120
st, _ = post(counter, "/inventory", {
    "part_number": f"FLOW{ts}", "name": probe_name, "bin_location": "T-01",
    "cost_price": "80", "selling_price": str(probe_price),
    "stock_quantity": "50", "min_safety_stock": "5",
})
check("建立測試零件（庫存充足）302", st == 302, f"got={st}")

# 售價直接讀「加入項目」表單中該零件 option 的標籤，避免解析欄位位置。
# 格式：<option value="ID">名稱 / 料號 / 售價 980 / 庫存 30</option>
st, ord_html = get(counter, base)
m = re.search(r'<option value="(\d+)">' + re.escape(probe_name) + r'[^<]*</option>', ord_html)
check("測試零件出現在零件下拉", m is not None)
if m:
    inv = m.group(1)
selling = "0"
if m:
    ms = re.search(r'售價\s*([\d,]+)', m.group(0))
    if ms:
        selling = ms.group(1).replace(",", "")
print(f"    (零件 #{inv} 售價 = {selling})")

st, _ = post(counter, f"{base}/items", {
    "item_type": "part", "inventory_item_id": inv,
    "description": "", "quantity": "2", "unit_price": "0",
})
check("加入零件項目 302（售價自動帶入）", st == 302, f"got={st}")

# 重新抓取：上面的 html 是加入項目之前的頁面，必須重讀才看得到新小計
st, html = get(counter, base)
sub = re.search(r'項目小計</th>\s*<td class="num">([\d,]+)</td>', html)
total = re.search(r'工單總額</th>\s*<td class="num"[^>]*>([\d,]+)</td>', html)
sub_v = int(sub.group(1).replace(",", "")) if sub else 0
tot_v = int(total.group(1).replace(",", "")) if total else 0
check("小計 = 800 + 2 x 售價", sub_v == 800 + 2 * int(selling),
      f"小計={sub_v} 售價={selling}")
check("工單總額 = 項目小計", tot_v == sub_v, f"總額={tot_v}")

# ---------- 4. 核准 ----------
print("\n--- 4. 估價核准 ---")
check("初始需客戶核准", "待核准" in html)
st, _ = post(counter, f"{base}/approval", {"approval_status": "approved"})
check("標記已核准 302", st == 302, f"got={st}")
st, html = get(counter, base)
check("核准狀態已更新", "已核准" in html)

# ---------- 5. 維修進行 ----------
print("\n--- 5. 進入維修 ---")
for s, label in [("in_progress", "進行中"), ("waiting_parts", "待料")]:
    st, _ = post(counter, f"{base}/status", {"status": s})
    check(f"狀態 → {label}", st == 302, f"got={st}")
st, html = get(counter, base)
check("工單顯示待料", "待料" in html)

st, _ = post(counter, f"{base}/sop", {})
check("切換 SOP 302", st == 302, f"got={st}")
st, html = get(counter, base)
check("SOP 標記完成", "已完成" in html)

# ---------- 6. 照片 ----------
print("\n--- 6. 車損照片 ---")
st, photos_html = get(counter, f"{base}/photos")
check("照片頁 200", st == 200)
check("照片頁有上傳表單", "multipart/form-data" in photos_html)

# ---------- 7. 技師工時 ----------
print("\n--- 7. 技師工時 ---")
st, _ = post(tech, f"{base}/worklogs", {
    "technician_id": "3", "work_date": time.strftime("%Y-%m-%d"),
    "hours": "2", "hourly_rate": "500", "commission_rate": "10",
})
check("技師記工時 302", st == 302, f"got={st}")
st, html = get(counter, f"{base}/worklogs")
check("工時頁 200", st == 200)
check("工資計算 2x500=1000", "1000" in html)
check("佣金計算 10%=100", "100" in html)

# ---------- 8. 完工 ----------
print("\n--- 8. 完工待結帳 ---")
st, _ = post(counter, f"{base}/status", {"status": "completed"})
check("狀態 → 待結帳", st == 302, f"got={st}")
st, html = get(counter, base)
check("工單顯示待結帳", "待結帳" in html)

# ---------- 9. 收據 ----------
print("\n--- 9. 收據列印 ---")
st, html = get(counter, f"{base}/receipt")
check("收據頁 200", st == 200)
check("收據含店名", "AutoRepair OS" in html)
check("收據含車牌", plate in html)
check("收據標示含稅總額", "含稅" in html)
check("收據有列印按鈕", "window.print()" in html)
check("收據有 print CSS", "@media print" in html)

# ---------- 10. 結帳 ----------
print("\n--- 10. 結帳 ---")
st, _ = post(counter, f"{base}/status", {"status": "paid"})
check("結帳 302", st == 302, f"got={st}")
st, html = get(counter, base)
check("工單顯示已結帳", "已結帳" in html)
check("已結帳後為唯讀", "內容唯讀" in html)
check("已結帳後無加入項目表單", 'name="item_type"' not in html)

print("\n--- 11. 已結帳保護 ---")
st, _ = post(counter, f"{base}/status", {"status": "in_progress"})
check("已結帳不可回退（400）", st == 400, f"got={st}")
st, _ = post(counter, f"{base}/items", {
    "item_type": "labor", "description": "事後加項",
    "quantity": "1", "unit_price": "9999",
})
check("已結帳不可加項目（400）", st == 400, f"got={st}")
st, _ = post(counter, f"{base}/status", {"status": "paid"})
check("重複結帳冪等（302）", st == 302, f"got={st}")

# ---------- 12. 報表與總覽 ----------
print("\n--- 12. 報表納入 ---")
st, html = get(owner, f"/reports?from=2000-01-01&to=2099-12-31")
check("報表頁 200", st == 200)
check("報表含營收總額", "營收總額" in html)
st, html = get(owner, "/")
check("總覽顯示最近工單", plate in html or "最近工單" in html)

print("\n" + "=" * 60)
print(f"PASS={sum(results)}  FAIL={len(results) - sum(results)}")
print("=" * 60)
sys.exit(0 if all(results) else 1)