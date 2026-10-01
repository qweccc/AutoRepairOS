import re
raw = open('test_phase2.py','rb').read()
print('bytes=%d' % len(raw))
for i, line in enumerate(raw.split(b'\n'),1):
    for m in re.finditer(rb"[bB]['\"](?P<body>(?:[^'\\\\]|\\\\.)*)\"", line):
        body = m.group('body')
        try:
            b = body.decode('utf-8')
        except:
            b = '<UNDEC>'
        if any(ord(c) > 0x7f for c in b):
            print('line %d: %s' % (i, b))
