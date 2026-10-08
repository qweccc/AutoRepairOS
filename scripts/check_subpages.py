import urllib.request, urllib.parse, http.cookiejar, re

BASE = "http://localhost:8090"
cj = http.cookiejar.CookieJar()
op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))

def post_form(path, data):
    body = urllib.parse.urlencode(data).encode("utf-8")
    r = urllib.request.Request(BASE + path, method="POST", data=body)
    r.add_header("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
    resp = op.open(r, timeout=8)
    return resp.status, resp.read().decode("utf-8", "replace"), resp.geturl()

def get(path):
    r = urllib.request.Request(BASE + path)
    resp = op.open(r, timeout=8)
    return resp.status, resp.read().decode("utf-8", "replace"), resp.geturl()

print("=== AUTH ===")
st, body, u = post_form("/login", {"username":"owner","password":"Owner001!"})
print("login:", st, "session cookie:", any(c.name=="session_token" for c in cj))
if st != 200:
    print(body[:300])
    raise SystemExit("auth failed")

def build_and_get():
    # 建一組測試資料
    st,b,u = post_form("/customers", {"name":"李四","phone":"0922888888"})
    cust_id = re.search(r'/customers/(\d+)/edit', b)
    cust_id = cust_id.group(1) if cust_id else "1"

    st,b,u = post_form("/vehicles", {"customer_id":cust_id,"plate_number":"AB-1234","brand_model":"Accord"})
    veh_id = re.search(r'/vehicles/(\d+)/edit', b)
    veh_id = veh_id.group(1) if veh_id else "1"

    st,b,u = post_form("/work-orders", {"vehicle_id":veh_id,"current_mileage":"1000","customer_complaint":"故障","approval_status":"not_required"})
    # 從標題 #NN 抓 work order id
    m = re.search(r'<h1[^>]*>工單\s*#\s*(\d+)</h1>', b)
    if not m:
        m = re.search(r'#(\d+)</h1>', b)
    wo_id = int(m.group(1)) if m else None

    # 加 labor 與 part 項目
    if wo_id:
        post_form(f"/work-orders/{wo_id}/items", {"item_type":"labor","description":"診斷","quantity":"1","unit_price":"1000","primary_technician_id":"1"})
        post_form(f"/work-orders/{wo_id}/items", {"item_type":"part","inventory_item_id":"1","description":"零件","quantity":"1","unit_price":"500"})

    # 三個子頁
    pages = {}
    for name in ["worklogs","receipt","photos"]:
        st, b, u = get(f"/work-orders/{wo_id}/{name}")
        pages[name] = (st, b, u)
    return cust_id, veh_id, wo_id, pages

cust_id, veh_id, wo_id, pages = build_and_get()
print("cust_id=%s veh_id=%s wo_id=%s" % (cust_id, veh_id, wo_id))

for name,(st,b,u) in pages.items():
    print("\n===== SUBPAGE: /work-orders/%s/%s  (HTTP %s, %d bytes) =====" % (wo_id, name, st, len(b)))
    # 抓 h1 / h2
    h1 = re.search(r'<h1[^>]*>(.*?)</h1>', b, re.S)
    h2 = re.search(r'<h2[^>]*>(.*?)</h2>', b, re.S)
    print("  <h1>:", (h1.group(1).strip() if h1 else "?")[:100])
    print("  <h2>:", (h2.group(1).strip() if h2 else "?")[:120])
    # 抓表格行數
    rows = re.findall(r'<tr[^>]*>.*?</tr>', b, re.S)
    print("  table rows:", len(rows))
    # 抓卡片內容
    main = b.split('<main>')[1].split('</main>')[0] if '<main>' in b else b
    print("  main len:", len(main))
    # 抓是否有實際表格數據（非 placeholder）
    empty_markers = ["尚未","尚無","無","空","none","No data","placeholder","empty","（無"]
    looks_empty = any(m in main for m in empty_markers)
    print("  疑似空殼 markers:", [m for m in empty_markers if m in main])
    # 截取 main 前 400 字給人工檢查
    print("  --- main head ---")
    print(main[:400].replace("\n"," "))
