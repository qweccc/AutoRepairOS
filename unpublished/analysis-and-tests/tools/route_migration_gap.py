#!/usr/bin/env python3
"""產生「路由遷移漏掛」的回復清單：列出遷移前 main.go 有、現在 routes.go 沒有，
且 handler 仍存在、UI/JS/字串頁仍有引用的注册列（可直接貼回 routes.go）。"""
import re
import subprocess
import pathlib

ROOT = pathlib.Path("C:/Users/fr/AutoRepairOS")
OLD_SRC = subprocess.run(["git", "show", "35195c1^:main.go"], cwd=ROOT,
                         capture_output=True, text=True, encoding="utf-8").stdout
NEW = (ROOT / "internal/routes/routes.go").read_text(encoding="utf-8")

line_pat = re.compile(r'^\s*r\.(Get|Post)\("([^"]+)",\s*(.+)\)\s*$')
old_lines = []
for line in OLD_SRC.splitlines():
    m = line_pat.match(line.strip())
    if m:
        old_lines.append((m.group(1), m.group(2), line.strip()))

cur = set(re.findall(r'r\.(Get|Post)\("([^"]+)"', NEW))
missing = [(m, p, l) for (m, p, l) in old_lines if (m, p) not in cur]

handler_src = "\n".join(p.read_text(encoding="utf-8", errors="replace")
                        for p in (ROOT / "internal/handler").glob("*.go")
                        if not p.name.endswith("_test.go"))
ref_src = []
for pat in ("web/**/*.templ", "internal/handler/static/*"):
    for p in ROOT.glob(pat):
        if p.is_file():
            try:
                ref_src.append(p.read_text(encoding="utf-8", errors="replace"))
            except Exception:
                pass
for p in (ROOT / "internal/handler").glob("*.go"):
    if not p.name.endswith("_test.go"):
        ref_src.append(p.read_text(encoding="utf-8", errors="replace"))
blob = "\n".join(ref_src)

pick = []
skip = []
for m, p, l in missing:
    mm = re.search(r'handler\.([A-Za-z]+)\(', l)
    wrapper = mm.group(1) if mm else "?"
    exists = ("func %s(" % wrapper) in handler_src or re.search(r'func %s\(' % wrapper, handler_src)
    refs = blob.count(p)
    if not exists:
        skip.append((m, p, "handler不在", l))
        continue
    if p == "/index.html":
        skip.append((m, p, "legacy 靜態頁，略過", l))
        continue
    if refs == 0:
        skip.append((m, p, "無引用", l))
        continue
    pick.append((m, p, refs, l))

print("## 建議補回（handler 在 + 有引用）%d 條" % len(pick))
for m, p, r, l in pick:
    print(l)
print("\n## 略過 %d 條" % len(skip))
for m, p, why, l in skip:
    print("  %-40s %s" % (p, why))
