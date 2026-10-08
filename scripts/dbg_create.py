import os, subprocess, time, socket, urllib.request, urllib.parse, sqlite3, re
ROOT=r"C:\Users\fr\AutoRepairOS"; EXE=os.path.join(ROOT,"autos.exe"); BASE="http://localhost:8093"; RES=os.path.join(ROOT,"scripts","dbg_create.txt"); os.makedirs(os.path.dirname(RES),exist_ok=True)
if os.path.exists(RES): os.remove(RES)
def line(*m): out=[str(x) for x in m]; print(" ".join(out),flush=True); open(RES,"a",encoding="utf-8").write(" ".join(out)+"\n")
env=os.environ.copy(); env["PORT"]="8093"; env["DB_PATH"]="file:"+os.path.join(ROOT,"data_iso_8093","autorepair.db")+"?_busy_timeout=5000"; env["PYTHONUTF8"]="1"
proc=subprocess.Popen([EXE],env=env,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,cwd=ROOT)
for _ in range(100):
    try:
        s=socket.create_connection(("127.0.0.1",8093),2);s.close();break
    except Exception: time.sleep(500)
else: line("PORT_DOWN",proc.poll());proc.terminate();exit(1)
line("PORT_UP")
class _NR(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,req,fp,code,msg,headers,newurl):return None
nop=urllib.request.build_opener(_NR)
req=urllib.request.Request(BASE+"/login",data=urllib.parse.urlencode({"username":"owner","password":"Owner001!"}).encode("utf-8"),headers={"Content-Type":"application/x-www-form-urlencoded"})
try:
    resp=nop.open(req,timeout=10)
except urllib.error.HTTPError as e:
    resp=e
    line("login_status",resp.status)
tok=re.search(r"session_token=([^;]+)",resp.headers.get("Set-Cookie","")).group(1)
cookie="session_token="+tok
line("login",resp.status)
con=sqlite3.connect(os.path.join(ROOT,"data_iso_8093","autorepair.db"))
ic=con.execute("SELECT id,name,active FROM insurance_companies").fetchall()
line("ic",ic)
# try create with full data
s,u,b=nop.open(urllib.request.Request(BASE+"/work-orders",data=urllib.parse.urlencode({"vehicle_id":"1","current_mileage":"5000","is_insurance":"1","insurance_company_id_mirror":"1","claim_no":"CLM-999","accident_date":"2026-10-01","deductible_amount":"1500"}).encode("utf-8"),headers={"Content-Type":"application/x-www-form-urlencoded","Cookie":cookie},method="POST"),timeout=10)
line("create_status",s,"url",u)
line("create_body_head",b[:500])
con.close()
line("DONE")
