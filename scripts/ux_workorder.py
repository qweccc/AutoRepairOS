import urllib.request, urllib.parse, http.cookiejar, re, sys
sys.stdout.reconfigure(encoding="utf-8", errors="replace")

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

req("/login", {"username": "counter", "password": "Counter001!"})

st, html = req("/work-orders")
print("=" * 60)
print("工單頁面 UX 分析")
print("=" * 60)

# 1. 建立工單表單的欄位與選單
print("\n【建立工單表單】")
form = re.search(r'<form[^>]*action="/work-orders"[^>]*>(.*?)</form>', html, re.S)
if form:
    f = form.group(1)
    labels = re.findall(r'<label>([^<]+)<(input|select)[^>]*name="([^"]+)"', f)
    for lab, tag, name in labels:
        print(f"  - {lab.strip():12s} [{tag}] name={name}")
    # select 的 options
    for sel in re.finditer(r'<select name="([^"]+)">(.*?)</select>', f, re.S):
        opts = re.findall(r'<option[^>]*value="([^"]*)"[^>]*>([^<]*)<', sel.group(2))
        print(f"    select {sel.group(1)}: {len(opts)} 個選項 → {[o[1] for o in opts][:6]}")
else:
    print("  找不到表單")

# 2. 頁面上有哪些進入點
print("\n【建立工單的入口】")
btns = re.findall(r'<button[^>]*>([^<]+)</button>', html)
links = re.findall(r'<a[^>]*href="([^"]*)"[^>]*>([^<]+)</a>', html)
print("  按鈕:", [b.strip() for b in btns])
print("  連結:")
for href, txt in links[:12]:
    print(f"    {txt.strip():12s} → {href}")

# 3. 車輛選擇方式
print("\n【車輛選擇的下拉內容】")
veh_sel = re.search(r'<select name="vehicle_id"[^>]*>(.*?)</select>', html, re.S)
if veh_sel:
    opts = re.findall(r'<option[^>]*value="([^"]*)"[^>]*>([^<]*)<', veh_sel.group(1))
    print(f"  總計 {len(opts)} 個選項")
    for v, t in opts[:5]:
        print(f"    value={v or '(空)'} label={t.strip()}")
    if len(opts) > 5:
        print(f"    ... 還有 {len(opts)-5} 個")
else:
    print("  找不到 vehicle_id 下拉")
    # 可能是文字輸入
    vi = re.search(r'name="vehicle_id"[^>]*', html)
    print("  vehicle_id 欄位定義:", vi.group(0)[:120] if vi else "無")
