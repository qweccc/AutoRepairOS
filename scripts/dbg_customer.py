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
        return e.code,e.read().decode("utf-8","replace"),e.url

def get(path):
    r=urllib.request.Request(BASE+path); resp=op.open(r,timeout=8); return resp.status,resp.read().decode("utf-8","replace"),resp.geturl()

st,b,u=pf("/login",{"username":"owner","password":"Owner001!"})
print("login", st)

# 建客戶，看回傳
st,b,u=pf("/customers",{"name":"王五","phone":"0900000000"})
print("customer create status:", st)
print("  body[:400]:", b[:400])
# 找 id 在 redirect URL
m=re.search(r'/customers/(\d+)', u)
print("  redirect id:", m.group(1) if m else None)

# 直接用抓到的 id 建車
cust_id=m.group(1) if m else None
print("cust_id:", cust_id)
if cust_id:
    st,b,u=pf("/vehicles",{"customer_id":cust_id,"plate_number":"WX-7777","brand_model":"Civic"})
    print("vehicle create status:", st)
    print("  body[:400]:", b[:400])
