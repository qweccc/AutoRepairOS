import urllib.request, urllib.parse, re

BASE="http://localhost:8090"

def login(un,pw):
    data=urllib.parse.urlencode({"username":un,"password":pw}).encode("utf-8")
    req=urllib.request.Request(BASE+"/login",method="POST",data=data)
    req.add_header("Content-Type","application/x-www-form-urlencoded; charset=utf-8")
    try:
        resp=urllib.request.urlopen(req,timeout=8)
        return resp.status,resp.read().decode("utf-8","replace"),[c.value for c in resp.headers.get_all("Set-Cookie")]
    except urllib.error.HTTPError as e:
        return e.code,e.read().decode("utf-8","replace"),[]

# owner 登入
st,b,cookies=login("owner","Owner001!")
print("owner login:",st)
for c in cookies: print("  set-cookie:",c)
print("  body:",b.strip()[:200])
