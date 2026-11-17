#!/usr/bin/env python3
"""語系網格複測：角色 × 頁面 × {zh-TW, en}，檢查真實渲染出的 HTML。

與靜態字典比對（locales/*.json）互補：字典對齊不代表畫面對——這裡看的是
伺服器真的吐出來給瀏覽器的字。

檢查項：
  A. 殘留原文：英文頁面出現中日韓字元（可能是未走 i18n 的 hardcoded 字串，
     或根本沒翻譯的資料值；本腳本只負責列出来，人工判定）。
  B. key 漏譯：畫面文字裡直接出現 locales 的 key（i18n.T 找不到時原樣回傳）。
  C. 語系切換生效：同一頁在 zh-TW / en 下的導覽標籤必須不同，且各自對。

用法：先起好 8099（或改 BASE），再 `python3 locale_grid.py`。
"""
import html
import http.cookiejar
import json
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
from html.parser import HTMLParser

BASE = "http://127.0.0.1:8099"
ROOT = r"C:/Users/fr/AutoRepairOS"
LANGS = ["zh-TW", "en"]

# 與 role_route_matrix.py 同一份頁面清單
PAGES = [
    "/", "/customers", "/vehicles", "/work-orders", "/estimates", "/appointments",
    "/receivables", "/inspections", "/purchase-orders", "/suppliers", "/inventory",
    "/maintenance-templates", "/sop-templates", "/insurances", "/insurance-companies",
    "/staff", "/technicians/commissions", "/expenses", "/reports", "/analytics",
    "/ap", "/lookup", "/scheduled-tasks", "/subcontracts", "/equipment", "/phrases",
]

# 帳密由環境提供（與正式部署一致，工具本身不內建任何密碼）：
# owner/counter/tech 都讀 repo 根目錄的 .env；沒有的角色就跳過。
CREDENTIALS = {
    "owner": ("owner", None),
    "counter": ("counter", None),
    "technician": ("tech", None),
}


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class TextCollector(HTMLParser):
    """收集可視文字：跳過 script/style，保留層級以便還原脈絡。"""

    SKIP = {"script", "style", "template", "noscript"}

    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.parts = []
        self.stack = []

    def handle_starttag(self, tag, attrs):
        if tag in self.SKIP:
            self.stack.append(tag)

    def handle_endtag(self, tag):
        if self.stack and self.stack[-1] == tag:
            self.stack.pop()

    def handle_data(self, data):
        if self.stack:
            return
        t = data.strip()
        if t:
            self.parts.append(t)


def load_keys():
    zh = json.load(open(f"{ROOT}/locales/zh-TW.json", encoding="utf-8"))
    en = json.load(open(f"{ROOT}/locales/en.json", encoding="utf-8"))
    return set(zh), set(en), zh, en


def build_opener(lang):
    cj = http.cookiejar.CookieJar()
    opener = urllib.request.build_opener(
        urllib.request.HTTPCookieProcessor(cj), NoRedirect
    )
    cj.set_cookie(
        http.cookiejar.Cookie(
            0, "lang", lang, None, False, "127.0.0.1", False, False,
            "/", False, False, None, False, None, None, {},
        )
    )
    return opener, cj


def post(opener, path, data=None):
    body = urllib.parse.urlencode(data or {}).encode()
    r = urllib.request.Request(
        BASE + path, data=body, method="POST",
        headers={"Content-Type": "application/x-www-form-urlencoded"},
    )
    try:
        resp = opener.open(r, timeout=20)
        return resp.getcode(), resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, ""


def get(opener, path):
    try:
        resp = opener.open(BASE + path, timeout=20)
        return resp.getcode(), resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, ""
    except OSError as e:
        return -1, str(e)


def visible_text(page_html):
    p = TextCollector()
    p.feed(page_html)
    return p.parts


def has_cjk(s):
    return any("\u3400" <= c <= "\u9fff" or "\uf900" <= c <= "\ufaff" for c in s)


