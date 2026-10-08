# -*- coding: utf-8 -*-
import http.cookiejar
import urllib.request
import urllib.parse
import urllib.error
import subprocess
import sys
import os
import time
import re
import shutil
import socket

ROOT = "C:\\Users\\fr\\AutoRepairOS"
EXE = os.path.join(ROOT, "autorepair_bin.exe")
ISO_DB = os.path.join(ROOT, "data_iso", "autorepair_iso.db")
RESULT = os.path.join(ROOT, "scripts", "http_8091_result.txt")

os.makedirs(os.path.dirname(RESULT), exist_ok=True)
if os.path.exists(RESULT):
    os.remove(RESULT)

# fresh iso db from original (guarantees owner/counter/tech exist)
if os.path.exists(ROOT + "\\data\\autorepair.db"):
    shutil.copy(ROOT + "\\data\\autorepair.db", ISO_DB)

env = os.environ.copy()
env["PORT"] = "8091"
env["DB_PATH"] = "file:" + ISO_DB + "?_busy_timeout=5000"
env["PYTHONUTF8"] = "1"

def line(*msg):
    out = [str(m) for m in msg]
    print(" ".join(out), flush=True)
    with open(RESULT, "a", encoding="utf-8") as f:
        f.write(" ".join(out) + "\n")

BASE = "http://localhost:8091"
cj = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))

def call(path, data=None, method="GET"):
    if data is None:
        req = urllib.request.Request(BASE + path, method=method)
    else:
        body = urllib.parse.urlencode(data).encode("utf-8")
        req = urllib.request.Request(BASE + path, data=body,
                                     headers={"Content-Type": "application/x-www-form-urlencoded"}, method=method)
    try:
        resp = opener.open(req, timeout=10)
        ct = resp.read().decode("utf-8", "replace")
        return resp.status, resp.geturl(), ct
    except urllib.error.HTTPError as e:
        ct = e.read().decode("utf-8", "replace")
        return e.code, e.geturl(), ct

# 0. start the binary
line("== start binary ==")
proc = subprocess.Popen([EXE], env=env,
                        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                        cwd=ROOT)
print("pid", proc.pid, flush=True)

# wait for port
line("== waiting for port ==")
for _ in range(60):
    try:
        s = socket.create_connection(("127.0.0.1", 8091), timeout=2)
        s.close()
        break
    except Exception:
        time.sleep(500)
else:
    line("TIMEOUT waiting for port 8091")
    line("STDERR:")
    try:
        line(proc.stderr.read().decode("utf-8", "replace")[:2000] if proc.stderr else "")
    except Exception as e:
        line(str(e))
    proc.terminate()
    sys.exit(1)

line("== 1. login owner ==")
s, u, b = call("/login", {"username": "owner", "password": "***"}, "POST")
line("login", s, "cookie", [c.name for c in cj])
ok = any(c.name == "session_token" for c in cj) and s in (200, 302)
line("LOGIN_OK" if ok else "LOGIN_FAILED:" + repr(b[:200]))
if not ok:
    proc.terminate()
    proc.kill()
    sys.exit(2)

line("== 2. list (initial, empty) ==")
s, u, b = call("/insurance-companies")
line("list", s, "has_header", "保險公司" in b, "has_create_form", "新增保險公司" in b, "has_list_card", "保險公司（" in b, "has_empty", "尚無" in b or "empty" in b.lower())

line("== 3. create ==")
s, u, b = call("/insurance-companies", {
    "code": "E2E-HTTP", "name": "E2E Insurance Co",
    "tax_id": "12345678", "claim_phone": "0912-345-678",
    "contact_person": "Ann", "address": "Taipei"}, "POST")
line("create", s, "redirect", u, "create_ok", "已新增" in b)

line("== 4. list (after create) ==")
s, u, b = call("/insurance-companies")
line("list", s, "has_code", "E2E-HTTP" in b)
mid = None
for m in re.finditer(r'/insurance-companies/(\d+)/edit', b):
    mid = int(m.group(1)); break
line("mid", mid)

line("== 5. edit form ==")
s, u, b = call("/insurance-companies/%d/edit" % mid)
line("edit", s, "has_form", "E2E Insurance Co" in b, "has_submit", "儲存" in b, "has_phone", "電話" in b)

line("== 6. update ==")
s, u, b = call("/insurance-companies/%d" % mid, {
    "code": "E2E-HTTP", "name": "E2E Insurance Co",
    "tax_id": "12345678", "claim_phone": "0912-345-678",
    "contact_person": "Ann", "address": "Taipei",
    "phone": "02-1234-5678", "postal_code": "100"}, "POST")
line("update", s, "ok", "已更新" in b)

line("== 7. list (after update) ==")
s, u, b = call("/insurance-companies")
line("list", s, "has_code", "E2E-HTTP" in b, "has_phone", "02-1234-5678" in b)

line("== 8. edit again (show saved phone) ==")
s, u, b = call("/insurance-companies/%d/edit" % mid)
line("edit", s, "shows_phone", "02-1234-5678" in b)

line("== 9. delete (deactivate) ==")
s, u, b = call("/insurance-companies/%d/delete" % mid)
line("delete", s, "ok", "已停用" in b)

line("== 10. list (after deactivate) ==")
s, u, b = call("/insurance-companies")
line("list", s, "has_code", "E2E-HTTP" in b, "has_empty", "尚無" in b or "empty" in b.lower())

line("ALL_DONE")

# cleanup
try:
    proc.terminate()
    proc.wait(timeout=3)
except Exception:
    proc.kill()
print("DONE", flush=True)
