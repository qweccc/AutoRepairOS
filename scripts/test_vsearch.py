import urllib.request, urllib.parse, http.cookiejar, re, json, sys
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

results = []
def check(name, cond, detail=""):
    results.append(cond)
    print(f"[{'PASS' if cond else 'FAIL'}] {name} {detail}")

st, html = req("/work-orders/new")
check("開工單頁載入", st == 200)

# 1. data-options 是合法 JSON
m = re.search(r'data-options="([^"]*)"', html)
check("有 data-options 屬性", m is not None)
if m:
    raw = m.group(1)
    # HTML entity 解碼
    raw = raw.replace("&#39;", "'").replace("&quot;", '"').replace("&amp;", "&")
    try:
        data = json.loads(raw)
        check("data-options 是合法 JSON", isinstance(data, list), f"{len(data)} 筆")
        if data:
            check("每筆有 v 與 label", all("v" in x and "label" in x for x in data))
            check("label 含三段（車牌/車型/車主）", all(x["label"].count(" / ") == 2 for x in data))
            print(f"    範例: {data[0]}")
    except Exception as e:
        check("data-options 是合法 JSON", False, str(e))
        print(f"    原始: {raw[:200]}")

# 2. 隱藏欄位與輸入框
check("有隱藏 vehicle_id", 'name="vehicle_id"' in html and 'type="hidden"' in html)
check("有搜尋輸入框", 'class="vs-input"' in html)
check("有清單容器", 'class="vs-list' in html)

# 3. 引入 JS/CSS
check("引入 vehicle-search.js", "/static/vehicle-search.js" in html)
check("引入 vehicle-search.css", "/static/vehicle-search.css" in html)

# 4. 靜態檔案可取得
for asset in ["/static/vehicle-search.js", "/static/vehicle-search.css"]:
    s, body = req(asset)
    check(f"{asset} 可取得", s == 200, f"HTTP {s}, {len(body)} bytes")

# 5. noscript 降級
check("有 noscript 降級選單", "<noscript>" in html)

# 6. 預選車輛
st, html2 = req("/work-orders/new?vehicle=21")
check("預選時有 data-picked", 'data-picked="1"' in html2)
vid = re.search(r'name="vehicle_id" id="vehicle_id" value="(\d+)"', html2)
check("預選帶入 vehicle_id", vid is not None and vid.group(1) == "21", vid.group(1) if vid else "none")
inp = re.search(r'class="vs-input"[^>]*value="([^"]*)"', html2)
check("預選帶入顯示文字", inp is not None and inp.group(1) != "", inp.group(1)[:50] if inp else "none")

# 7. 搜尋模式：?q= 應讓清單變短
def parse_opts(h):
    m = re.search(r'data-options="([^"]*)"', h)
    if not m:
        return None
    raw = m.group(1).replace("&quot;", '"').replace("&#39;", "'")
    return json.loads(raw)

all_o = parse_opts(req("/work-orders/new")[1]) or []
st, html3 = req("/work-orders/new?q=MNT")
q_o = parse_opts(html3) or []
check("?q= 有篩選效果", 0 < len(q_o) < len(all_o),
      f"全部 {len(all_o)} → 篩選 {len(q_o)}")

print(f"\nPASS={sum(results)} FAIL={len(results)-sum(results)}")
