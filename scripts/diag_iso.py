# -*- coding: utf-8 -*-
# Diagnostic: launch binary via subprocess, verify liveness, capture any crash.
import os, subprocess, time, socket, urllib.request, urllib.parse, re

ROOT = r"C:\Users\fr\AutoRepairOS"
EXE = os.path.join(ROOT, "autos.exe")
BASE = "http://localhost:8093"
os.makedirs(os.path.join(ROOT, "scripts"), exist_ok=True)

env = os.environ.copy()
env["PORT"] = "8093"
env["DB_PATH"] = "file:" + os.path.join(ROOT, "data_iso_8093", "autorepair.db") + "?_busy_timeout=5000"
env["PYTHONUTF8"] = "1"
outf = open(os.path.join(ROOT, "scripts", "diag_iso_out.txt"), "wb")
errf = open(os.path.join(ROOT, "scripts", "diag_iso_err.txt"), "wb")
proc = subprocess.Popen([EXE], env=env, stdout=outf, stderr=errf, cwd=ROOT)
print("started pid", proc.pid, flush=True)

for _ in range(30):
    try:
        s = socket.create_connection(("127.0.0.1", 8093), 2); s.close(); break
    except Exception:
        time.sleep(500)
else:
    print("PORT_DOWN", proc.poll(), flush=True)
    proc.terminate(); exit(1)
print("PORT_UP", flush=True)

time.sleep(3)
print("alive_after_3s", proc.poll() is None, "poll", proc.poll(), flush=True)

class _NR(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None
noprop = urllib.request.build_opener(_NR)

def get(path):
    req = urllib.request.Request(BASE + path, headers={"Cookie": "session_token=" + TOKEN})
    try:
        resp = noprop.open(req, timeout=10)
        return resp.status, resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")

def post(path, data):
    req = urllib.request.Request(BASE + path, data=urllib.parse.urlencode(data).encode("utf-8"),
        headers={"Content-Type": "application/x-www-form-urlencoded", "Cookie": "session_token=" + TOKEN}, method="POST")
    try:
        resp = noprop.open(req, timeout=10)
        return resp.status, resp.geturl(), resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.geturl(), e.read().decode("utf-8", "replace")

req = urllib.request.Request(BASE + "/login",
    data=urllib.parse.urlencode({"username": "owner", "password": "Owner001!"}).encode("utf-8"),
    headers={"Content-Type": "application/x-www-form-urlencoded"})
try:
    resp = noprop.open(req, timeout=10)
except urllib.error.HTTPError as e:
    resp = e
    print("login", e.code, flush=True)
TOKEN = re.search(r"session_token=([^;]+)", resp.headers.get("Set-Cookie", "")).group(1)
print("TOKEN", TOKEN, flush=True)

form = {"vehicle_id": "1", "current_mileage": "5000", "is_insurance": "1",
    "insurance_company_id_mirror": "1", "claim_no": "CLM-999",
    "accident_date": "2026-10-01", "deductible_amount": "1500"}
code, url, body = post("/work-orders", form)
print("POST", code, url, flush=True)
print("body_head:", body[:300], flush=True)
print("alive_after_POST", proc.poll() is None, flush=True)
print("===ERR===")
print(errf.read().decode("utf-8", "replace")[:1000], flush=True)
proc.terminate()
try: proc.wait(timeout=3)
except Exception: proc.kill()
outf.close(); errf.close()
