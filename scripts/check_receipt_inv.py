import urllib.request, urllib.parse, re, uuid

BASE="http://localhost:8090"

def login(un,pw):
    data=urllib.parse.urlencode({"username":un,"password":pw}).encode("utf-8")
    req=urllib.request.Request(BASE+"/login",method="POST",data=data)
    req.add_header("Content-Type","application/x-www-form-urlencoded; charset=utf-8")
    try:
        resp=urllib.request.urlopen(req,timeout=8)
        tok=_extract(resp.getheader("Set-Cookie"))
        return resp.status,resp.read().decode("utf-8","replace"),tok
    except urllib.error.HTTPError as e:
        return e.code,e.read().decode("utf-8","replace"),""

def _extract(hdr):
    if not hdr: return ""
    for p in hdr.split(";"):
        if p.startswith("session_token="):
            return p.split("=",1)[1].strip()
    return ""

def get(path,cookie=None):
    r=urllib.request.Request(BASE+path)
    if cookie: r.add_header("Cookie","session_token="+cookie)
    try:
        resp=urllib.request.urlopen(r,timeout=8); return resp.status,resp.read().decode("utf-8","replace")
    except urllib.error.HTTPError as e:
        return e.code,e.read().decode("utf-8","replace")

print("=== [A] 庫存頁 /inventory (owner) ===")
st,b,u=login("owner","Owner001!")
print("  status:",st,"| len:",len(b))
rows=len(re.findall(r'href="/inventory/\d+"',b))
print("  零件行數:",rows)
print("  含實際零件名:", any(k in b for k in ["機油","螺絲","輪胎","煞車","方向","冷卻","電池","濾心","保險桿","火花塞"]))

print("\n=== [B] 線上收據 /work-orders/{id}/receipt (owner) ===")
st,b,u=login("owner","Owner001!")
ids=re.findall(r'/work-orders/(\d+)"',b)
if not ids:
    m=re.search(r'/work-orders/(\d+)(?:/|$)',b); ids=[int(m.group(1))] if m else []
rid=None
for cand in sorted(set(ids), key=int, reverse=True):
    cid=int(cand)
    s2,rb,_=get(f"/work-orders/{cid}/receipt",cookie=u)
    if s2==200 and len(rb)>400:
        rid=cid; print("  收據工單 #",cid,"status:",s2); break
if rid is None:
    rid=ids[0] if ids else None
print("  使用工單 #",rid,"| len:",len(b) if rid else 0)
if rid:
    print("  含客戶名:", bool(re.search(r'陳[\u4e00-\u9fff]{2,}', b)) or "客" in b)
    items=len(re.findall(r'item_type="part"|"labor"|labor|part', b))
    print("  項目標記數:", items)
    totals=re.findall(r'NT\$?\s?\d{2,5}[\s,]?\d{2,4}|總計|VAT|稅|折扣|小計', b)
    print("  金額/稅/折扣相關:", totals[:10])
    print("  len:",len(b))

print("\nDONE")
