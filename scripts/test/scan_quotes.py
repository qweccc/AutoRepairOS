# -*- coding: utf-8 -*-
import re
with open("test_phase2.py", "rb") as f:
    data = f.read()
lines = data.split(b"\n")

for i, ln in enumerate(lines):
    # b'...'
    for m in re.finditer(rb"b'(?P<body>(?:[^'\\]|\\.)*)'", ln):
        body = m.group("body")
        if b"'" in body and b"\\'" not in body:
            print("LINE", i+1, "MISMATCH b' literal:", ln)
    # b"..."
    for m in re.finditer(rb'b"(?P<body>(?:[^"\\]|\\.)*)"', ln):
        body = m.group("body")
        if b'"' in body and b'\\"' not in body:
            print("LINE", i+1, "MISMATCH b\" literal:", ln)

print("=== scanning complete, no mismatches if nothing above ===")
