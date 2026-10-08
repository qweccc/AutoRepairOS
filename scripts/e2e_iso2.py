# -*- coding: utf-8 -*-
# Minimal isolated POST test: login -> POST work-orders, print full body.
import os, subprocess, time, socket, urllib.request, urllib.parse, re

ROOT = r"C:\Users\fr\AutoRepairOS"
EXE = os.path.join(ROOT, "autos.exe")
BASE = "http://localhost:8093"
os.makedirs(os.path.join(ROOT, "scripts"), exist_ok=True)

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
    print("PORT_DOWN", proc.poll()); proc.terminate(); exit(1)
print("PORT_UP")

class _NR(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None
noprop = urllib.request.build_opener(_NR)

req = urllib.request.Request(BASE + "/login",
    data=urllib.parse.urlencode({"username": "owner", "password": "Owner001!"}).encode("utf-8"),
    headers={"Content-Type": "application/x-www-form-urlencoded"})
try:
    resp = noprop.open(req, timeout=10)
except urllib.error.HTTPError as e:
    resp = e
    print("login_status", e.code)
setc = resp.headers.get("Set-Cookie", "")
print("setc:", setc)
m = re.search(r"session_token=([^;]+)", setc)
tok = m.group(1) if m else None
print("tok:", tok)
cookie = "session_token=" + tok if tok else ""

form = urllib.parse.urlencode({
    "vehicle_id": "1", "current_mileage": "5000", "is_insurance": "1",
    "insurance_company_id_mirror": "1", "claim_no": "CLM-999",
    "accident_date": "2026-10-01", "deductible_amount": "1500"})
req = urllib.request.Request(BASE + "/work-orders", data=form.encode("utf-8"),
    headers={"Content-Type": "application/x-www-form-urlencoded", "Cookie": cookie}, method="POST")
try:
    resp = noprop.open(req, timeout=10)
    print("create_status", resp.status, resp.geturl())
    body = resp.read().decode("utf-8", "replace")
    print("body_head:", body[:400])
except urllib.error.HTTPError as e:
    print("create_status", e.code, e.geturl())
    body = e.read().decode("utf-8", "replace")
    print("body_head:", body[:400])

proc.terminate()
try: proc.wait(timeout=3)
except Exception: proc.kill()
