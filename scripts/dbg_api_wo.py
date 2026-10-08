import urllib.request, urllib.parse, http.cookiejar, re, json

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

st,b,u=pf("/login",{"username":"owner","password":"***"})
print("login:",st)

# GET /api/workorders (no status)
def get(path):
    r=urllib.request.Request(BASE+path)
    try:
        resp=op.open(r,timeout=8); return resp.status,resp.read().decode("utf-8","replace")
    except urllib.error.HTTPError as e:
        return e.code,e.read().decode("utf-8","replace")

st,b,u=get("/api/workorders")
print("\n=== /api/workorders status:",st,"===")
# count all ids
print("  total ids in response:", len(re.findall(r'"id"\s*:\s*\d+', b)))
# show first record
m=re.search(r'\{"id":(\d+),.*?"status":"([^"]*)".*?"total_amount":"([^"]*)".*?"approval_status":"([^"]*)"\}', b)
if m:
    print("  sample:", m.group(1), m.group(2), m.group(3), m.group(4))
else:
    print("  sample regex failed; first 800 chars:")
    print(b[:800])

# check status distribution
print("  status distribution:")
for row in re.finditer(r'"status"\s*:\s*"([^"]*)"', b):
    print("    ", row.group(1))
