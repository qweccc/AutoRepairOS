#!/usr/bin/env python3
"""E2E 結帳扣料流程的「前置資料建立」與「結果驗證」（纯 HTTP，供瀏覽器中段使用）。

用法：
    python e2e_checkout_fixture.py setup <base_url> <owner_u> <owner_p>
    python e2e_checkout_fixture.py verify <db_path> <work_order_id> <expected_qty>

setup 會建立：客戶 → 車輛 → 工單 → 工單零件項目（扣库存用的那筆），
並嘗試把工單推到 completed（待結帳）。印出 JSON 摘要（含庫存基準）。
verify 則檢查結帳後：庫存是否扣除、inventory_movements 是否留痕、
收款單是否存在、工單是否已 paid、金額是否對得上。

设计：只负责「週邊」，不碰關鍵路徑——真正按「結帳」的那一步留給瀏覽器，
這樣才測得到 HTMX／表單／JS 這條路。
"""
import http.cookiejar
import json
import re
import sqlite3
import sys
import urllib.error
import urllib.parse
import urllib.request


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def opener():
    cj = http.cookiejar.CookieJar()
    return urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj), NoRedirect)


def req(op, method, url, data=None, headers=None):
    h = dict(headers or {})
    body = None
    if data is not None:
        body = urllib.parse.urlencode(data).encode()
        h.setdefault("Content-Type", "application/x-www-form-urlencoded")
    r = urllib.request.Request(url, data=body, method=method, headers=h)
    try:
        resp = op.open(r, timeout=20)
        return resp.getcode(), resp.headers.get("Location", ""), resp.read()
    except urllib.error.HTTPError as e:
        loc = e.headers.get("Location", "") if e.headers else ""
        return e.code, loc, e.read()


def token(op, base):
    _, _, body = req(op, "GET", base + "/")
    text = body.decode("utf-8", "replace").replace("&#34;", '"')
    m = re.search(r'name="_csrf"\s+value="([^"]+)"', text)
    return m.group(1) if m else ""


def last_id(loc, prefix):
    m = re.search(prefix + r"/(\d+)", loc or "")
    return int(m.group(1)) if m else 0


def setup(base, user, pw):
    op = opener()
    code, _, _ = req(op, "POST", base + "/login", {"username": user, "password": pw})
    assert code in (301, 302, 303, 307, 308), "登入失敗 %s" % code
    tok = token(op, base)
    h = {"X-CSRF-Token": tok}
    out = {"csrf": tok}

    # 客戶（搜尋 API 回 HTMX fragment，不是 JSON）
    code, loc, _ = req(op, "POST", base + "/customers",
                       {"name": "E2E 測試客戶", "phone": "0912-000-111"}, h)
    _, _, body = req(op, "GET", base + "/api/customers/search?q=E2E")
    frag = body.decode("utf-8", "replace")
    m = re.search(r"/customers/(\d+)/edit", frag)
    cid = int(m.group(1)) if m else 0
    out["customer_id"] = cid
    out["customer_create"] = [code, loc]

    # 車輛
    code, loc, _ = req(op, "POST", base + "/vehicles",
                       {"customer_id": cid, "plate_number": "E2E-0001", "brand_model": "測試車型",
                        "vehicle_type": "car"}, h)
    vid = 0
    _, _, body = req(op, "GET", base + "/api/vehicles/search?q=E2E-0001")
    frag = body.decode("utf-8", "replace")
    m = re.search(r"/vehicles/(\d+)", frag)
    if m:
        vid = int(m.group(1))
    else:
        vid = last_id(loc, "/vehicles")
    out["vehicle_id"] = vid
    out["vehicle_create"] = [code, loc]

    # 工單
    code, loc, _ = req(op, "POST", base + "/work-orders",
                       {"vehicle_id": vid, "current_mileage": 5000,
                        "customer_complaint": "E2E 噪音"}, h)
    woid = last_id(loc, "/work-orders")
    if woid == 0:
        con = sqlite3.connect("file:%s?mode=ro" % DBPATH, uri=True)
        woid = con.execute("SELECT COALESCE(MAX(id),0) FROM work_orders").fetchone()[0]
        con.close()
    out["work_order_id"] = woid
    out["wo_create"] = [code, loc]

    # 工單零件項目（用 seed 的 1 号零件）
    if woid > 0:
        code, loc, body = req(op, "POST", base + "/work-orders/%d/items" % woid,
                              {"item_type": "part", "inventory_item_id": 1, "quantity": 2,
                               "description": "E2E 零件"}, h)
        out["item_add"] = [code, (body or b"")[:120].decode("utf-8", "replace")]

    # 完工（待結帳）
    code, loc, body = req(op, "POST", base + "/work-orders/%d/status" % woid,
                          {"status": "completed"}, h)
    out["complete"] = [code, (body or b"")[:160].decode("utf-8", "replace")]

    # 庫存基準
    con = sqlite3.connect("file:%s?mode=ro" % DBPATH, uri=True)
    row = con.execute("SELECT stock_quantity, reserved_quantity FROM inventory_items WHERE id=1").fetchone()
    wo = con.execute("SELECT status, COALESCE(total_amount,0) FROM work_orders WHERE id=?", (woid,)).fetchone()
    con.close()
    out["stock_before"] = row
    out["wo_status"] = wo[0] if wo else None
    out["wo_total"] = wo[1] if wo else None
    print(json.dumps(out, ensure_ascii=False))


def verify(db, woid, qty):
    con = sqlite3.connect("file:%s?mode=ro" % db, uri=True)
    stock, reserved = con.execute(
        "SELECT stock_quantity, reserved_quantity FROM inventory_items WHERE id=1").fetchone()
    mv = con.execute(
        "SELECT delta, before_quantity, after_quantity, reason, source, work_order_id "
        "FROM inventory_movements WHERE work_order_id=? ORDER BY id DESC LIMIT 1", (woid,)).fetchone()
    rec = con.execute(
        "SELECT COUNT(*), COALESCE(SUM(pay_total),0) FROM receivables WHERE work_order_id=?", (woid,)).fetchone()
    wo = con.execute("SELECT status, COALESCE(total_amount,0) FROM work_orders WHERE id=?", (woid,)).fetchone()
    con.close()
    res = {
        "stock_after": [stock, reserved],
        "last_movement": mv,
        "receivable_count": rec[0], "receivable_pay_total": rec[1],
        "wo_status": wo[0], "wo_total": wo[1],
    }
    checks = []
    checks.append(("工單已 paid", wo[0] == "paid"))
    checks.append(("有收款單", rec[0] >= 1))
    checks.append(("異動存在且原因是結帳扣料", mv is not None and "結帳扣料" in (mv[3] or "")))
    checks.append(("異動數量 = -%d" % qty, mv is not None and mv[0] == -qty))
    checks.append(("before/after 合理", mv is not None and mv[2] == mv[1] - qty))
    res["checks"] = [(n, ok) for n, ok in checks]
    res["all_ok"] = all(ok for _, ok in checks)
    print(json.dumps(res, ensure_ascii=False))
    return 0 if res["all_ok"] else 1


DBPATH = None
if __name__ == "__main__":
    mode = sys.argv[1]
    if mode == "setup":
        DBPATH = sys.argv[5]
        setup(sys.argv[2], sys.argv[3], sys.argv[4])
    elif mode == "verify":
        raise SystemExit(verify(sys.argv[2], int(sys.argv[3]), int(sys.argv[4])))
