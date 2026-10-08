"""從 CAR35 的 Crystal Report (.rpt) 與 Access DB (.mdb) 抽出功能與資料表結構。

.rpt 是 Crystal Reports 的二進位格式，但表頭區塊通常可讀到欄位名，
我們藉此反推 CAR35 的資料模型，作為 AutoRepair OS 的功能對照基準。
"""
import os, re, sys, glob
sys.stdout.reconfigure(encoding="utf-8", errors="replace")

BASE = r"C:\Users\fr\35-20231008"

# ---------------------------------------------------------------- .rpt
print("=" * 60)
print("Crystal Report (.rpt) 欄位分析")
print("=" * 60)

# Crystal Report 會把字串以長度前綴或純文字方式存檔，
# 這裡抓「看起來像資料表.欄位」的 token，以及中英文標題
table_field = re.compile(rb'([A-Za-z_][A-Za-z0-9_]{1,30})\.([A-Za-z_][A-Za-z0-9_]{1,30})')

all_fields = {}
for path in sorted(glob.glob(os.path.join(BASE, "*.rpt"))):
    name = os.path.basename(path)
    try:
        data = open(path, "rb").read()
    except Exception as e:
        print(f"{name}: 讀取失敗 {e}")
        continue

    # 抓 utf-16le 中文（Windows 軟體常見）
    text_u16 = data.decode("utf-16-le", errors="ignore")
    text_ascii = data.decode("latin-1", errors="ignore")

    pairs = set()
    for blob in (data,):
        for m in table_field.finditer(blob):
            pairs.add(f"{m.group(1).decode('latin-1')}.{m.group(2).decode('latin-1')}")

    # 中文字串片段（挑 2-8 個中文字，看起來像標題）
    cn = set(re.findall(r'[\u4e00-\u9fff]{2,10}', text_u16))

    all_fields[name] = {"pairs": sorted(pairs), "cn": sorted(cn)[:12]}

    print(f"\n--- {name} ({len(data)} bytes) ---")
    if pairs:
        print(f"  表.欄位: {', '.join(sorted(pairs)[:14])}")
    if cn:
        print(f"  中文字串: {', '.join(list(cn)[:10])}")

# ---------------------------------------------------------------- 彙整
print("\n" + "=" * 60)
print("跨報表出現的資料表（推測 CAR35 核心資料模型）")
print("=" * 60)

tables = {}
for name, info in all_fields.items():
    for p in info["pairs"]:
        t = p.split(".")[0]
        tables.setdefault(t, set()).add(name)

for t, reports in sorted(tables.items(), key=lambda x: -len(x[1])):
    print(f"  {t:24s} 出現在 {len(reports)} 份報表")

# ---------------------------------------------------------------- .mdb
print("\n" + "=" * 60)
print("Access 資料庫 (.mdb)")
print("=" * 60)
for mdb in sorted(glob.glob(os.path.join(BASE, "*.mdb"))):
    print(f"  {os.path.basename(mdb)} ({os.path.getsize(mdb)} bytes)")
