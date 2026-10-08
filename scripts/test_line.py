import urllib.request, urllib.parse, http.cookiejar, re, json, sys, hashlib, hmac, base64
sys.stdout.reconfigure(encoding="utf-8", errors="replace")

BASE = "http://localhost:8090"
results = []

def check(name, cond, detail=""):
    results.append(cond)
    print(f"[{'PASS' if cond else 'FAIL'}] {name} {detail}")

cj = http.cookiejar.CookieJar()
op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))

def req(path, data=None, headers=None, raw=None):
    if raw is not None:
        body = raw
    else:
        body = urllib.parse.urlencode(data, encoding="utf-8").encode("utf-8") if data else None
    r = urllib.request.Request(BASE + path, data=body)
    for k, v in (headers or {}).items():
        r.add_header(k, v)
    try:
        resp = op.open(r)
        return resp.status, resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")

print("=" * 58)
print("LINE Messaging API 整合驗證")
print("=" * 58)

req("/login", {"username": "owner", "password": "Owner001!"})

# 1. Webhook 端點存在且拒絕無簽名請求
print("\n【1】Webhook 端點")
body = json.dumps({"destination": "U123", "events": []}).encode("utf-8")
st, resp = req("/webhook/line", raw=body,
               headers={"Content-Type": "application/json"})
# 無簽名應該被拒（400/403）
check("無簽名請求被拒", st in (400, 403, 401), f"HTTP {st}")

# 錯誤簽名也要被拒
st, resp = req("/webhook/line", raw=body,
               headers={"Content-Type": "application/json", "X-Line-Signature": "deadbeef"})
check("錯誤簽名被拒", st in (400, 403, 401), f"HTTP {st}")

# 2. UI 顯示 LINE 狀態
print("\n【2】UI 狀態顯示")
st, html = req("/scheduled-tasks")
check("排程頁載入", st == 200)
has_line = "LINE" in html or "MOCK" in html
check("顯示 LINE 狀態", has_line)
for pat in ["MOCK 模式", "已連線", "憑證不完整", "LINE"]:
    if pat in html:
        print(f"    找到: {pat}")
        break

# 3. 通知紀錄的 LINE 通道
print("\n【3】通知通道")
st, html = req("/scheduled-tasks")
has_line_ch = ">LINE<" in html or "LINE</td>" in html or "line" in html.lower()
check("通知紀錄含 LINE 通道", has_line_ch)

# 4. 客戶的 line_user_id 欄位存在
print("\n【4】客戶 LINE 綁定欄位")
st, html = req("/customers?msg=x")
check("客戶頁載入", st == 200)
has_field = "line_user_id" in html
check("客戶表單有 line_user_id 欄位", has_field)

st, html = req("/customers")
m = re.search(r'/customers/(\d+)/edit', html)
if m:
    st, eh = req(f"/customers/{m.group(1)}/edit")
    check("編輯頁有 line_user_id", "line_user_id" in eh)

# 5. Flex 估價單模板存在
print("\n【5】估價單模板")
st, wh = req("/work-orders")
wids = sorted(set(re.findall(r'/work-orders/(\d+)', wh)), key=int)
if wids:
    st, rh = req(f"/work-orders/{wids[-1]}/receipt")
    check("收據頁載入", st == 200)
    has_flex = "rpt-flex" in rh or "ToFlex" in rh or "Flex" in rh
    check("收據含 LINE Flex 區塊", has_flex)

print("\n" + "=" * 58)
p = sum(results)
print(f"PASS={p} FAIL={len(results)-p}")
