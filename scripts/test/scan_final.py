# -*- coding: utf-8 -*-
with open("C:/Users/fr/AutoRepairOS/scripts/test/test_phase2.py", "rb") as f:
    data = f.read()

lines = data.split(b"\n")
for i, ln in enumerate(lines):
    # find every b'...' and b"..." literal in this line
    pos = 0
    while True:
        idx = ln.find(b"b'", pos)
        if idx < 0:
            break
        # scan for closing quote respecting escapes
        j = idx + 2
        while j < len(ln):
            if ln[j] == 0x27:  # '
                break
            if ln[j] == 0x5c:  # backslash
                j += 2
                continue
            j += 1
        if j >= len(ln):
            print("UNTERMINATED b' at line", i+1)
            print("  RAW:", ln)
            break
        inner = ln[idx+2:j]
        # decode inner utf-8
        try:
            inner.decode("utf-8")
        except UnicodeDecodeError as e:
            print("BAD b' at line", i+1, ":", e)
            print("  RAW:", ln)
        pos = j + 1

for i, ln in enumerate(lines):
    pos = 0
    while True:
        idx = ln.find(b'b"', pos)
        if idx < 0:
            break
        j = idx + 2
        while j < len(ln):
            if ln[j] == 0x22:  # "
                break
            if ln[j] == 0x5c:
                j += 2
                continue
            j += 1
        if j >= len(ln):
            print("UNTERMINATED b\" at line", i+1)
            print("  RAW:", ln)
            break
        inner = ln[idx+2:j]
        try:
            inner.decode("utf-8")
        except UnicodeDecodeError as e:
            print("BAD b\" at line", i+1, ":", e)
            print("  RAW:", ln)
        pos = j + 1

print("=== done scanning ===")
