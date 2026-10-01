import sys
try:
    sys.stdout.reconfigure(encoding='utf-8')
except Exception:
    pass
BASE = 'http://localhost:8090'
import urllib.request, urllib.parse

def req(path):
    try:
        r = urllib.request.Request(BASE + path)
        with urllib.request.urlopen(r, timeout=10) as resp:
            return resp.status, resp.read().decode('utf-8', 'replace')
    except Exception as e:
        return None, str(e)

st, html = req('/technicians/commissions')
print('=== commissions page status=%s ===' % st)
print(html)
