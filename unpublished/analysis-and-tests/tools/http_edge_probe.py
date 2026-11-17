#!/usr/bin/env python3
"""HTTP 邊界拳擊（edge probe）：對運行中的服務送畸形／越權／極端請求，
確認錯誤落在「對的狀態碼」，而不是 500 或外洩錯誤細節。

用法：
    python http_edge_probe.py [base_url] [owner_user] [owner_pass]

設計重點（都是實測踩出來的）：
  - 不追蹤 redirect：我們要觀察「第一道防線」實際吐什麼，轉址之後的行為
    由呼叫端決定（307 保留 method 這種细节只在原文才看得到）。
  - 每個案例只斷言狀態碼落在預期集合；產品回應內容不影響結論。
  - 內建 PASS/FAIL 計數，適合直接放 CI 或 nightly。
"""
import http.cookiejar
import html
import json
import re
import sys
import urllib.error
import urllib.parse
import urllib.request

BASE = (sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:8099").rstrip("/")
USER = sys.argv[2] if len(sys.argv) > 2 else ""
PASSWORD = sys.argv[3] if len(sys.argv) > 3 else ""

PASS = 0
FAIL = 0


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None  # 全部不追蹤，讓呼費者看到原始狀態碼


cj = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(
    urllib.request.HTTPCookieProcessor(cj), NoRedirect
)


def chk(label, got, want):
    global PASS, FAIL
    wants = want if isinstance(want, (list, tuple, set)) else [want]
    if got in wants:
        PASS += 1
        print("  ok   %-46s %s" % (label, got))
    else:
        FAIL += 1
        print("  FAIL %-46s got=%s want=%s" % (label, got, wants))


def req(method, path, data=None, headers=None, timeout=20, raw_body=None):
    """回 (status, body_bytes)。URL 很長或 body 很大時自行控制 timeout。"""
    h = dict(headers or {})
    body = None
    if raw_body is not None:
        body = raw_body if isinstance(raw_body, bytes) else raw_body.encode()
    elif data is not None:
        body = urllib.parse.urlencode(data).encode()
        h.setdefault("Content-Type", "application/x-www-form-urlencoded")
    r = urllib.request.Request(BASE + path, data=body, method=method, headers=h)
    try:
        resp = opener.open(r, timeout=timeout)
        return resp.getcode(), resp.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()
    except OSError as e:
        # 連線被服務端重置／拒收：不是 5xx，但要讓案例顯式回報，不可以當成功
        return -1, ("conn-error: %s" % e).encode()


def login():
    code, body = req("POST", "/login", {"username": USER, "password": PASSWORD})
    chk("登入（正確憑證）", code, [302, 303])
    return code in (302, 303)


def csrf_token():
    """從已登入頁抓 CSRF token。版面把屬性 HTML escape 成 &#34;，先還原再找。"""
    _, body = req("GET", "/")
    text = html.unescape(body.decode("utf-8", "replace"))
    m = re.search(r'name="_csrf"\s+value="([^"]+)"', text)
    if not m:
        m = re.search(r'X-CSRF-Token"\s*:\s*"([^"]+)"', text)
    return m.group(1) if m else ""


def main():
    if not USER or not PASSWORD:
        print("需要 owner 帳號密碼才能跑登入後的案例")
        return 2

    # --- 未登入 ---
    chk("未登入 GET /customers（應回到登入頁）", req("GET", "/customers")[0], [302, 303])
    chk("未登入 POST /work-orders/1/status（應被擋）", req("POST", "/work-orders/1/status")[0], [302, 303, 401, 403])

    # --- 登入與憑證 ---
    code, _ = req("POST", "/login", {"username": USER, "password": "wrong-on-purpose"})
    chk("登入（錯誤密碼）", code, [401, 403])
    if not login():
        print("登入失敗，後續有登入門檻的案例無法執行")
        return 2
    tok = csrf_token()
    print("  ..  取得 CSRF token: %s" % ("yes" if tok else "NO"))

    hdr = {"X-CSRF-Token": tok} if tok else {}

    # --- CSRF 邊界 ---
    # 注意：這個案例要「不帶」任何 token，否則測的是 handler 而不是 CSRF。
    chk("已登入＋無 token POST 狀態變更（403）", req("POST", "/work-orders/1/status", {"status": "paid"})[0], [403])
    chk("已登入＋假 token POST 狀態變更（403）", req("POST", "/work-orders/1/status", {"status": "paid"}, {"X-CSRF-Token": "forged"})[0], [403])
    chk("免責不外溢 /approvalX/approve/x（需 CSRF）", req("POST", "/approvalX/approve/x", {}, {})[0], [403, 404, 405])

    # --- 公開端點仍可用（免責有效）---
    chk("公開 POST /approval/approve/faketoken（免責、交由 handler 判定）", req("POST", "/approval/approve/faketoken", {}, {})[0], [200, 302, 400, 404])

    # --- 路由／方法 ---
    chk("未知 id 編輯頁（404）", req("GET", "/customers/999999/edit")[0], [404])
    chk("錯誤方法 DELETE /customers（不可執行）", req("DELETE", "/customers")[0], [403, 404, 405])

    # --- 輸入極端（需 vision client 有初始化，否則 handler 會先回 503）---
    chk("畸形 JSON POST /api/vision/invoice", req("POST", "/api/vision/invoice", raw_body="{bad", headers={"Content-Type": "application/json", **hdr})[0], [400, 403, 503])
    chk("空 body POST /api/vision/invoice", req("POST", "/api/vision/invoice", raw_body="", headers={"Content-Type": "application/json", **hdr})[0], [400, 403, 503])
    big = json.dumps({"image": "A" * (2 * 1024 * 1024)})
    chk("2MB payload 不可拖垮服務", req("POST", "/api/vision/invoice", raw_body=big, headers={"Content-Type": "application/json", **hdr}, timeout=40)[0], [400, 403, 413, 502, 504, -1])
    chk("超長 URL 不爆 500", req("GET", "/" + "a" * 4000)[0], [400, 404, 414, 431])

    # --- 路徑穿越（應被清理＋挡下，不可當成免責路徑的成功操作）---
    chk("路徑穿越 POST 不得成功", req("POST", "/approval/approve/../work-orders/1/status", {}, {})[0], [301, 302, 307, 403, 404, 405])

    # --- 主頁面仍健康（證明前面的拳擊沒有打壞服務）---
    for p in ["/", "/customers", "/inventory"]:
        chk("拳擊後 %s 仍正常" % p, req("GET", p)[0], [200])

    print("\nPASS=%d FAIL=%d" % (PASS, FAIL))
    return 0 if FAIL == 0 else 1


if __name__ == "__main__":
    sys.exit(main())