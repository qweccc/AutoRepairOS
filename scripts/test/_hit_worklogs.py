import sys, http.cookiejar, urllib.request, urllib.parse, json
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

# login
st, _ = req(op, '/login', {'username': 'counter', 'password': 'Counter001!'})
print('login=%s' % st)

# work hours page
st, html = req(op, '/work-orders/1/worklogs')
print('=== worklogs page status=%s ===' % st)
import re
for m in re.finditer(r'>([^<]{0,50})<', html):
    print('  ', m.group(1))
print('--- total bytes=%d ---' % len(html))
