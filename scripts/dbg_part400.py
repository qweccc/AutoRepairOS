import urllib.request, urllib.parse, http.cookiejar

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

# 建零件 - 抓 400 內容
st,b,u=pf("/inventory",{"part_number":"OIL-100","name":"機油測試油","bin_location":"A-09-9","selling_price":"800","stock_quantity":"10","min_safety_stock":"2"})
print("part:",st,"| body:",b.strip()[:300])
