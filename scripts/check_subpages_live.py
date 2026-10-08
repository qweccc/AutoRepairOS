import urllib.request, urllib.parse, http.cookiejar, re, random

BASE="http://localhost:8090"
cj=http.cookiejar.CookieJar()
op=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))

def pf(path,data):
    b=urllib.parse.urlencode(data).encode("utf-8")
    r=urllib.request.Request(BASE+path,method="POST",data=b)
    r.add_header("Content-Type","application/x-www-form-urlencoded; charset=utf-8")
    try:
        resp=op.open(r,timeout=8); return resp.status,resp.read().decode("utf-8","replace"),resp.geturl()
    except urllib.error.HTTPError as e:
        return e.code,e.read().decode("utf-8","replace"),e.url

def get(path):
    r=urllib.request.Request(BASE+path); resp=op.open(r,timeout=8); return resp.status,resp.read().decode("utf-8","replace"),resp.geturl()

def row_id(html, name):
    for m in re.finditer(r'<tr>.*?</tr>', html, re.S):
        if name in m.group(0):
            t=m.group(0)
            m2=re.search(r'<td>\s*(\d+)', t)
            return m2.group(1) if m2 else None
    return None

def redirect_id(url, prefix):
    m=re.search(prefix+r'(\d+)', url)
    return m.group(1) if m else None

print("=== AUTH ===")
st,b,u=pf("/login",{"username":"owner","password":"Owner001!"})
print("login", st)

phone="09"+str(random.randint(10000000,99999999))
st,b,u=pf("/customers",{"name":"ZZ王五ZZ","phone":phone})
print("customer", st)
cust_id=row_id("customers","ZZ王五ZZ")
print("  cust_id:", cust_id)

st,b,u=pf("/vehicles",{"customer_id":cust_id,"plate_number":"WX-7777","brand_model":"Civic","manufacture_year":"2021","vin":"VINWX"})
print("vehicle", st)
veh_id=redirect_id(u,"/vehicles/")
print("  veh_id:", veh_id)

st,b,u=pf("/work-orders",{"vehicle_id":veh_id,"current_mileage":"20000","customer_complaint":"煞車異音","approval_status":"not_required"})
print("workorder", st)
wid=redirect_id(u,"/work-orders/")
print("  wo_id:", wid)

if wid:
    part_id=redirect_id(u,"/inventory/")
    print("  part_id:", part_id)
    pf(f"/work-orders/{wid}/items",{"item_type":"labor","description":"檢查與診斷","quantity":"2","unit_price":"1500","primary_technician_id":"1"})
    pf(f"/work-orders/{wid}/items",{"item_type":"part","inventory_item_id":part_id,"description":"機油","quantity":"1","unit_price":"800"})
    for s in ["in_progress","waiting_parts","in_progress","completed"]:
        st,b,u=pf(f"/work-orders/{wid}/status",{"status":s}); print("  status->",s,st)
    st,b,u=pf(f"/work-orders/{wid}/status",{"status":"paid"}); print("  status->paid", st)
    st,b,u=pf(f"/work-orders/{wid}/sop",{}); print("  sop toggle", st)
    st,b,u=pf(f"/work-orders/{wid}/worklogs",{"technician_id":"1","work_date":"2026-10-03","hours":"2","hourly_rate":"1500","commission_rate":"10","notes":"檢查"})
    print("  add worklog", st)

print("\n=== 抓三個子頁原始 HTML 關鍵字 ===")
for name in ["worklogs","receipt","photos"]:
    st,b,u=get(f"/work-orders/{wid}/{name}") if wid else (404,"ERR",u)
    print(f"\n===== /work-orders/{wid}/{name}  HTTP {st} {len(b)} bytes =====")
    for key in ["技師工時","檢查與診斷","機油","煞車異音","WX-7777","Civic","20000","ZZ王五ZZ","列印","收據","照片","工資","佣金","2026-10-03"]:
        if key in b:
            idx=b.find(key); print(f"  [命中]{key}: ...{b[max(0,idx-25):idx+25].strip()[:50]}...")
        else:
            print(f"  [缺失]{key}")
