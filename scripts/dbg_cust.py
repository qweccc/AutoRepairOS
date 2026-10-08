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

st,b,u=pf("/login",{"username":"owner","password":"Owner001!"})
print("login", st)
phone="09"+str(random.randint(10000000,99999999))
print("phone", phone)
st,b,u=pf("/customers",{"name":"王五","phone":phone})
print("customer status", st, "url", u)
# show the customer-related section
for m in re.finditer(r'<tr>.*?</tr>', b, re.S):
    t=m.group(0)
    if "王五" in t or "王五" in b[:m.start()+40]:
        print("ROW:", t[:200])
