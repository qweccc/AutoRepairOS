# -*- coding: utf-8 -*-
import ast
with open("test_phase2.py", "rb") as f:
    data = f.read()
lines = data.split(b"\n")

# Try ast parse the decoded content
try:
    text = data.decode("utf-8")
    ast.parse(text)
    print("AST PARSE OK")
except SyntaxError as e:
    print("SYNTAX ERROR:", e)
    print("  line:", e.lineno)
    print("  text:", repr(text.splitlines()[e.lineno-1] if e.lineno else ""))

# Walk AST for constant bytes literals with bad utf-8
tree = ast.parse(text)
bad = []
for node in ast.walk(tree):
    if isinstance(node, ast.Constant) and isinstance(node.value, bytes):
        try:
            node.value.decode("utf-8")
        except UnicodeDecodeError as e:
            bad.append((node.lineno, node.value))
for ln, bv in bad:
    print("BAD BYTES LITERAL line", ln, "=>", bv)
if not bad:
    print("ALL BYTES LITERALS DECODE OK")
