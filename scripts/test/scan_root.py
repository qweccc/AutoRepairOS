# -*- coding: utf-8 -*-
import re

path = "C:/Users/fr/AutoRepairOS/scripts/test_phase2.py"
with open(path, "rb") as f:
    data = f.read()

print("FILE:", path, "SIZE:", len(data))
print("FIRST 3 bytes:", data[:3].hex())
print("Has utf-8 BOM:", data.startswith(b'\xef\xbb\xbf'))

# Check for mixed encodings: find any byte sequence that is not valid utf-8
lines = data.split(b"\n")
print("TOTAL LINES:", len(lines))

# For each line, try to find b'...' / b"..." and also check for cp1252 mojibake
for i, ln in enumerate(lines):
    # Check if line has non-utf8 bytes
    try:
        ln.decode("utf-8")
    except UnicodeDecodeError as e:
        print("LINE", i+1, "NON-UTF8:", e)
        print("   RAW:", ln)
        continue

# Now scan for broken literals using a robust state machine
print("=== robust literal scan ===")
def scan_literals(data, qopen, qclose):
    results = []
    pos = 0
    while True:
        idx = data.find(qopen, pos)
        if idx < 0:
            break
        # find closing, respecting escapes
        i = idx + len(qopen)
        while i < len(data):
            if data[i] == ord(qclose):
                break
            if data[i] == 0x5c:  # backslash
                i += 2
                continue
            i += 1
        if i >= len(data):
            results.append(("UNTERMINATED", idx, data[max(0,idx-10):idx+20]))
            break
        inner = data[idx+len(qopen):i]
        try:
            inner.decode("utf-8")
        except UnicodeDecodeError as e:
            results.append(("BAD_UTF8", idx, data[max(0,idx-10):idx+40], e))
        results.append(("OK", idx, data[max(0,idx-10):idx+40]))
        pos = i + 1
    return results

for q in [b"'", b'"']:
    print("--- quote:", repr(q), "---")
    for r in scan_literals(data, b"b"+q, q):
        kind = r[0]
        if kind != "OK":
            print("  ", kind, "at", r[1], "RAW:", r[2])
        else:
            seg = r[2]
            # only print lines with non-ascii
            if any(b > 0x7f for b in seg) and r[1] % 1 != 0:
                pass
        # print full line for non-ascii context
        line_no = data.count(b"\n", 0, r[1]) + 1
        print("   line", line_no, ":", seg)
