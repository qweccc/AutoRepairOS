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

print("=" * 60)
print("建立工單流程 — 逐步走一遍")
print("=" * 60)

# 步驟 1：櫃檯接到車，現況只能從下拉選車輛
print("\n步驟 1：櫃檯要開立新工單")
st, html = req("/work-orders")
print("  現在只能：從 6 筆車輛下拉中選一個")
print("  問題：車行客戶常直接走進來報車牌，櫃檯知道的是「車牌」或「車主名字」")
print("       但下拉是按建立順序排，沒有搜尋框")

# 檢查有沒有關聯查詢
print("\n步驟 2：想先查客戶/車輛再建立工單")
has_search = "vehicle_id" in html and ("搜尋車牌" in html or "search" in html.lower() or "q=" in html)
print(f"  工單頁有車輛搜尋？ {'有' if has_search else '沒有'}")

# 車輛頁有搜尋嗎
st, vh = req("/vehicles")
v_search = re.search(r'name="q"[^>]*placeholder="([^"]*)"', vh)
print(f"  車輛頁有搜尋框？ {'有：' + v_search.group(1) if v_search else '沒有'}")
c_search = re.search(r'name="q"[^>]*placeholder="([^"]*)"', req("/customers")[1])
print(f"  客戶頁有搜尋框？ {'有：' + c_search.group(1) if c_search else '沒有'}")

# 從車輛頁能不能直接開工單？
print("\n步驟 3：從車輛頁或客戶頁能不能直接開工單？")
v_links = re.findall(r'<a[^>]*href="([^"]*)"[^>]*>([^<]*)</a>', vh)
wo_from_vehicle = [l for l in v_links if "work-order" in l[0] or "工單" in l[1]]
print(f"  車輛頁的工單相關連結：{wo_from_vehicle if wo_from_vehicle else '沒有'}")

c_links = re.findall(r'<a[^>]*href="([^"]*)"[^>]*>([^<]*)</a>', req('/customers')[1])
wo_from_cust = [l for l in c_links if "work-order" in l[0] or "工單" in l[1]]
print(f"  客戶頁的工單相關連結：{wo_from_cust if wo_from_cust else '沒有'}")

# 工單頁下拉能否帶入 customer_id
print("\n步驟 4：工單表單能否指定客戶？")
f = re.search(r'<form[^>]*action="/work-orders"[^>]*>(.*?)</form>', html, re.S)
has_cust = "customer_id" in f.group(1) if f else False
print(f"  表單有 customer_id 欄位？ {'有' if has_cust else '沒有（只靠 vehicle_id 推導）'}")

# 車輛數量
st, vh2 = req("/vehicles")
cnt = re.search(r'車輛列表（(\d+) 筆', vh2)
print(f"\n目前車輛總數：{cnt.group(1) if cnt else '?'}")
print("  → 全部塞進下拉選單。車行有 300 台車時就是 300 個選項，無法使用。")
