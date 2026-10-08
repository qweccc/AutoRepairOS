# -*- coding: utf-8 -*-
# E2E round-trip via urllib with no-redirect opener + correct cookie format.
# Binary launched by subprocess so it stays alive for the whole run.
import os, subprocess, time, socket, urllib.request, urllib.parse, sqlite3, re

ROOT = r"C:\Users\fr\AutoRepairOS"
EXE = os.path.join(ROOT, "autos.exe")
BASE = "http://localhost:8093"
RESULT = os.path.join(ROOT, "scripts", "e2e_curl_result.txt")
os.makedirs(os.path.dirname(RESULT), exist_ok=True)
if os.path.exists(RESULT): os.remove(RESULT)

def line(*m):
    out = [str(x) for x in m]
    print(" ".join(out), flush=True)
    open(RESULT, "a", encoding="utf-8").write(" ".join(out) + "\n")

# start binary in background; stays alive until we terminate
env = os.environ.copy()
env["PORT"] = "8093"
env["DB_PATH"] = "file:" + os.path.join(ROOT, "data_iso_8093", "autorepair.db") + "?_busy_timeout=5000"
env["PYTHONUTF8"] = "1"
proc = subprocess.Popen([EXE], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, cwd=ROOT)
for _ in range(150):
    try:
        s = socket.create_connection(("127.0.0.1", 8093), 2); s.close(); break
    except Exception:
        time.sleep(500)
else:
    line("PORT_DOWN", proc.poll()); proc.terminate(); exit(1)
line("PORT_UP")

# opener that does NOT follow redirects so we keep the 302 Set-Cookie
class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None
noprop = urllib.request.build_opener(_NoRedirect)

def post(path, data, cookie):
    req = urllib.request.Request(BASE + path, data=urllib.parse.urlencode(data).encode("utf-8"),
        headers={"Content-Type": "application/x-www-form-urlencoded", "Cookie": cookie}, method="POST")
    try:
        resp = noprop.open(req, timeout=10)
        return resp.status, resp.geturl(), resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.geturl(), e.read().decode("utf-8", "replace")

def get(path, cookie):
    req = urllib.request.Request(BASE + path, headers={"Cookie": cookie})
    try:
        resp = noprop.open(req, timeout=10)
        return resp.status, resp.geturl(), resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.geturl(), e.read().decode("utf-8", "replace")

# login (token on Set-Cookie of the 302 response)
req = urllib.request.Request(BASE + "/login",
    data=urllib.parse.urlencode({"username": "owner", "password": "Owner001!"}).encode("utf-8"),
    headers={"Content-Type": "application/x-www-form-urlencoded"})
try:
    resp = noprop.open(req, timeout=10)
except urllib.error.HTTPError as e:
    resp = e
    status = e.code
setc = resp.headers.get("Set-Cookie", "")
m = re.search(r"session_token=([^;]+)", setc)
tok = m.group(1) if m else None
line("login_status", status, "has_cookie", bool(setc), "has_token", bool(tok))
if not tok:
    line("NO_TOKEN"); proc.terminate(); exit(1)
cookie = "session_token=" + tok

# ensure active insurance company
con = sqlite3.connect(os.path.join(ROOT, "data_iso_8093", "autorepair.db"))
rows = con.execute("SELECT id, name, active FROM insurance_companies").fetchall()
if not any(r[2] == 1 for r in rows):
    con.execute("INSERT INTO insurance_companies (code, name, active) VALUES ('E2E-INS','E2E Insurance Co',1)")
    con.commit()
con.close()
line("ic_active", sum(1 for r in rows if r[2] == 1))

# LIST
s, u, b = get("/work-orders", cookie)
line("list_status", s, "has_empty", "沒有工單" in b)

# CREATE insurance work order
s, u, b = post("/work-orders", {
    "vehicle_id": "1", "current_mileage": "5000",
    "is_insurance": "1",
    "insurance_company_id_mirror": "1",
    "claim_no": "CLM-999", "accident_date": "2026-10-01", "deductible_amount": "1500"}, cookie)
mm = re.search(r'/work-orders/(\d+)', u)
wo_id = int(mm.group(1)) if mm else 0
line("create_status", s, "redirect_url", u, "wo_id", wo_id)
if wo_id == 0:
    line("CREATE_FAILED"); proc.terminate(); exit(1)

# SHOW detail -> round-trip insurance display
s, u, b = get("/work-orders/" + str(wo_id), cookie)
line("show_status", s, "has_insurance_pill", "保險理賠" in b, "has_selfpay_pill", "自費" in b)
line("rt_company", "E2E Insurance Co" in b, "rt_claim", "CLM-999" in b, "rt_accident", "2026-10-01" in b, "rt_deductible", "1500" in b)

# RECIPT -> insurance fields
s, u, b = get("/work-orders/" + str(wo_id) + "/receipt", cookie)
line("receipt_status", s, "receipt_has_company", "E2E Insurance Co" in b or "CLM-999" in b)

# SELF-PAY work order
s, u, b = post("/work-orders", {
    "vehicle_id": "1", "current_mileage": "5100",
    "is_insurance": "0", "claim_no": "", "accident_date": "", "deductible_amount": "0"}, cookie)
mm = re.search(r'/work-orders/(\d+)', u)
wo2 = int(mm.group(1)) if mm else 0
s, u, b = get("/work-orders/" + str(wo2), cookie)
line("selfpay_status", s, "selfpay_pill", ("自費" in b and "保險理賠" not in b))

line("ALL_DONE")
proc.terminate()
try: proc.wait(timeout=3)
except Exception: proc.kill()
