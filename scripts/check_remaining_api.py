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

# 1. login
st,b,u=pf("/login",{"username":"owner","password":"***"})
print("login:",st)

# 2. get current users
st,b,u=get("/api/me")
print("\n=== /api/me ===")
print(b[:500])

# 3. work orders list (schema)
st,b,u=get("/api/workorders")
print("\n=== /api/workorders count ===")
j=re.findall(r'"id"\s*:\s*\d+', b)
print("work_orders count:", len(j))

# 4. customers list
st,b,u=get("/api/customers")
print("\n=== /api/customers count ===")
print(len(j))

# 5. inventory list
st,b,u=get("/api/inventory")
print("\n=== /api/inventory count ===")
print(len(j))

# 6. vehicles list
st,b,u=get("/api/vehicles")
print("\n=== /api/vehicles count ===")
print(len(j))

# 7. technician worklogs
st,b,u=get("/api/technicians")
print("\n=== /api/technicians count ===")
print(len(j))

# 8. reports
st,b,u=get("/api/reports")
print("\n=== /api/reports ===")
print(b[:300])

# 9. scheduled tasks
st,b,u=get("/scheduled-tasks")
print("\n=== /scheduled-tasks ===")
print("bytes:", len(b))

# 10. dashboard
st,b,u=get("/")
print("\n=== / bytes ===")
print(len(b))
