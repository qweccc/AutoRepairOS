import os, re, glob

pat = re.compile(rb"""[bB]\s*(['"])(.*?)\1""", re.DOTALL)
for p in sorted(glob.glob('*.py')):
    name = os.path.basename(p)
    raw = open(p, 'rb').read()
    print('=== %s (%d bytes) ===' % (name, len(raw)))
    hits = pat.findall(raw)
    print('  b-literals found:', len(hits))
    for i, (q, body) in enumerate(hits, 1):
        try:
            content = body.decode('utf-8')
        except UnicodeDecodeError as e:
            content = '<UNDECODABLE: %s>' % e
        print('  [%d] %s' % (i, content))
    print()
