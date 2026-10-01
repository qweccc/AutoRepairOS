import urllib.request, urllib.parse, http.cookiejar, re, time

BASE = "http://localhost:8090"
PLATE = f"MNT-{int(time.time()) % 100000}"

def sess():
    cj = http.cookiejar.CookieJar()
    return urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))

def req(op, path, data=None):
    body = urllib.parse.urlencode(data, encoding="utf-8").encode("utf-8") if data else None
    r = urllib.request.Request(BASE + path, data=body)
    try:
        resp = op.open(r)
        return resp.status, resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")

results = []
def check(name, cond, detail=""):
    results.append(cond)
    print(f"[{'PASS' if cond else 'FAIL'}] {name} {detail}")

op = sess()
st, _ = req(op, "/login", {"username": "owner", "password": "Owner001!"})
check("owner 登入", st == 200)

# --- 1. 排程任務頁 ---
st, html = req(op, "/scheduled-tasks")
check("排程頁載入", st == 200)
check("含通知佇列處理任務", "process_notifications" in html)
check("含每日備份任務", "daily_backup" in html)
check("含保養掃描任務", "maintenance_due_scan" in html)
check("任務顯示頻率", ("1 分鐘" in html or "分鐘" in html))
check("備份紀錄有資料", "autorepair_" in html, "自動備份已執行")

# --- 2. 手動觸發備份 ---
st, html = req(op, "/scheduled-tasks/backups/run")
check("手動備份成功", st == 200)

# --- 3. 建立一台即將到期的車，測試保養掃描 ---
soon = time.strftime("%Y-%m-%d", time.localtime(time.time() + 3*86400))
st, body = req(op, "/vehicles", {
    "customer_id": "1", "plate_number": PLATE, "brand_model": "Test Car",
    "manufacture_year": "2021", "current_mileage": "10000",
    "next_maintenance_date": soon,
})
check("建立到期車輛", st == 200, body[:70] if st != 200 else "")

st, html = req(op, "/scheduled-tasks/maintenance-due/run")
check("保養掃描執行", st == 200)

st, html = req(op, "/scheduled-tasks")
check("保養提醒進佇列", "保養到期" in html or "定期保養" in html)
check("通知內容含車牌", PLATE in html)
m = re.search(r'/notifications/(\d+)/cancel', html)
check("通知有取消按鈕", m is not None, m.group(1) if m else "none")

# --- 4. 取消通知 ---
if m:
    nid = m.group(1)
    st, _ = req(op, f"/notifications/{nid}/cancel")
    check("取消通知", st == 200)
    st, html2 = req(op, "/scheduled-tasks")
    check("取消後從待處理移除", f'/notifications/{nid}/cancel' not in html2)

# --- 5. 處理通知佇列 ---
st, html = req(op, "/scheduled-tasks/notifications/run")
check("處理通知佇列", st == 200)

# --- 6. 任務啟停切換 ---
st, html = req(op, "/scheduled-tasks")
m = re.search(r'/scheduled-tasks/(\w+)/toggle', html)
check("有切換按鈕", m is not None)
if m:
    key = m.group(1)
    st, _ = req(op, f"/scheduled-tasks/{key}/toggle")
    check("切換任務狀態", st == 200)
    st, html2 = req(op, "/scheduled-tasks")
    # 切回來
    req(op, f"/scheduled-tasks/{key}/toggle")

# --- 7. 報表 ---
st, html = req(op, "/reports")
check("報表頁載入", st == 200)
check("報表含營收總額", "營收總額" in html)
check("報表含工單狀態分布", "工單狀態分布" in html)
check("報表含熱門項目", "熱門項目" in html)
check("報表含技師績效", "技師績效" in html)
check("報表含庫存低水位", "庫存低水位警示" in html)

# 報表日期篩選
st, html = req(op, "/reports?from=2020-01-01&to=2020-12-31")
check("報表日期篩選", st == 200 and "2020-01-01" in html)

print("\n=== SUMMARY ===")
print(f"PASS={sum(results)} FAIL={len(results)-sum(results)}")
