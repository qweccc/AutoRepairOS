import sys
try:
    sys.stdout.reconfigure(encoding='utf-8')
except Exception:
    pass
p = 'out.txt'
try:
    raw = open(p, 'r', encoding='utf-8').read()
    print('=== out.txt (bytes=%d) ===' % len(raw))
    print(raw)
except FileNotFoundError:
    print('out.txt NOT FOUND')
except Exception as e:
    print('ERROR: %s' % e)
