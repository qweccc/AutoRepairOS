# -*- coding: utf-8 -*-
import os, subprocess, time, socket

ROOT = "C:\\Users\\fr\\AutoRepairOS"
EXE = os.path.join(ROOT, "autos.exe")
ISO = os.path.join(ROOT, "data_iso", "autorepair_iso.db")
env = os.environ.copy()
env["PORT"] = "8092"
env["DB_PATH"] = "file:" + ISO + "?_busy_timeout=5000"
env["PYTHONUTF8"] = "1"

proc = subprocess.Popen([EXE], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, cwd=ROOT)
for _ in range(60):
    try:
        s = socket.create_connection(("127.0.0.1", 8092), 2); s.close(); break
    except Exception: time.sleep(500)
else:
    proc.terminate(); exit(1)

import urllib.request
resp = urllib.request.urlopen("http://localhost:8092/work-orders", timeout=10)
body = resp.read().decode("utf-8", "replace")
os.makedirs(ROOT + "\\scripts", exist_ok=True)
open(ROOT + "\\scripts\\wo_body.txt", "w", encoding="utf-8").write(body)
print("STATUS", resp.status, "LEN", len(body), flush=True)
proc.terminate()
try: proc.wait(timeout=3)
except Exception: proc.kill()
