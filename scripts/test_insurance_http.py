import urllib.request, urllib.parse, http.cookiejar, re, sys, time
sys.stdout.reconfigure(encoding="utf-8", errors="replace")

BASE = "http://localhost:8091"
results = []

def check(name, cond, detail=""):
    results.append(cond)
    print(f"[{'PASS' if cond else 'FAIL'}] {name} {detail}")

def sess(user, pwd):
    cj = http.cookiejar.CookieJar()
    op = urllib.request.build_opener(
        urllib.request.HTTPCookieProcessor(cj),
        urllib.request.HTTPRedirectHandler())
    body = urllib.parse.urlencode({"username": user, "password": pwd}).encode()
    op.open(urllib.request.Request(BASE + "/login", data=body))
    return op

def req(op, path, data=None):
    body = urllib.parse.urlencode(data, encoding="utf-8").encode("utf-8") if data else None
    # URL 必須自行編碼：QueryString 有中文時直接組會觸發 ascii 編碼錯誤
    full = BASE + urllib.parse.quote(path, safe="/?=&%")
    r = urllib.request.Request(full, data=body)
    try:
        resp = op.open(r)
        return resp.status, resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")

print("=" * 58)
print("保險公司頁面 — 真實 HTTP 驗證 (8091)")
print("=" * 58)

owner = sess("owner", "Owner001!")
tech = sess("tech", "Tech001!")

# 1. 列表頁
st, html = req(owner, "/insurance-companies")
check("列表頁載入", st == 200, f"HTTP {st}, {len(html)} bytes")
check("有新增表單", "新增保險公司" in html)
check("有搜尋框", "搜尋簡稱" in html or 'name="q"' in html)
check("有客服電話欄位", "客服電話" in html)
check("有統編欄位", "統一編號" in html or "統編" in html)

# 2. 側邊欄入口
check("側邊欄有保險公司", 'href="/insurance-companies"' in html)

# 3. 建立
# 用時間戳避免重跑時撞到已存在的簡稱
CODE = "國泰" + str(int(time.time()))[-4:]
st, html = req(owner, "/insurance-companies", {
    "code": {CODE}.format(CODE=CODE) if False else CODE,
    "name": "國泰世紀產物保險股份有限公司",
    "tax_id": "28817468", "claim_phone": "0800-024-888",
    "contact_person": "王經理",
})
check("建立保險公司", "已新增" in html or st == 200, f"HTTP {st} code={CODE}")

st, html = req(owner, "/insurance-companies")
check("列表顯示新公司", CODE in html and "28817468" in html)
check("顯示客服電話", "0800-024-888" in html)

# 4. 重複簡稱被拒
st, html = req(owner, "/insurance-companies", {"code": CODE, "name": "重複"})
check("重複簡稱被拒", "已存在" in html or st != 200, f"HTTP {st}")

# 5. 必填驗證
st, html = req(owner, "/insurance-companies", {"code": "", "name": "沒簡稱"})
check("空簡稱被拒", "必填" in html or st != 200)

# 6. 搜尋
st, html = req(owner, "/insurance-companies?q=" + CODE)
check("搜尋找到", "國泰" in html)
st, html = req(owner, "/insurance-companies?q=NOSUCH")
check("搜尋無結果時不顯示", (">" + CODE + "<") not in html)

# 7. 編輯
ids = re.findall(r'/insurance-companies/(\d+)/edit', html or "")
st, allhtml = req(owner, "/insurance-companies")
cid = re.findall(r'/insurance-companies/(\d+)/edit', allhtml)[0]
st, eh = req(owner, f"/insurance-companies/{cid}/edit")
check("編輯頁載入", st == 200)
check("編輯頁預填統編", "28817468" in eh)
check("編輯頁預填客服電話", "0800-024-888" in eh)

st, html = req(owner, f"/insurance-companies/{cid}", {
    "code": CODE, "name": "國泰產險",
    "tax_id": "28817468", "claim_phone": "0800-024-999",
})
check("更新成功", "已更新" in html or st == 200)

st, html = req(owner, "/insurance-companies")
check("更新已生效", "國泰產險" in html)
check("舊名已消失", "國泰世紀產物保險股份有限公司" not in html)

# 8. 權限：技師不能建立
st, html = req(tech, "/insurance-companies", {"code": "TechX", "name": "技師測試"})
check("技師建立被拒", "權限不足" in html or st == 403, f"HTTP {st}")

# 技師可讀
st, html = req(tech, "/insurance-companies")
check("技師可讀取列表", st == 200 and "國泰產險" in html)

# 9. 停用（軟刪除）
st, html = req(owner, f"/insurance-companies/{cid}/delete")
check("停用成功", "已停用" in html or st == 200)

st, html = req(owner, "/insurance-companies")
check("停用後仍可查到（歷史保留）", "國泰產險" in html)
check("顯示停用狀態", "停用" in html)

print("\n" + "=" * 58)
p = sum(results)
print(f"PASS={p} FAIL={len(results)-p}")