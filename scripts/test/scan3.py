# -*- coding: utf-8 -*-
import re

path = "C:/Users/fr/AutoRepairOS/scripts/test/test_phase2.py"
with open(path, "rb") as f:
    data = f.read()

# Find every b followed by a quote, extract the full literal, verify balance + utf-8
print("=== scanning b'...' and b\"...\" literals ===")
pos = 0
while True:
    idx = data.find(b"b'", pos)
    if idx < 0:
        break
    # find closing quote (handle escaped quotes)
    i = idx + 2
    while i < len(data) and data[i] != 0x27:  # 0x27 = '
        if data[i] == 0x5c:  # backslash
            i += 2
            continue
        i += 1
    if i >= len(data):
        print("LINE issue: UNTERMINATED b' at", idx)
        print("   RAW:", data[max(0,idx-30):idx+30])
        break
    literal = data[idx:i+1]
    inner = data[idx+2:i]
    try:
        inner.decode("utf-8")
        # print only if non-ascii
        if any(b > 0x7f for b in inner):
            print(f"OK non-ascii b' literal at {idx}: {inner!r}")
    except UnicodeDecodeError as e:
        print(f"BAD b' at {idx}: {e}")
        print("   RAW:", data[max(0,idx-30):idx+30])
    pos = i + 1

# also b"
pos = 0
while True:
    idx = data.find(b'b"', pos)
    if idx < 0:
        break
    i = idx + 2
    while i < len(data) and data[i] != 0x22:  # 0x22 = "
        if data[i] == 0x5c:
            i += 2
            continue
        i += 1
    if i >= len(data):
        print("LINE issue: UNTERMINATED b\" at", idx)
        print("   RAW:", data[max(0,idx-30):idx+30])
        break
    literal = data[idx:i+1]
    inner = data[idx+2:i]
    try:
        inner.decode("utf-8")
        if any(b > 0x7f for b in inner):
            print(f"OK non-ascii b\" literal at {idx}: {inner!r}")
    except UnicodeDecodeError as e:
        print(f"BAD b\" at {idx}: {e}")
        print("   RAW:", data[max(0,idx-30):idx+30])
    pos = i + 1

print("=== done ===")
