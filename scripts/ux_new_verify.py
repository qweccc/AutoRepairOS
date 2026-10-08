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

# 找一個有工單的車輛（里程預填需要）
st, vh = req("/vehicles")
vids = re.findall(r'/vehicles/(\d+)', vh)
print("車輛:", vids[:5])

for q in ["", "?vehicle=21", "?plate=MNT-97881", "?q=MNT", "?q=NOSUCH"]:
    st, html = req("/work-orders/new" + q)
    sel = re.findall(r'<option value="(\d+)" selected>([^<]*)', html)
    mile = re.search(r'name="current_mileage"[^>]*value="([^"]*)"', html)
    cust = re.search(r'p-completed">([^<]*)</span>', html)
    hint = "自動帶入車主" in html
    err = re.search(r'msg-err">([^<]*)', html)
    n_opt = len(re.findall(r'<option value="\d+"', html))

    print(f"\n--- /work-orders/new{q}")
    print(f"  HTTP {st} | 車輛選項 {n_opt} 筆")
    print(f"  預選車輛: {sel[0][1] if sel else '（無）'}")
    print(f"  預填里程: {mile.group(1) if mile else '?'}")
    print(f"  車主提示: {cust.group(1) if cust else ('待建立後帶入' if hint else '?')}")
    if err:
        print(f"  錯誤提示: {err.group(1)[:50]}")
