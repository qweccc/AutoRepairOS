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
for wid in [18]:
    st,b,u=get("/work-orders/%d"%wid)
    print("workorder", wid, st, "bytes", len(b))
    for sub in ["worklogs","receipt","photos"]:
        print("\n################ /work-orders/%d/%s ################" % (wid, sub))
        st,b,u=get("/work-orders/%d/%s" % (wid, sub))
        print("HTTP", st, "bytes", len(b))
        h1=re.search(r'<h1[^>]*>(.*?)</h1>', b, re.S)
        h2=re.search(r'<h2[^>]*>(.*?)</h2>', b, re.S)
        print("h1:", (h1.group(1).strip() if h1 else "?")[:100])
        print("h2:", (h2.group(1).strip() if h2 else "?")[:120])
        tables=re.findall(r'<table.*?</table>', b, re.S)
        print("tables:", len(tables))
        for t in tables:
            rows=re.findall(r'<tr[^>]*>.*?</tr>', t, re.S)
            print("   table rows:", len(rows))
        for key in ["技師工時","工時","工資","佣金","機油","檢查與診斷","WX-7777","Civic","20000","ZZ王五ZZ","列印","收據","照片","2026-10-03","煞車異音","王五"]:
            if key in b:
                idx=b.find(key)
                print(f"  [命中]{key}: ...{b[max(0,idx-30):idx+30].strip()[:70]}...")
        main=b.split('<main>')[-1].split('</main>')[0] if '<main>' in b else b
        print("  --- main section ---")
        print(main[:600].replace("\n"," ").replace("\t"," "))
