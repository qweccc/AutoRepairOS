import sys, http.cookiejar, urllib.request, urllib.parse
try:
    sys.stdout.reconfigure(encoding='utf-8')
except Exception:
    pass
BASE = 'http://localhost:8090'
cj = http.cookiejar.CookieJar()
op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))
urllib.request.install_opener(op)

def req(op, path, data=None):
    body = urllib.parse.urlencode(data, encoding='utf-8').encode('utf-8') if data else None
    r = urllib.request.Request(BASE + path, data=body)
    try:
        with op.open(r, timeout=10) as resp:
            return resp.status, resp.read().decode('utf-8', 'replace')
    except Exception as e:
        return None, str(e)

st, _ = req(op, '/login', {'username': 'counter', 'password': 'Counter001!'})
print('login status=%s' % st)
st, html = req(op, '/technicians/commissions')
print('=== commissions status=%s ===' % st)
print(html)
