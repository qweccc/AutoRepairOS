import urllib.request, urllib.parse, http.cookiejar, re

BASE="http://localhost:8090"
cj=http.cookiejar.CookieJar()
op=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))

def pf(path,data):
    b=urllib.parse.urlencode(data).encode("utf-8")
    r=urllib.request.Request(BASE+path,method="POST",data=b)
    r.add_header("Content-Type","application/x-www-form-urlencoded; charset=utf-8")
    resp=op.open(r,timeout=8); return resp.status,resp.read().decode("utf-8","replace"),resp.geturl()

def get(path):
    r=urllib.request.Request(BASE+path); resp=op.open(r,timeout=8); return resp.status,resp.read().decode("utf-8","replace"),resp.geturl()

st,b,u=pf("/login",{"username":"owner","password":"Owner001!"})
print("login",st)

st,b,u=pf("/vehicles",{"customer_id":"1","plate_number":"ZZ-9","brand_model":"Test"})
print("vehicle create", st)
m=re.search(r'/vehicles/(\d+)/', b)
vid=m.group(1) if m else "NONE"
print("vehicle url:", vid)

st,b,u=pf("/work-orders",{"vehicle_id":vid,"current_mileage":"100","customer_complaint":"x","approval_status":"not_required"})
print("wo create", st)
m=re.search(r'/work-orders/(\d+)/', b)
wid=m.group(1) if m else "NONE"
print("wo url:", wid)

# 直接抓工單詳情頁
mid=int(wid) if wid!="NONE" else None
st,b,u=get("/work-orders/%s"%mid)
print("detail status", st, "len", len(b))
print("title:", re.search(r'<title>(.*?)</title>', b, re.S).group(1).strip())
# 找 detail 頁裡是否有子頁連結
print("=== subpage links on detail ===")
for pat in [r'href="/work-orders/\d+/worklogs"', r'href="/work-orders/\d+/receipt"', r'href="/work-orders/\d+/photos"']:
    m=re.search(pat, b)
    print(" ", pat, "->", m.group(0) if m else "MISS")
