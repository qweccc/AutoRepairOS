# -*- coding: utf-8 -*-
with open("test_phase2.py", "rb") as f:
    data = f.read()
lines = data.split(b"\n")
print("TOTAL:", len(lines))
for i in range(len(lines)):
    ln = lines[i]
    # find byte-literal strings like b'...' that contain non-ascii bytes (bad UTF-8)
    starts = [j for j in range(len(ln)) if ln.startswith(b"b'", j) or (ln[j:j+1] == b"b" and ln[j+1:j+2] == b"'")]
    # crude: check for b' sequences that include CJK bytes (0xe4-0xef)
    if any(0xe4 <= b <= 0xef for b in ln):
        # check if this line has a b'...' literal containing the cjk byte
        # split on b'
        segs = ln.split(b"'")
        for j, seg in enumerate(segs):
            if seg and any(0xe4 <= b <= 0xef for b in seg) and (j % 2 == 1):
                print("SUSPECT b' literal at line", i+1, "=>", ln)
        # also just show the line
        print("LINE", i+1, "=>", ln)
