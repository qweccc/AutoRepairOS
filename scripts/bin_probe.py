# -*- coding: utf-8 -*-
import os, subprocess, time, socket

ROOT = "C:\\Users\\fr\\AutoRepairOS"
EXE = os.path.join(ROOT, "autos.exe")
ISO = os.path.join(ROOT, "data_iso", "autorepair_iso.db")
env = os.environ.copy()
env["PORT"] = "8092"
env["DB_PATH"] = "file:" + ISO + "?_busy_timeout=5000"
env["PYTHONUTF8"] = "1"

def line(*m):
    print(" ".join(str(x) for x in m), flush=True)

proc = subprocess.Popen([EXE], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, cwd=ROOT)
line("pid", proc.pid)
for _ in range(60):
    try:
        s = socket.create_connection(("127.0.0.1", 8092), 2); s.close(); break
    except Exception:
        time.sleep(500)
else:
    line("TIMEOUT"); proc.terminate(); exit(1)

import urllib.request
try:
    resp = urllib.request.urlopen("http://localhost:8092/work-orders", timeout=10)
    body = resp.read().decode("utf-8", "replace")
    line("STATUS", resp.status)
    line("has_form", "開立新工單" in body)
    line("has_empty", "沒有工單" in body)
except Exception as e:
    line("ERR", repr(e))

# probe if alive
import urllib.error
try:
    r2 = urllib.request.urlopen("http://localhost:8092/work-orders", timeout=5)
    line("SECOND", r2.status)
except urllib.error.HTTPError as e:
    line("SECOND_HTTPERR", e.code)
except Exception as e:
    line("SECOND_ERR", repr(e))

line("LIVE" if proc.poll() is None else "DEAD")
try: proc.terminate(); proc.wait(timeout=3)
except Exception: proc.kill()
