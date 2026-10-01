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
# work order detail
st, html = req(op, '/work-orders/1')
print('=== work-orders/1 status=%s ===' % st)
# extract commission-related
import re
for m in re.finditer(r'(工單|工資|佣金|labor|salary|commission|total|工時)[^<]{0,60}</td>', html):
    print('  ', m.group(0))

# commissions by month 2026-09
st, hsep = req(op, '/technicians/commissions')
print('sep status=%s' % st)
st, html2 = req(op, '/technicians/commissions?month=2026-09')
print('=== commissions 2026-09 status=%s ===' % st)
for m in re.finditer(r'<th[^>]*>總佣金</th><td[^>]*>([0-9]+)</td>', html2):
    print('  總佣金=', m.group(1))
for m in re.finditer(r'<th[^>]*>工單數</th><td[^>]*>([0-9]+)</td>', html2):
    print('  工單數=', m.group(1))
for m in re.finditer(r'<th[^>]*>總工時</th><td[^>]*>([^<]+)</td>', html2):
    print('  總工時=', m.group(1))
