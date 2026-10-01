# -*- coding: utf-8 -*-
with open("test_phase2.py", "rb") as f:
    data = f.read()
lines = data.split(b"\n")

# For each line, find b'...' and b"..." and report full raw content
for i, ln in enumerate(lines):
    # find all quote-delimited regions starting with b
    pos = 0
    while True:
        idx = ln.find(b"b'", pos)
        if idx < 0:
            break
        # find closing b'
        end = ln.find(b"'", idx + 1)
        if end < 0:
            print("LINE", i+1, "UNTERMINATED b' starting at", idx)
            print("   RAW:", ln)
            break
        content = ln[idx:end+1]
        # decode the content inside (between b and ')
        inner = ln[idx+1:end]
        try:
            inner.decode("utf-8")
        except UnicodeDecodeError as e:
            print("LINE", i+1, "BAD UTF-8 inside b' literal:", e)
            print("   RAW:", ln)
        pos = end + 1

print("=== check b\" literals ===")
for i, ln in enumerate(lines):
    pos = 0
    while True:
        idx = ln.find(b'b"', pos)
        if idx < 0:
            break
        end = ln.find(b'"', idx + 1)
        if end < 0:
            print("LINE", i+1, "UNTERMINATED b\" starting at", idx)
            print("   RAW:", ln)
            break
        inner = ln[idx+1:end]
        try:
            inner.decode("utf-8")
        except UnicodeDecodeError as e:
            print("LINE", i+1, "BAD UTF-8 inside b\" literal:", e)
            print("   RAW:", ln)
        pos = end + 1

print("=== done ===")
