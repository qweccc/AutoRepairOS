import os, subprocess, time, socket, re

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

def curl(args):
    p = subprocess.run(["curl.exe"] + args, capture_output=True, encoding="utf-8", errors="replace")
    return p.returncode, p.stdout, p.stderr

# login: curl saves cookie natively
rc, out, err = curl(["-s", "-c", os.path.join(ROOT,"scripts","cookies.txt"),
    "-X POST", BASE + "/login",
    "--data", "username=owner&password=Owner001!"])
print("login", rc, err.strip()[:200])

# show cookies file
print("=== cookies file ===")
try:
    print(open(os.path.join(ROOT,"scripts","cookies.txt"),encoding="utf-8").read())
except Exception as e:
    print("ERR", e)

# POST with cookie
form = "vehicle_id=1&current_mileage=5000&is_insurance=1&insurance_company_id_mirror=1&claim_no=CLM-999&accident_date=2026-10-01&deductible_amount=1500"
rc, out, err = curl(["-s", "-b", os.path.join(ROOT,"scripts","cookies.txt"),
    "-X POST", BASE + "/work-orders",
    "-H", "Content-Type: application/x-www-form-urlencoded",
    "--data", form])
print("post", rc, "url=", out.strip())
print("stderr", err.strip()[:300])

# GET list to confirm cookie accepted
rc, out, err = curl(["-s", "-b", os.path.join(ROOT,"scripts","cookies.txt"), BASE + "/work-orders"])
print("get", rc, "has_owner", "owner" in out, "wos_count", out.count("work-orders/"))

proc.terminate()
try: proc.wait(timeout=3)
except Exception: proc.kill()
