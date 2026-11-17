#!/usr/bin/env python3
"""新版 exe 冒煙測試（隔離連接埠 + 隔離 DB，不動正式服務）。

重點不在「首頁有開」，而是確認**上一版壞掉的功能在新版真的可用**：
遷移漏掛的端點（收款新增/編輯、進貨單新增、各頁即時搜尋）在畫面流程
都要回 200/正常行為，而不只是路由存在。

用法：python3 release_smoke.py [exe 路徑]
預設測 release/ 下最新的 AutoRepairOS-*.exe。
"""
import http.cookiejar
import os
import re
import shutil
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

ROOT = r"C:/Users/fr/AutoRepairOS"
PORT = 8099
BASE = f"http://127.0.0.1:{PORT}"


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def main():
    exe = sys.argv[1] if len(sys.argv) > 1 else latest_exe()
    print(f"受測：{exe}")
    work = r"C:/Users/fr/AppData/Local/hermes/cache/scratch/smoke"
    shutil.rmtree(work, ignore_errors=True)
    os.makedirs(work, exist_ok=True)
    db = os.path.join(work, "smoke.db")
    log = open(os.path.join(work, "server.log"), "w", encoding="utf-8")

    user = pw = ""
    for line in open(os.path.join(ROOT, ".env"), encoding="utf-8"):
        if line.startswith("DEFAULT_OWNER_USERNAME="):
            user = line.strip().split("=", 1)[1]
        elif line.startswith("DEFAULT_OWNER_PASSWORD="):
            pw = line.strip().split("=", 1)[1]

    env = dict(os.environ)
    env.update({
        "PORT": str(PORT),
        "DB_PATH": db,
        "RUN_SEED": "true",
        "DEFAULT_OWNER_USERNAME": user,
        "DEFAULT_OWNER_PASSWORD": pw,
    })
    proc = subprocess.Popen([exe], cwd=work, env=env, stdout=log, stderr=subprocess.STDOUT)
    try:
        wait_ready()
        run_checks(user, pw)
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            proc.kill()
        log.close()


def latest_exe():
    exes = [f for f in os.listdir(os.path.join(ROOT, "release")) if f.endswith(".exe")]
    exes.sort()
    return os.path.join(ROOT, "release", exes[-1])


def wait_ready():
    for _ in range(60):
        try:
            urllib.request.urlopen(BASE + "/login", timeout=2)
            return
        except Exception:
            time.sleep(0.5)
    raise SystemExit("server 起不來，看 server.log")


def opener():
    cj = http.cookiejar.CookieJar()
    op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj), NoRedirect)
    return op, cj


def raw(op, method, path, data=None, ctype="application/x-www-form-urlencoded"):
    body = urllib.parse.urlencode(data or {}).encode()
    req = urllib.request.Request(BASE + path, data=body or None, method=method,
                                 headers={"Content-Type": ctype} if body else {})
    try:
        r = op.open(req, timeout=20)
        return r.getcode(), r.read(), dict(r.headers)
    except urllib.error.HTTPError as e:
        return e.code, e.read(), dict(e.headers)
    except OSError as e:
        return -1, str(e).encode(), {}


def csrf(html_text):
    m = re.search(r'name="_csrf"\s+value="([^"]+)"', html_text)
    return m.group(1) if m else ""


def run_checks(user, pw):
    fails = []

    def check(name, cond, detail=""):
        print(("PASS  " if cond else "FAIL  ") + name + (f"  {detail}" if detail else ""))
        if not cond:
            fails.append(name)

    op, _ = opener()
    code, body, _ = raw(op, "GET", "/login")
    check("登入頁 200", code == 200, f"code={code}")
    token = csrf(body.decode("utf-8", "replace"))
    code, _, loc = raw(op, "POST", "/login", {"username": user, "password": pw, "_csrf": token})
    check("登入成功 302", code in (301, 302, 303), f"code={code}")

    # 一般頁面
    for page in ["/", "/customers", "/vehicles", "/work-orders", "/inventory",
                 "/receivables", "/reports", "/analytics"]:
        code, _, _ = raw(op, "GET", page)
        check(f"GET {page} 200", code == 200, f"code={code}")

    # 遷移漏掛修復後應可用的端點（前一版全是 404）。
    # 有 seed 資料的：必須 200。
    for p in ["/purchase-orders/new", "/inventory/1/edit", "/suppliers/1/edit",
              "/staff/1/edit", "/phrases/1/edit", "/lookup/1/edit"]:
        code, _, _ = raw(op, "GET", p)
        check(f"路由在 {p}", code == 200, f"code={code}")

    # seed 沒有 id=1 資料的編輯頁：handler 查無資料會回 404（與路由沒掛難以
    # 由回應分辨），所以這裡只確認不是 5xx；路由是否存在由
    # internal/routes 的 TestEveryRenderedPathHasARoute 全面把關。
    for p in ["/appointments/1/edit", "/subcontracts/1/edit"]:
        code, _, _ = raw(op, "GET", p)
        check(f"{p} 不是 5xx", 200 <= code < 500, f"code={code}")

    # 真的建一筆再開編輯頁：證明「新增→編輯」这条路是通的（裝置設備）
    code, body, _ = raw(op, "GET", "/equipment")
    tok = csrf(body.decode("utf-8", "replace"))
    code, _, _ = raw(op, "POST", "/equipment", {"name": "smoke-test-lift", "_csrf": tok})
    check("POST /equipment 成立", code in (301, 302, 303), f"code={code}")
    code, _, _ = raw(op, "GET", "/equipment/1/edit")
    check("路由在 /equipment/1/edit（建資料後）", code == 200, f"code={code}")

    # 收款新增表單（結帳流程的第一哩路）：action 必須是 /receivables，
    # 不可空字串（空＝送回目前網址 /receivables/new，那裡沒有 POST 路由）。
    code, body, _ = raw(op, "GET", "/receivables/new")
    check("GET /receivables/new 200", code == 200, f"code={code}")
    actions = [a.decode() for a in re.findall(rb'<form[^>]*action="([^"]*)"', body)]
    check("收款頁沒有空 action 的表單", "" not in actions, f"actions={actions}")
    check("收款表單 action 為 /receivables", "/receivables" in actions, f"actions={actions}")

    # API 即時搜尋（UI 有引用、之前 404）
    for api in ["/api/purchase-orders/search", "/api/suppliers/quick-create",
                "/api/lookup/search?q=a", "/api/receivables/search?q=a"]:
        code, _, _ = raw(op, "GET", api)
        check(f"API {api} 非 404", code != 404, f"code={code}")

    # 靜態資源：與 embed 來源比對
    import hashlib
    code, body, _ = raw(op, "GET", "/static/app.css")
    src = open(os.path.join(ROOT, "internal/handler/static/app.css"), "rb").read()
    check("app.css md5 與來源一致",
          hashlib.md5(body).hexdigest() == hashlib.md5(src).hexdigest(),
          f"{len(body)} bytes")

    # 啟動log 的版本戳
    log_text = open(r"C:/Users/fr/AppData/Local/hermes/cache/scratch/smoke/server.log",
                    encoding="utf-8", errors="replace").read()
    m = re.search(r"git[^\n]*?([0-9a-f]{7})", log_text)
    check("啟動 log 含 git 修訂", bool(m), m.group(0) if m else "找不到")

    print(f"\n{len(fails)} 個失敗" if fails else "\n全部 PASS")
    if fails:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
