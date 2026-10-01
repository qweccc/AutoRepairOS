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

# hit add worklog endpoint (POST)
data = {
    'technician_id': '3',
    'work_date': '2026-10-01',
    'hours': '4',
    'hourly_rate': '300',
    'commission_rate': '15',
    'notes': 'hit test new row'
}
st, resp = req(op, '/work-orders/1/worklogs', data)
print('POST worklogs status=%s' % st)
print('RESP BODY:')
print(resp)

# re-fetch page to see DB state
st, html = req(op, '/work-orders/1/worklogs')
print('=== page status=%s total=%d ===' % (st, len(html)))
m = re.findall(r'工時明細[^<]*<br/>|日期[^<]*技師[^<]*工時[^<]*時薪', html)
print('section found')
for m in re.finditer(r'>(\d+(?:\.\d+)?)<', html):
    pass
