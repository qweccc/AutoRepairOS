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

st,b,u=pf("/login",{"username":"owner","password":"Owner001!"})
print("login:",st)

UID="e35b866a"

# 建零件 - 看完整 flash 訊息與所有 href
st,b,u=pf("/inventory",{"part_number":"OIL-"+UID,"name":"機油測試油","bin_location":"A-09-9","selling_price":"800","stock_quantity":"10","min_safety_stock":"2"})
print("\n=== 建零件 === status:",st)
m=re.search(r'<div class="flash[^"]*">(.*?)</div>', b, re.S)
print("  flash:", m.group(1).strip()[:200] if m else "?")
hrefs=re.findall(r'href="/inventory/(\d+)', b)
print("  /inventory/ID hrefs:", hrefs)
# 表格行
rows=re.findall(r'<tr><td[^>]*>(.*?)</td>', b, re.S)
print("  first row cells:", [x.strip()[:20] for x in rows[:6]])

# 直接 GET 庫存頁看表格
st2,b2,u2=pf("/login",{"username":"owner","password":"Owner001!"})
print("\n=== GET /inventory ===")
st,b,u=get("/inventory")
hrefs2=re.findall(r'href="/inventory/(\d+)', b)
print("  hrefs:", hrefs2)
for row in re.findall(r'<tr><td[^>]*>(.*?)</td>', b, re.S):
    print("  ROW:", row.strip()[:25])
    break
