import urllib.request, urllib.parse, http.cookiejar, re, sys, time
sys.stdout.reconfigure(encoding="utf-8", errors="replace")

BASE = "http://localhost:8092"
results = []

def check(name, cond, detail=""):
    results.append(cond)
    print(f"[{'PASS' if cond else 'FAIL'}] {name} {detail}")

def sess(user, pwd):
    cj = http.cookiejar.CookieJar()
    op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))
    body = urllib.parse.urlencode({"username": user, "password": pwd}).encode()
    op.open(urllib.request.Request(BASE + "/login", data=body))
    return op

def req(op, path, data=None):
    body = urllib.parse.urlencode(data, encoding="utf-8").encode("utf-8") if data else None
    full = BASE + urllib.parse.quote(path, safe="/?=&%")
    try:
        resp = op.open(urllib.request.Request(full, data=body))
        return resp.status, resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")

print("=" * 60)
print("Task 4: 工單保險欄位 — 真實 HTTP 驗證")
print("=" * 60)

owner = sess("owner", "Owner001!")
ts = int(time.time())

# 1. 建保險公司
st, _ = req(owner, "/insurance-companies", {
    "code": f"INS{ts}", "name": "國泰世紀產物保險",
    "tax_id": "28817468", "claim_phone": "0800-024-888",
})
check("建立保險公司", st == 200, f"HTTP {st}")

st, html = req(owner, "/insurance-companies")
ins_id = re.findall(r'/insurance-companies/(\d+)/edit', html)[0]
st, html = req(owner, f"/insurance-companies/{ins_id}/edit")
check("取得保險公司 id", ins_id.isdigit(), f"id={ins_id}")

# 2. 基礎資料
st, _ = req(owner, "/customers", {"name": f"Task4客戶{ts}", "phone": f"0900{ts%100000:06d}"})
st, html = req(owner, f"/customers?q=Task4客戶{ts}")
cid = re.findall(r'/customers/(\d+)/edit', html)[0]

st, _ = req(owner, "/vehicles", {
    "customer_id": cid, "plate_number": f"T4-{ts}",
    "brand_model": "Toyota", "current_mileage": "50000",
})
st, html = req(owner, f"/vehicles?customer={cid}")
vid = re.findall(r'/vehicles/(\d+)/edit', html)[0]
check("建立車輛", vid.isdigit(), f"vehicle_id={vid}")

# 3. 開立工單表單有保險欄位
st, html = req(owner, "/work-orders/new")
check("開工單頁載入", st == 200)
check("有件別欄位", 'name="is_insurance"' in html)
check("有保險公司欄位", 'name="insurance_company_id"' in html)
check("有理賠編號欄位", 'name="claim_no"' in html)
check("有事故日期欄位", 'name="accident_date"' in html)
check("有自負額欄位", 'name="deductible_amount"' in html)
check("保險下拉含新建公司", f"INS{ts}" in html)

# 4. 自費工單（不填保險）
st, html = req(owner, "/work-orders", {
    "vehicle_id": vid, "current_mileage": "50000",
    "customer_complaint": "自費案件", "is_insurance": "0",
})
check("建立自費工單", "工單已建立" in html or st == 200)
st, html = req(owner, "/work-orders")
wo_self = re.findall(r'/work-orders/(\d+)', html)[0]

# 5. 保險件工單
st, html = req(owner, "/work-orders", {
    "vehicle_id": vid, "current_mileage": "50500",
    "customer_complaint": "保險案件車擦撞",
    "is_insurance": "1",
    "insurance_company_id_mirror": ins_id,
    "claim_no": f"CLM-{ts}",
    "accident_date": "2026-10-01",
    "deductible_amount": "5000",
})
check("建立保險件工單", "工單已建立" in html or st == 200, f"HTTP {st}")

# 6. 驗證保險工單詳情
st, html = req(owner, "/work-orders")
wo_ids = re.findall(r'/work-orders/(\d+)', html)
wo_ins = wo_ids[0]

st, html = req(owner, f"/work-orders/{wo_ins}")
check("保險工單頁載入", st == 200)
check("顯示保險理賠", "保險理賠" in html)
check("顯示保險公司名", "國泰世紀產物保險" in html)
check("顯示理賠編號", f"CLM-{ts}" in html)
check("顯示事故日期", "2026-10-01" in html)
check("顯示自負額", "5,000" in html or "5000" in html)

# 7. 自費工單顯示自費
st, html = req(owner, f"/work-orders/{wo_self}")
check("自費工單顯示自費", "自費" in html)
check("自費工單不含保險公司", "國泰世紀產物保險" not in html)

# 8. 勾保險件但沒選公司 → 擋
st, html = req(owner, "/work-orders", {
    "vehicle_id": vid, "current_mileage": "51000",
    "is_insurance": "1", "insurance_company_id_mirror": "",
})
check("保險件缺公司被擋", "請選擇保險公司" in html or st != 200, f"HTTP {st}")

# 9. 停用保護：有未結帳工單引用時不可停用
st_del, html_del = req(owner, f"/insurance-companies/{ins_id}/delete")
check("有未結帳工單時拒絕停用", "未結帳" in html_del or st_del != 200, f"HTTP {st_del}")

# 10. 欄位對齊驗證：所有工單頁都要正常載入（欄位錯位會導致 NotFound）
allok = True
bad = []
for wid in set(wo_ids[:5]):
    s, h = req(owner, f"/work-orders/{wid}")
    if s != 200 or "Internal" in h or "工單 #" not in h:
        allok = False
        bad.append(f"#{wid}:{s}")
check("欄位對齊：多張工單頁正常", allok, ",".join(bad) if bad else f"抽查{len(set(wo_ids[:5]))}張")

# 11. 列表頁也要正常（同一組 SELECT）
st, html = req(owner, "/work-orders")
check("工單列表頁正常", st == 200 and "工單列表" in html)

# 12. 收據頁正常（也用 workOrderCols）
st, html = req(owner, f"/work-orders/{wo_ins}/receipt")
check("收據頁正常", st == 200 and "應付總額" in html, f"HTTP {st}")

print("\n" + "=" * 60)
p = sum(results)
print(f"PASS={p} FAIL={len(results)-p}")