def main():
    keys, _, zh, en = load_keys()
    creds = dict(CREDENTIALS)
    env_path = f"{ROOT}/.env"
    try:
        for line in open(env_path, encoding="utf-8"):
            line = line.strip()
            if line.startswith("DEFAULT_OWNER_USERNAME="):
                creds["owner"] = (line.split("=", 1)[1], creds["owner"][1])
            elif line.startswith("DEFAULT_OWNER_PASSWORD="):
                creds["owner"] = (creds["owner"][0], line.split("=", 1)[1])
            elif line.startswith("DEFAULT_COUNTER_PASSWORD="):
                pw = line.split("=", 1)[1]
                creds["counter"] = ("counter", pw or None)
            elif line.startswith("DEFAULT_TECH_PASSWORD="):
                pw = line.split("=", 1)[1]
                creds["technician"] = ("tech", pw or None)
    except OSError:
        pass
    creds = {k: v for k, v in creds.items() if v[1]}

    problems = []          # (kind, role, lang, page, sample)
    coverage = {}
    page_langs_text = {}   # page -> {lang: 前 5 個文字}

    for role, (user, pw) in creds.items():
        for lang in LANGS:
            opener, _ = build_opener(lang)
            code, _ = post(opener, "/login", {"username": user, "password": pw or ""})
            if code not in (301, 302, 303, 307, 308):
                print(f"[warn] {role}/{lang} 登入失敗 code={code}", file=sys.stderr)
                continue
            for page in PAGES:
                status, body = get(opener, page)
                if status != 200:
                    continue
                coverage.setdefault(page, {})[lang] = True
                texts = visible_text(body)
                joined = " | ".join(texts)
                page_langs_text.setdefault(page, {})[lang] = texts[:6]
                # A. 英文頁残留 CJK
                if lang == "en":
                    seen = set()
                    for t in texts:
                        if has_cjk(t) and t not in seen:
                            seen.add(t)
                            problems.append(("residual-cjk", role, lang, page, t))
                # B. 任何語系漏出字典 key（i18n.T 原樣回傳的徵狀）
                for tok in re.findall(r"[a-z][a-z0-9_]*(?:\.[a-z0-9_]+)+", joined):
                    if tok in keys:
                        problems.append(("raw-key", role, lang, page, tok))

    print("=== 覆蓋 ===")
    missing = [p for p in PAGES if len(coverage.get(p, {})) < len(LANGS)]
    print(f"页面 {len(PAGES)}、每页應有 {len(LANGS)} 語系；缺語系的頁面: {missing or '無'}")

    print("\n=== C. 語系切換抽樣（首 6 個可視文字）===")
    same = []
    for page in PAGES:
        both = page_langs_text.get(page, {})
        if len(both) != 2:
            continue
        zt, en_t = both["zh-TW"], both["en"]
        if zt == en_t:
            same.append(page)
    print(f"兩語前 6 個文字完全相同的頁面: {same or '無'}")

    print("\n=== A/B 問題 ===")
    by_kind = {}
    for kind, role, lang, page, sample in problems:
        by_kind.setdefault(kind, []).append((role, lang, page, sample))
    for kind, items in sorted(by_kind.items()):
        uniq = sorted({(page, s) for _, _, page, s in items})
        print(f"\n[{kind}] {len(items)} 筆，相異 {len(uniq)}：")
        for page, s in uniq[:40]:
            print(f"   {page}: {s[:70]}")
    if not problems:
        print("無")

    out = f"{ROOT}/unpublished/analysis-and-tests/locale_grid_result.txt"
    with open(out, "w", encoding="utf-8") as f:
        for kind, role, lang, page, sample in problems:
            f.write(f"{kind}\t{role}\t{lang}\t{page}\t{sample}\n")
    print(f"\n明細寫入 {out}")


if __name__ == "__main__":
    main()
