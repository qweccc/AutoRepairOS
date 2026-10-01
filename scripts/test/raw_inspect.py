# -*- coding: utf-8 -*-
with open("test_phase2.py", "rb") as f:
    data = f.read()

print("FILE SIZE:", len(data))
print("FIRST 3 bytes (BOM?):", data[:3].hex())
print("Has utf-8 BOM:", data.startswith(b'\xef\xbb\xbf'))
print("Has cp1252 BOM:", data.startswith(b'\xff\xfe'))

# count non-ascii bytes
nonascii = sum(1 for b in data if b > 0x7f)
print("Non-ascii bytes:", nonascii)

# Find all b'...' and b"..." literals and report each raw
import re
print("=== all b'...' literals ===")
for m in re.finditer(rb"b'(?P<b>(?:[^'\\]|\\.)*)'", data):
    body = m.group("b")
    # decode inner
    try:
        body.decode("utf-8")
        print("OK:", body)
    except UnicodeDecodeError as e:
        print("BAD:", body, "ERR", e)

print("=== all b\"...\" literals ===")
for m in re.finditer(rb'b"(?P<b>(?:[^"\\]|\\.)*)"', data):
    body = m.group("b")
    try:
        body.decode("utf-8")
        print("OK:", body)
    except UnicodeDecodeError as e:
        print("BAD:", body, "ERR", e)
