#!/usr/bin/env python3
"""
UX 順手度量測（可重跑）

用途：把「頁面操作起來順不順」變成可比較的數字——步驟數、選項數、欄位數、
按鈕名稱重複次數、列表是否有分頁。避免憑印象說「應該還好吧」。

用法：
    # 1. 先編譯並用「複本 DB」啟動服務（不要動正式 data/autorepair.db）
    cd C:/Users/fr/AutoRepairOS
    export PATH="$PATH:/c/Program Files/Go/bin"
    go build -o "$TMPDIR/ar_ux.exe" .
    cp data/autorepair.db "$TMPDIR/ux_copy.db"
    PORT=8092 DB_PATH="file:$TMPDIR/ux_copy.db?_busy_timeout=5000" "$TMPDIR/ar_ux.exe" &

    # 2. 量測
    python scripts/ux_probe.py --base http://localhost:8092 --out "$TMPDIR/ux"

輸出：stdout 表格 + 每個頁面的 HTML 快照（方便事後對照改了什麼）

為什麼要用複本 DB：直接對 data/autorepair.db 啟動，排程器會寫入
每日備份與通知，把正式資料弄髒。
"""

import argparse
import http.cookiejar
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request

PAGES = [
    ("/", "總覽"),
    ("/work-orders", "工單列表"),
    ("/work-orders/new", "開立工單"),
    ("/customers", "客戶"),
    ("/vehicles", "車輛"),
    ("/inventory", "庫存"),
    ("/suppliers", "廠商"),
    ("/staff", "員工"),
    ("/insurances", "投保"),
    ("/inspections", "定檢"),
    ("/maintenance-templates", "保養套餐"),
    ("/technicians/commissions", "技師佣金"),
    ("/expenses", "支出"),
    ("/reports", "報表"),
    ("/scheduled-tasks", "排程"),
    ("/insurance-companies", "保險公司"),
]


def make_opener():
    cj = http.cookiejar.CookieJar()
    op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))
    op.addheaders = [("User-Agent", "ux-probe")]
    return op


def fetch(op, base, path):
    try:
        r = op.open(base + path, timeout=20)
        return r.getcode(), r.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")
    except Exception as e:                       # noqa: BLE001
        return 0, str(e)


def login(op, base, user, pwd):
    code, _ = fetch(op, base, "/login")
    data = urllib.parse.urlencode({"username": user, "password": pwd}).encode()
    req = urllib.request.Request(base + "/login", data=data)
    try:
        r = op.open(req, timeout=20)
        return r.getcode()
    except urllib.error.HTTPError as e:
        return e.code
    except Exception:                            # noqa: BLE001
        return 0


def visible_text(html):
    t = re.sub(r"<script[\s\S]*?</script>", " ", html, flags=re.I)
    t = re.sub(r"<style[\s\S]*?</style>", " ", t, flags=re.I)
    t = re.sub(r"<[^>]+>", "\n", t)
    return [re.sub(r"\s+", " ", x).strip() for x in t.split("\n") if x.strip()]


def analyse(path, code, html):
    names = re.findall(r'name="([^"]+)"', html)
    forms = re.findall(r'<form[^>]*action="([^"]*)"', html, re.I)
    buttons = [re.sub(r"<[^>]+>", " ", m).strip()
               for m in re.findall(r"<button[^>]*>([\s\S]{0,80}?)</button>", html, re.I)]
    buttons = [re.sub(r"\s+", " ", b) for b in buttons if b.strip()]
    links = re.findall(r'<a[^>]*href="([^"]*)"', html, re.I)
    rows = len(re.findall(r"<tr\b", html, re.I))
    # 列表是否有分頁線索
    has_page = bool(re.search(r"[?&](page|offset|limit)=", html)) or "下一頁" in html
    # 按鈕名稱重複
    dup = {b: buttons.count(b) for b in set(buttons) if buttons.count(b) > 1}
    return {
        "path": path, "code": code, "bytes": len(html),
        "forms": forms, "fields": names, "required": len(re.findall(r"\brequired\b", html, re.I)),
        "buttons": buttons, "dup_buttons": dup,
        "rows": rows, "links": len(links), "paged": has_page,
    }


def main():
    ap = argparse.ArgumentParser(description="AutoRepairOS UX 順手度量測")
    ap.add_argument("--base", default="http://localhost:8092")
    ap.add_argument("--user", default="owner")
    ap.add_argument("--password", default="Owner001!")
    ap.add_argument("--out", default=None, help="HTML 快照輸出目錄")
    args = ap.parse_args()

    if args.out:
        os.makedirs(args.out, exist_ok=True)

    op = make_opener()
    code = login(op, args.base, args.user, args.password)
    print(f"登入 {args.user} → HTTP {code}")
    if code != 200:
        print("登入失敗，無法繼續。請確認 --base / --user / --password")
        sys.exit(1)

    print(f"\n{'頁面':<12}{'路徑':<26}{'HTTP':>5}{'大小':>9}{'表單':>5}"
          f"{'欄位':>5}{'必填':>5}{'列':>6}{'按鈕':>5}  分頁")
    print("-" * 96)
    results = []
    for path, label in PAGES:
        c, html = fetch(op, args.base, path)
        r = analyse(path, c, html)
        r["label"] = label
        results.append(r)
        if args.out:
            fn = os.path.join(args.out, "ux_" + (path.strip("/").replace("/", "_") or "home") + ".html")
            with open(fn, "w", encoding="utf-8") as f:
                f.write(html)
        print(f"{label:<12}{path:<26}{c:>5}{len(html):>9,}{len(r['forms']):>5}"
              f"{len(r['fields']):>5}{r['required']:>5}{r['rows']:>6}{len(r['buttons']):>5}"
              f"  {'有' if r['paged'] else '—'}")

    print("\n【重複的按鈕名稱】（使用者無法分辨哪個按鈕做什麼）")
    for r in results:
        if r["dup_buttons"]:
            print(f"   {r['label']:<12} {r['dup_buttons']}")

    print("\n【表單 action 一頁多個】（同一頁要分多次送出）")
    for r in results:
        if len(r["forms"]) > 1:
            print(f"   {r['label']:<12} {r['forms']}")

    print("\n【開立工單的欄位】（步驟數的來源）")
    for r in results:
        if r["path"] == "/work-orders/new":
            for n in r["fields"]:
                print("   ", n)

    print("\n【選項爆量的下拉】（錯誤預防風險）")
    for r in results:
        if r["path"] == "/work-orders/new":
            c, html = fetch(op, args.base, r["path"])
            for m in re.finditer(r"<select[^>]*name=\"([^\"]+)\"[^>]*>([\s\S]*?)</select>", html, re.I):
                opts = re.findall(r"<option[^>]*>([\s\S]{0,60}?)</option>", m.group(2), re.I)
                opts = [re.sub(r"<[^>]+>", " ", o).strip() for o in opts]
                if len(opts) > 10:
                    print(f"   {m.group(1)}: {len(opts)} 個選項")
                    print(f"      前 6 個: {opts[:6]}")
                    print(f"      後 3 個: {opts[-3:]}")


if __name__ == "__main__":
    main()
