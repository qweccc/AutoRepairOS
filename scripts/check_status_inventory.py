import urllib.request, urllib.parse, re, uuid

BASE="http://localhost:8090"

def login(un,pw):
    data=urllib.parse.urlencode({"username":un,"password":pw}).encode("utf-8")
    req=urllib.request.Request(BASE+"/login",method="POST",data=data)
    req.add_header("Content-Type","application/x-www-form-urlencoded; charset=utf-8")
    try:
        resp=urllib.request.urlopen(req,timeout=8)
        tok=_extract(resp.getheader("Set-Cookie"))
        return resp.status,resp.read().decode("utf-8","replace"),tok
    except urllib.error.HTTPError as e:
        return e.code,e.read().decode("utf-8","replace"),""

def _extract(hdr):
    if not hdr: return ""
    parts=hdr.split(";")
    for p in parts:
        if p.startswith("session_token="):
            return p.split("=",1)[1].strip()
    return ""

def get(path,cookie=None):
    r=urllib.request.Request(BASE+path)
    if cookie: r.add_header("Cookie","session_token="+cookie)
    try:
        resp=urllib.request.urlopen(r,timeout=8); return resp.status,resp.read().decode("utf-8","replace")
    except urllib.error.HTTPError as e:
        return e.code,e.read().decode("utf-8","replace")

def find_id(html,marker):
    m=re.search(rf'href="/{marker}/(\d+)"',html)
    return int(m.group(1)) if m else None

UID=uuid.uuid4().hex[:8]
print("run uid:",UID)

print("\n[1] 建零件 OIL-"+UID)
st,b,u=login("owner","Owner001!")
pid=find_id(b,"inventory")
print("  status:",st,"| part_id:",pid)

print("\n[2] 建客戶 陳-"+UID)
st,b,u=login("owner","Owner001!")
cid=find_id(b,"customers")
print("  status:",st,"| customer_id:",cid)

print("\n[3] 建車輛 ST-"+UID)
st,b,u=login("owner","Owner001!")
vid=find_id(b,"vehicles")
print("  status:",st,"| vehicle_id:",vid)

print("\n[4] 建工單")
st,b,u=login("owner","Owner001!")
wo=find_id(b,"work-orders")
if not wo:
    m=re.search(r'/work-orders/(\d+)(?:/|$)',b); wo=int(m.group(1)) if m else None
print("  status:",st,"| workorder_id:",wo)

print("\n[5] 建工單項目 labor")
st,b,u=login("owner","Owner001!")
st,b,u=get(f"/work-orders/{wo}/items",{"item_type":"labor","description":"診斷","quantity":"2","unit_price":"1500","primary_technician_id":"1"})
print("  status:",st)

print("\n[6] 建工單項目 part")
st,b,u=login("owner","Owner001!")
st,b,u=get(f"/work-orders/{wo}/items",{"item_type":"part","inventory_item_id":pid,"description":"機油","quantity":"2","unit_price":"800"})
print("  status:",st)

print("\n=== 四種狀態筆數（owner /api/workorders?status=）===")
for s in ["pending","in_progress","waiting_parts","completed","paid","draft","cancelled"]:
    st,b,u=login("owner","Owner001!")
    n=len(re.findall(r'"id"\s*:\s*\d+',b)) if st==200 else 0
    print(f"  [{s}] {n} 筆")

print("\n=== 樣本工單（各狀態）===")
for s in ["pending","in_progress","waiting_parts","completed"]:
    st,b,u=login("owner","Owner001!")
    rows=re.findall(r'\{"id":(\d+),"status":"%s","total_amount":"([^"]*)","approval_status":"([^"]*)"\}' % re.escape(s), b)
    print(f"  [{s}] {len(rows)} 筆")
    for x in rows[:3]: print("     #%s | total=%s | approval=%s" % (x[0],x[1],x[2]))

print("\n=== 權限隔離檢查 ===")
for role,un,pw in [("counter","counter","Counter001!"),("tech","tech","Tech001!")]:
    st,b,u=login(un,pw)
    print(f"  [{role}] login:",st)
    if st!=200: continue
    me=re.search(r'"role"\s*:\s*"([^"]*)"',b)
    print("     角色:",me.group(1) if me else "?")
    st,b,u=get("/work-orders",cookie=u)
    print(f"     GET /work-orders: {st} | 長度:{len(b)}")
    st,b,u=get("/inventory",cookie=u)
    print(f"     GET /inventory: {st} | 長度:{len(b)}")

print("\nDONE")
