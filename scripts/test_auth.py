import urllib.request, urllib.parse, http.cookiejar, re

BASE = "http://localhost:8090"

def make_session():
    cj = http.cookiejar.CookieJar()
    return urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))

def req(op, path, data=None):
    body = urllib.parse.urlencode(data, encoding="utf-8").encode("utf-8") if data else None
    r = urllib.request.Request(BASE + path, data=body)
    try:
        resp = op.open(r)
        return resp.status, resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")

results = []
def check(name, cond, detail=""):
    results.append(cond)
    print(f"[{'PASS' if cond else 'FAIL'}] {name} {detail}")

ACCOUNTS = [
    ("owner", "Owner001!", "管理員"),
    ("counter", "Counter001!", "櫃檯"),
    ("tech", "Tech001!", "技師"),
]

print("=== 三角色登入驗證 ===")
for user, pwd, label in ACCOUNTS:
    op = make_session()
    st, body = req(op, "/login", {"username": user, "password": pwd})
    ok = st == 200
    check(f"{label} ({user}) 登入", ok)
    if ok:
        st2, me = req(op, "/api/me")
        m = re.search(r'"role":"(\w+)"', me)
        role = m.group(1) if m else "?"
        expected = "technician" if user == "tech" else user
        check(f"{label} role={role}", role == expected, f"expected={expected}")

print("\n=== 未登入存取保護 ===")
op = make_session()
for path, expect in [("/api/me", 401), ("/work-orders", None), ("/inventory", None)]:
    st, body = req(op, path)
    if expect:
        check(f"未登入 {path} -> {expect}", st == expect, f"got={st}")
    else:
        # 應該導向登入
        ok = st == 200 and "登入" in body
        check(f"未登入 {path} 導向登入頁", ok, f"got={st}")

print("\n=== 錯誤密碼 ===")
op = make_session()
st, body = req(op, "/login", {"username": "owner", "password": "wrongpass"})
check("錯誤密碼被拒", st == 401, f"got={st}")

print("\n=== SUMMARY ===")
print(f"PASS={sum(results)} FAIL={len(results)-sum(results)}")
