import sys, http.cookiejar, urllib.request
try:
    sys.stdout.reconfigure(encoding='utf-8')
except Exception:
    pass
BASE = 'http://localhost:8090'
cj = http.cookiejar.CookieJar()
op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))
urllib.request.install_opener(op)

def req(path):
    try:
        with op.open(BASE + path, timeout=10) as resp:
            return resp.status, resp.read().decode('utf-8', 'replace')
    except Exception as e:
        return None, str(e)

st, _ = req('/login')
st, html = req('/technicians/commissions')
print('=== commissions status=%s ===' % st)
print(html)
