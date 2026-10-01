# -*- coding: utf-8 -*-
with open("test_phase2.py", "rb") as f:
    data = f.read()
lines = data.split(b"\n")
print("TOTAL LINES:", len(lines))
for i, ln in enumerate(lines):
    print(i+1, "=>", ln)
