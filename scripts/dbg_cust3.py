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
phone="09"+str(random.randint(10000000,99999999))
st,b,u=pf("/customers",{"name":"ZZ王五ZZ","phone":phone})
print("url:", u)
# Find all occurrences of the name and their surrounding context
print("=== name occurrences ===")
for m in re.finditer(r'ZZ王五ZZ', b):
    s=max(0,m.start()-60); e=min(len(b),m.start()+60)
    print("AT",m.start(),"|",b[s:e].replace("\n"," "))
