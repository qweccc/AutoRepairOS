import urllib.request, urllib.parse, http.cookiejar, re, sys

BASE = "http://localhost:8090"
cj = http.cookiejar.CookieJar()
op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))

def req(path, method="GET", data=None):
    r = urllib.request.Request(BASE + path, method=method, data=data)
    if data is not None:
        r.add_header("Content-Type", "application/x-www-form-urlencoded")
    try:
        resp = op.open(r, timeout=5)
        return resp.status, resp.read().decode("utf-8", "replace"), resp.geturl(), resp.headers.get("Set-Cookie")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace"), e.url, e.headers.get("Set-Cookie")

creds = "username=owner&password=Owner001!".encode("utf-8")
print("=== LOGIN (owner) ===")
st, body, u, sc = req("/login", "POST", creds)
print("POST /login ->", st, "redirect:", u, "setcookie:", sc)
print("has session_token:", any(c.name == "session_token" for c in cj))
if not any(c.name == "session_token" for c in cj):
    print("!!! LOGIN FAILED - cookie not created, checking response body:")
    print(body[:500])
    sys.exit(1)

print("\n=== 8 PAGES (with session) ===")
results = {}
for p in ["/", "/customers", "/vehicles", "/work-orders", "/reports", "/scheduled-tasks"]:
    st, body, u, sc = req(p)
    real = bool(re.search(r'<h2[^>]*>([^<]{2,})</h2>', body))
    data = bool(re.search(r'(避震|Toyota|機油|陳|王|李|張|owner|ST-|pending|completed|總計|name|phone|plate|id=|避震器|待辦|客戶|車輛|工單|報表|排程)', body))
    # extract real table data
    names = re.findall(r'<td[^>]*>([^<]{1,15})</td>', body)
    results[p] = (st, len(body), real, data, u)
    print(f"{p:20} {st} len={len(body):5} real={real} data={data}")

print("\n=== REAL CONTENT SNIPPETS (with session) ===")
for p, res in results.items():
    st, ln, real, data, u = res
    if ln > 1600:
        print(f"\n--- {p} (len={ln}) ---")
        print(body[:2000])
