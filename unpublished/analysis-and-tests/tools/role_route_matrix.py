#!/usr/bin/env python3
"""角色 × GET 路由 實機權限矩陣。

讓 owner / counter / technician 各自登入同一個跑著的服務，逐條打
internal/routes/routes.go 裡的 GET 路由，把「實際能不能進去」與
router 的權限包裝器（Wrapper）對照，抓出兩類不一致：

  A. 不該進得去卻進得去   —— 越權（wrapper 太寬、handler 沒再擋）
  B. 看得到卻進不去       —— 死路（nav 有連結，但實際被擋）

用法：
    python role_route_matrix.py <routes.go> <base_url> \
        <owner_u> <owner_p> <counter_u> <counter_p> <tech_u> <tech_p>

設計：
  - 期望值來自路由原始碼（不是手寫清單），所以權限一改就能重跑對帳。
  - 已知「handler 內部再擋一次」的路徑写在 OVERRIDES（與
    internal/handler/nav_roles_sync_test.go 同一份事實來源）。
  - 路徑參數用 /1 頂替即可：權限發生在 wrapper， wrapper 早於 handler
    查資料，所以不存在的資料會得到 404（=ALLOWED），被擋是 403（=DENIED）。
"""
import http.cookiejar
import re
import sys
import urllib.error
import urllib.parse
import urllib.request

ROUTES_GO = sys.argv[1]
BASE = sys.argv[2].rstrip("/")
CREDS = {
    "owner": (sys.argv[3], sys.argv[4]),
    "counter": (sys.argv[5], sys.argv[6]),
    "technician": (sys.argv[7], sys.argv[8]),
}
ROLE_ORDER = ["owner", "counter", "technician"]

# 與 internal/handler/authz.go 的實作一致
WRAPPER_ROLES = {
    "AllStaff": ROLE_ORDER,
    "CounterUpPage": ["owner", "counter"],
    "CounterUp": ["owner", "counter"],
    "OwnerOnly": ["owner"],
}
REQUIRE_LOGIN = ["RequireLogin"]

# handler 內部再擋一次的路徑（wrapper 放行但實際更嚴）
OVERRIDES = {
    "/reports": ["owner", "counter"],  # ReportsHome: canSeeMoney
    "/technicians/commissions": ["owner", "counter"],  # 佣金表：canSeeMoney
}

# 逐筆授權（IDOR 防護）的路由：能不能看取決於「這筆資料屬不屬於我」，
# 用固定 id=1 去探一定是「不屬於技師」→ 403 是正確行為，不可當成死路。
PER_RECORD_OWNERSHIP = {"/work-orders/{id}/worklogs"}

NO_REDIRECT = None


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return NO_REDIRECT


def build_opener():
    cj = http.cookiejar.CookieJar()
    return urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj), NoRedirect), cj


def request(opener, method, path, data=None, headers=None):
    h = dict(headers or {})
    body = None
    if data is not None:
        body = urllib.parse.urlencode(data).encode()
        h.setdefault("Content-Type", "application/x-www-form-urlencoded")
    r = urllib.request.Request(BASE + path, data=body, method=method, headers=h)
    try:
        resp = opener.open(r, timeout=20)
        return resp.getcode(), resp.headers.get("Location", "")
    except urllib.error.HTTPError as e:
        return e.code, e.headers.get("Location", "") if e.headers else ""
    except OSError as e:
        return -1, str(e)


def login(opener, user, pw):
    code, _ = request(opener, "POST", "/login", {"username": user, "password": pw})
    return code in (301, 302, 303, 307, 308)


def expected_roles(path, wrapper):
    if path in OVERRIDES:
        return OVERRIDES[path]
    if wrapper in WRAPPER_ROLES:
        return WRAPPER_ROLES[wrapper]
    if wrapper in REQUIRE_LOGIN:
        return ROLE_ORDER
    return None  # 沒有權限包裝器＝公開（不列入角色矩陣）


def normalize(p):
    if p.endswith("{$}"):
        p = p[: -len("{$}")]
    return p or "/"


def classify(status, location):
    if status == 403:
        return "DENIED"
    if status in (301, 302, 303, 307, 308) and "/login" in (location or ""):
        return "DENIED"
    if status >= 500:
        return "ERROR"
    return "ALLOWED"


def main():
    src = open(ROUTES_GO, encoding="utf-8").read()
    seen = set()
    routes = []
    for m in re.finditer(r'r\.Get\("([^"]+)",\s*(?:handler\.)?([A-Za-z]+)\(', src):
        path, wrapper = normalize(m.group(1)), m.group(2)
        if path in seen:
            continue
        seen.add(path)
        probe = re.sub(r"\{[^}]+\}", "1", path)
        routes.append((path, probe, wrapper))

    print("解析到 %d 條 GET 路由（已去除重複）" % len(routes))
    opm = {}
    for role, (u, p) in CREDS.items():
        opener, _ = build_opener()
        ok = login(opener, u, p)
        print("  %s 登入：%s" % (role, "OK" if ok else "失敗"))
        opm[role] = opener

    over_grant, dead_end, errors, checked = [], [], [], 0
    public = []
    per_record = []
    detail = []
    for path, probe, wrapper in routes:
        exp = expected_roles(path, wrapper)
        if exp is None:
            public.append(path)
            continue
        for role in ROLE_ORDER:
            code, loc = request(opm[role], "GET", probe)
            got = classify(code, loc)
            checked += 1
            want_allow = role in exp
            detail.append((path, role, code, got, want_allow))
            if got == "ERROR":
                errors.append((path, role, code))
            elif path in PER_RECORD_OWNERSHIP and got == "DENIED":
                per_record.append((path, role, code))  # IDOR 防護下的正常拒絕
            elif want_allow and got == "DENIED":
                dead_end.append((path, role, code, wrapper))
            elif not want_allow and got == "ALLOWED":
                over_grant.append((path, role, code, wrapper))

    print("\n檢查次數：%d（%d 條路由 × 3 角色，公開路由 %d 條不列入）" % (checked, len(routes) - len(public), len(public)))
    print("\n=== A. 越權：不該進得去卻進得去 ===")
    if over_grant:
        for p, r, c, w in over_grant:
            print("  %-34s %-11s HTTP %s (wrapper=%s)" % (p, r, c, w))
    else:
        print("  無")
    print("\n=== B. 死路：看得到卻進不去 ===")
    if dead_end:
        for p, r, c, w in dead_end:
            print("  %-34s %-11s HTTP %s (wrapper=%s)" % (p, r, c, w))
    else:
        print("  無")
    print("\n=== C. 伺服器錯誤（5xx）===")
    if errors:
        for p, r, c in errors:
            print("  %-34s %-11s HTTP %s" % (p, r, c))
    else:
        print("  無")
    print("\n=== D. 逐筆授權下的正常拒絕（IDOR 防護，非缺陷）===")
    if per_record:
        for p, r, c in per_record:
            print("  %-34s %-11s HTTP %s" % (p, r, c))
    else:
        print("  無")
    print("\n公開路由（不驗角色）：%s" % ", ".join(public))
    print("\nMATRIX_SUMMARY checked=%d over_grant=%d dead_end=%d errors=%d public=%d"
          % (checked, len(over_grant), len(dead_end), len(errors), len(public)))
    return 0 if not (over_grant or dead_end or errors) else 1


if __name__ == "__main__":
    sys.exit(main())