import urllib.request, urllib.parse, http.cookiejar, re

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
        return e.code, e.read().decode("utf-8","replace"), e.url

def get(path):
    r=urllib.request.Request(BASE+path); resp=op.open(r,timeout=8); return resp.status,resp.read().decode("utf-8","replace"),resp.geturl()

def first_id(html, prefix):
    m=re.search(prefix+r"(\d+)/edit", html) or re.search(prefix+r"(\d+)</a>", html) or re.search(prefix+r"(\d+)", html)
    return m.group(1) if m else None

st,b,u=pf("/login",{"username":"owner","password":"Owner001!"})
print("login", st)

import random as _r
st,b,u=pf("/customers",{"name":"李四","phone":"0922"+str(_r.randint(10000000,99999999))})
print("customer create status:", st)
print("  body[:300]:", b[:300])
cust_id=first_id("customers", "/customers/")
print("  cust_id:", cust_id)

st,b,u=pf("/vehicles",{"customer_id":cust_id,"plate_number":"ZZ-9","brand_model":"Test"})
print("vehicle create status:", st)
veh_id=first_id("vehicles", "/vehicles/")
print("  veh_id:", veh_id)

st,b,u=pf("/inventory",{"part_number":"P-999","name":"測試零件","bin_location":"B-1","selling_price":"500","stock_quantity":"5","min_safety_stock":"1"})
print("part create status:", st)
part_id=first_id("inventory", "/inventory/")
print("  part_id:", part_id)

st,b,u=pf("/work-orders",{"vehicle_id":veh_id,"current_mileage":"100","customer_complaint":"x","approval_status":"not_required"})
print("wo create status:", st)
wid=first_id("work-orders", "/work-orders/")
print("  wo_id:", wid)

if wid:
    pf(f"/work-orders/{wid}/items",{"item_type":"labor","description":"診斷","quantity":"1","unit_price":"1000","primary_technician_id":"1"})
    pf(f"/work-orders/{wid}/items",{"item_type":"part","inventory_item_id":part_id,"description":"零件","quantity":"1","unit_price":"500"})

for name in ["worklogs","receipt","photos"]:
    st,b,u=get(f"/work-orders/{wid}/{name}")
    print(f"\n===== /work-orders/{wid}/{name}  HTTP {st}  {len(b)} bytes =====")
    h1=re.search(r'<h1[^>]*>(.*?)</h1>', b, re.S)
    h2=re.search(r'<h2[^>]*>(.*?)</h2>', b, re.S)
    print("  <h1>:", (h1.group(1).strip() if h1 else "?")[:100])
    print("  <h2>:", (h2.group(1).strip() if h2 else "?")[:150])
    main=b.split('<main>')[1].split('</main>')[0] if '<main>' in b else b
    empties=[m for m in ["尚未","尚無","無","empty","none","No data","placeholder","（無"] if m in main]
    print("  empty markers:", empties)
    print("  main[:250]:", main[:250].replace("\n"," ").replace("\t"," "))
