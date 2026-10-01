# -*- coding: utf-8 -*-
import sys, re, ast

paths = sys.argv[1:] if len(sys.argv) > 1 else ["scripts/test/test_phase2.py", "scripts/test_phase2.py"]
for p in paths:
    print("=== FILE:", p, "===")
    try:
        with open(p, "rb") as f:
            data = f.read()
    except Exception as e:
        print("ERR read:", e); continue

    # 1. AST parse of utf-8 decoded
    try:
        text = data.decode("utf-8")
        ast.parse(text)
        print("AST parse: OK")
    except SyntaxError as e:
        print("AST parse: SYNTAX ERROR line", e.lineno)
        print("   text:", repr(text.splitlines()[e.lineno-1] if e.lineno else ""))
        continue

    # 2. scan every b'...' / b"..." literal for bad utf-8
    bad = 0
    for m in re.finditer(rb"b[\"'](?P<b>(?:[^\"'\\]|\\.)*)[\"']", data):
        body = m.group("b")
        try:
            body.decode("utf-8")
        except UnicodeDecodeError as e:
            print("BAD literal:", body, "ERR", e)
            bad += 1

    print("bad literals:", bad)
    if not bad:
        print("OK all bytes literals decode")
