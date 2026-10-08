import os, subprocess, time, socket, urllib.request, urllib.parse, sqlite3, re
ROOT = r"C:\Users\fr\AutoRepairOS"
EXE = os.path.join(ROOT, "autos.exe")
BASE = "http://localhost:8093"
RESULT = os.path.join(ROOT, "scripts", "e2e_curl_result.txt")
os.makedirs(os.path.dirname(RESULT), exist_ok=True)
if os.path.exists(RESULT): os.remove(RESULT)
def line(*m):
    out=[str(x) for x in m]; print(" ".join(out), flush=True)
    open(RESULT,"a",encoding="utf-8").write(" ".join(out)+"\n")
class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl): return None
noprop = urllib.request.build_opener(_NoRedirect)
def post(path, data, cookie):
    req = urllib.request.Request(BASE+path, data=urllib.parse.urlencode(data).encode("utf-8"),
        headers={"Content-Type":"application/x-www-form-urlencoded","Cookie":cookie}, method="POST")
    try:
        resp = noprop.open(req, timeout=10); return resp.status, resp.geturl(), resp.read().decode("utf-8","replace")
    except urllib.error.HTTPError as e:
        return e.code, e.geturl(), e.read().decode("utf-8","replace")
def get(path, cookie):
    req = urllib.request.Request(BASE+path, headers={"Cookie":cookie})
    try:
        resp = noprop.open(req, timeout=10); return resp.status, resp.geturl(), resp.read().decode("utf-8","replace")
    except urllib.error.HTTPError as e:
        return e.code, e.geturl(), e.read().decode("utf-8","replace")
line("PORT_UP")
req = urllib.request.Request(BASE+"/login", data=urllib.parse.urlencode({"username":"owner","password":"Owner001!"}).encode("utf-8"), headers={"Content-Type":"application/x-www-form-urlencoded"})
try:
    resp = noprop.open(req, timeout=10); status=resp.status; setc=resp.headers.get("Set-Cookie","")
except urllib.error.HTTPError as e:
    status=e.code; setc=e.headers.get("Set-Cookie","")
m=re.search(r"session_token=([^;]+)",setc); tok=m.group(1) if m else None
line("login_status",status,"has_token",bool(tok))
if not tok: line("NO_TOKEN"); exit(1)
cookie="session_token="+tok
con=sqlite3.connect(os.path.join(ROOT,"data_iso_8093","autorepair.db"))
rows=con.execute("SELECT id,name,active FROM insurance_companies").fetchall()
active=[r for r in rows if r[2]==1]
if not active:
    con.execute("INSERT INTO insurance_companies (code,name,active) VALUES ('E2E-INS','E2E Insurance Co',1)"); con.commit()
con.close()
line("ic_rows",len(rows),"active",len(active))
# list to see existing wos
s,u,b = get("/work-orders",cookie)
m=re.search(r'/work-orders/(\d+)',u)
ids=[int(x) for x in re.findall(r'/work-orders/(\d+)',u)]
line("list_ids",len(ids),ids[:10])
# create insurance
s,u,b = post("/work-orders",{"vehicle_id":"1","current_mileage":"5000","is_insurance":"1","insurance_company_id_mirror":"1","claim_no":"CLM-999","accident_date":"2026-10-01","deductible_amount":"1500"},cookie)
line("create_status",s,"redirect_url",u,"wo_id",re.search(r'/work-orders/(\d+)',u).group(1) if re.search(r'/work-orders/(\d+)',u) else 0)
# fetch detail of the LAST id on the list (or the created one)
target = s if s>=300 else ids[-1] if ids else 0
s,u,b = get("/work-orders/"+str(target),cookie)
line("show_status",s,"has_insurance_pill","保險理賠" in b,"has_selfpay_pill","自費" in b)
line("rt_company","E2E Insurance Co" in b,"rt_claim","CLM-999" in b,"rt_accident","2026-10-01" in b,"rt_deductible","1500" in b)
s,u,b = get("/work-orders/"+str(target)+"/receipt",cookie)
line("receipt_status",s,"receipt_has_company","E2E Insurance Co" in b or "CLM-999" in b)
line("ALL_DONE")
