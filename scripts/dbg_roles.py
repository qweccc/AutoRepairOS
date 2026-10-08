import urllib.request, urllib.parse, http.cookiejar, re

BASE="http://localhost:8090"

# 測試各角色登入
for role,un,pw in [("owner","owner","Owner001!"),("counter","counter","Counter001!"),("tech","tech","Tech001!")]:
    cj=http.cookiejar.CookieJar()
    op=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))
    b=urllib.parse.urlencode({"username":un,"password":pw}).encode("utf-8")
    r=urllib.request.Request(BASE+"/login",method="POST",data=b)
    r.add_header("Content-Type","application/x-www-form-urlencoded; charset=utf-8")
    try:
        resp=op.open(r,timeout=8)
        st=resp.status
        tok=[c.value for c in cj if c.name=="session_token"]
        resp.read(); resp.close()
        print(f"{role}: {st} | tokens={len(tok)}")
    except urllib.error.HTTPError as e:
        print(f"{role}: {e.code}")
