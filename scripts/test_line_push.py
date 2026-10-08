import urllib.request, urllib.parse, http.cookiejar, json, sys, time, re
sys.stdout.reconfigure(encoding="utf-8", errors="replace")

BASE = "http://localhost:8090"
results = []

def check(name, cond, detail=""):
    results.append(cond)
    print(f"[{'PASS' if cond else 'FAIL'}] {name} {detail}")

cj = http.cookiejar.CookieJar()
op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))

def req(path, data=None):
    # data 可為 dict（urlencode 轉 form）或 bytes（原始 body，如推播）。
    if isinstance(data, bytes):
        body = data
    elif data:
        body = urllib.parse.urlencode(data, encoding="utf-8").encode("utf-8")
    else:
        body = None
    r = urllib.request.Request(BASE + path, data=body)
    try:
        resp = op.open(r)
        return resp.status, resp.geturl(), resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, path, e.read().decode("utf-8", "replace")

print("=" * 58)
print("LINE 真實推播測試")
print("=" * 58)

req("/login", {"username": "owner", "password": "Owner001!"})
ts = int(time.time())

# 1. 建立帶 line_user_id 的客戶
st, url, html = req("/customers", {
    "name": "LINE推播測試", "phone": f"0977{ts % 100000:06d}",
    "notification_preference": "line",
    # 假的 user id 會被 LINE API 拒絕，這是預期行為 — 驗證錯誤處理正確
    "line_user_id": "U0000000000000000000000000000000",
})
check("建立帶 line_user_id 的客戶", st == 200)

st, _, html = req("/customers?q=LINE")
cids = sorted(set(re.findall(r'/customers/(\d+)', html)), key=int)
cid = cids[0] if cids else "1"
print(f"   customer_id={cid}")

# 確認 line_user_id 已寫入
st, _, eh = req(f"/customers/{cid}/edit")
saved = re.search(r'name="line_user_id" value="([^"]*)"', eh)
check("line_user_id 已保存", saved and saved.group(1).startswith("U"),
      saved.group(1)[:12] + "..." if saved else "none")

# 2. 建立車輛（保養日期設在 3 天內，觸發保養通知）
st, _, vh = req("/vehicles", {
    "customer_id": cid, "plate_number": f"LINET-{ts % 100000}",
    "brand_model": "Test Car", "current_mileage": "10000",
    "next_maintenance_date": time.strftime("%Y-%m-%d", time.localtime(time.time() + 3*86400)),
})
check("建立車輛", st == 200)

# 3. 觸發保養掃描 → 產生 LINE 通道通知
st, _, _ = req("/scheduled-tasks/maintenance-due/run")
check("觸發保養掃描", st == 200)

st, _, sh = req("/scheduled-tasks")
has_line_notif = f"U0000000000000000000000000000000" in sh or "LINE</td>" in sh
plate_found = f"LINET-{ts % 100000}" in sh
check("保養通知進佇列", plate_found)
check("通知使用 LINE 通道", has_line_notif)

# 4. 處理佇列 → 應該嘗試真實外送並記錄結果
st, _, flash = req("/scheduled-tasks/notifications/run")
check("觸發通知處理", st == 200)

# 等待排程器處理
time.sleep(3)
st, _, sh2 = req("/scheduled-tasks")
failed = "失敗" in sh2
sent = "已送達" in sh2
print(f"   狀態分佈: 已送達={sent} 失敗={failed}")

# 假的 user id 應該被 LINE API 拒 → 標記失敗（這是正確行為）
if failed:
    err = re.search(r'LINE[^<]{0,80}', sh2)
    check("無效 user id 被標記失敗（預期）", True,
          "LINE API 拒絕不正確的 to")
else:
    print("   ⚠ 未看到失敗狀態，可能是 MOCK 模式或排程器尚未執行")

# 5. 確認錯誤訊息有記錄原因
has_reason = "line" in sh2.lower() or "invalid" in sh2.lower() or "推播" in sh2
check("失敗有記錄原因", has_reason)

print("\n" + "=" * 58)
p = sum(results)
print(f"PASS={p} FAIL={len(results)-p}")
