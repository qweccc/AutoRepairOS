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

print("=== AUTH ===")
st,b,u=pf("/login",{"username":"owner","password":"Owner001!"})
print("login", st)

# 直接抓已結帳且有項目的工單 ID
for wid in [18,22,25,27]:
    st,b,u=get("/work-orders/%d"%wid)
    print("workorder", wid, st, "bytes", len(b))
    if st==200:
        # 找三個子頁連結
        for sub in ["worklogs","receipt","photos"]:
            m=re.search(r'href="/work-orders/%d/%s"'% (wid, sub), b)
            print("   subpage /%s -> %s" % (sub, m.group(0) if m else "MISS"))
        # 檢查內容關鍵字
        print("   明細:", [k for k in ["檢查與診斷","機油","WX-7777","Civic"] if k in b])
