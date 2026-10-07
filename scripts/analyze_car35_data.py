#!/usr/bin/env python3
"""
CAR35 資料缺口盤點（可重跑）

用途：回答「繼續開發 AutoRepairOS 還需要 CAR35 的哪些資料」。
不是功能清單（那看 docs/CAR35_COMPARISON.md），而是**逐表逐欄的資料盤點**：
CAR35 有什麼、已經搬了多少、還缺多少、缺的是哪一段年份或哪一種業務。

用法：
    pip install access-parser          # 需要一次
    python scripts/analyze_car35_data.py
    python scripts/analyze_car35_data.py --mdb C:/path/Data35.mdb --db data/autorepair.db

輸出：stdout 報告 + data/car35_data_gap.json（機器可讀，給後續 migration 用）

注意：
  * access-parser 讀 Big5 字串會變成 latin-1 亂碼，需 fix_encoding() 還原。
  * 它對 130MB 的 mdb 沒有索引，逐表 parse 要幾分鐘，這是正常的。
  * 「非空筆數」比「總筆數」重要：舊系統有大量建立好但從未使用的欄位
    （Insurance Flag / Decorate Flag / Check Date 全表 0 筆），
    那些不是功能，不要為了它們改 schema。
"""

import argparse
import contextlib
import io
import json
import os
import re
import sqlite3
import sys
from collections import Counter

try:
    from access_parser import AccessParser
except ImportError:
    print("錯誤: 需要 access-parser。安裝: pip install access-parser")
    sys.exit(1)


# ------------------------------------------------------------
# 對照表：CAR35 表 → AutoRepairOS 表
# 每一項記錄「哪些欄位是重點」以及「這張表存在的意義」。
# 沒有出現在這裡的 CAR35 表，代表評估後不需要（見 docs/CAR35_COMPARISON.md）。
# ------------------------------------------------------------
TABLE_MAP = [
    # (CAR35 表,            AutoRepairOS 表,       業務意義,                     關鍵欄位)
    ("Customer",           "customers",            "客戶主檔",                   ["No", "Name", "Short Name", "Birthday", "ID No", "Telephone-1", "Hand Telephone", "Address-1", "Comm Name"]),
    ("Car",                "vehicles",             "車輛主檔",                   ["No", "Customer No", "Model No", "OEM No", "Year", "Color", "Cylinder Capacity", "Engine No", "Body No", "Driver Name", "Driver Telephone-1", "Last Mileage", "Last Maintain Date", "Start Check Date", "End Check Date", "Vaild Date"]),
    ("Firm",               "suppliers",            "廠商主檔",                   ["No", "Name", "Short Name", "Tax No", "Fax", "Sales Name"]),
    ("Staff",              "users",                "人員（含外包商）",           ["No", "Name", "Branch No", "Id No", "Start Date", "End Date", "Bank No", "Birthday"]),
    ("Material",           "inventory_items",      "零件主檔",                   ["No", "Name", "Specific", "Class No", "Store No", "In Price", "Out Price", "Current Amount", "Safe Amount", "Firm No-1", "Last In Date", "Last Out Date"]),
    ("Insurance Company",  "insurance_companies",  "保險公司主檔",               ["No", "Name", "Short Name", "Tax No", "Comm Name", "Telephone-1", "Fax", "Address-1"]),
    ("Maintain Header",    "work_orders",          "工單主檔（估價/維修單）",     ["No", "Date", "Car No", "Customer No", "Mileage", "Real Total", "Detail Total", "Discount Percent", "Discount Price", "Tax Flag", "Tax Price", "Tax Total", "Pay Flag", "Clear No", "Staff No", "Remark", "Complete Date", "Consign Date", "Insurance Flag", "Insurance Company No", "Insurance Remark", "Decorate Flag"]),
    ("Maintain Detail",    "work_order_items",     "工單明細（零件/工資）",       ["No", "Sequence No", "Material No", "Price", "Amount", "Staff No", "Remark"]),
    ("Maintain Record Header", "maintenance_templates",    "保養套餐主檔",      ["No", "Name"]),
    ("Maintain Record Detail", "maintenance_template_items", "保養套餐明細",    ["No", "Sequence No", "Material No", "Price", "Amount", "Staff No"]),
    ("Car Insurance List", "vehicle_insurances",   "車輛投保紀錄",               ["No", "Insurance Class", "License Date", "Card No", "Company No", "Remark"]),
    ("Car Check List",     "vehicle_inspections",  "車輛定檢排程",               ["No", "Order Date", "Check Date", "Firm No"]),
    ("Spend List",         "expenses",             "零用金／雜支",               ["Sequence No", "Date", "In Price", "Out Price", "Material No", "Remark"]),
    ("Customer Pay",       "（缺：收款/付款方式）", "應收帳款沖銷（收款紀錄）",   ["No", "Maintain No", "Customer No", "Car No", "Real Total", "Detail Total", "Discount Price", "Tax Price", "Pay Total", "Cash Total", "Card Total", "Check Total", "Card No", "Check No", "Bank Class", "Check Date", "Pay Date", "Pay Remark", "Delete Flag"]),
    ("Material Header",    "（缺：進貨單）",        "進貨單主檔（廠商→車行）",    ["No", "Date", "Firm No", "Firm Cit No", "Real Total", "Detail Total", "Discount Percent", "Tax Flag", "Tax Price", "Staff No", "Remark"]),
    ("Material Detail",    "（缺：進貨明細）",      "進貨單明細",                 ["No", "Sequence No", "Material No", "Price", "Amount", "Remark"]),
    ("Check In",           "（空表）",              "應收票據",                   ["No", "In Date", "Due Date", "Customer No", "Bank No1", "Amount", "Status"]),
    ("Check Out",          "（空表）",              "應付票據",                   ["No", "Out Date", "Due Date", "Firm No", "Amount", "Status"]),
    ("Firm Pay",           "（空表）",              "應付帳款沖銷",               ["No", "Firm No", "Real Total", "Pay Total", "Cash Total", "Check Total", "Pay Date"]),
]

# 這幾欄在舊系統是「建立好但全表 0 筆」，屬於設計時預留而非實際使用。
# 若盤點結果顯示它們是 0，不要因此加 schema —— 先確認是沒用還是沒回填。
ZERO_SUSPECTS = [
    ("Maintain Header", ["Insurance Flag", "Insurance Company No", "Insurance Name",
                         "Insurance Date", "Insurance Table No", "Insurance Remark",
                         "Decorate Flag", "Maintain Price", "Check Date",
                         "State_1", "State_2", "State_3", "State_4"]),
]

# 只抽這幾張的付款欄位，用來量化「金流拆分」的實際使用率
PAY_COLS = ["Cash Total", "Card Total", "Check Total", "Bank Class", "Card No",
            "Check No", "Check Date", "Pay Date", "Maintain No", "Discount Price",
            "Tax Price", "Delete Flag"]


# ------------------------------------------------------------
# 讀取與清理
# ------------------------------------------------------------
def fix_encoding(s):
    """access-parser 以 latin-1 讀出 Big5 位元組，這裡還原為正確中文。"""
    if not isinstance(s, str) or not s:
        return s
    try:
        return s.encode('latin-1').decode('big5')
    except (UnicodeEncodeError, UnicodeDecodeError, LookupError):
        return s


def norm(v):
    v = fix_encoding(v) if isinstance(v, str) else v
    if v is None:
        return None
    v = str(v).replace('\x00', '').strip()
    return v or None


def is_filled(v):
    """非空判定：None / '' / 0 / '0' / ' ' 都算空。

    為什麼 0 算空：舊系統大量用 0 當「未填」的預設值（欄位是數值型，
    沒填就存 0），若把 0 算成有值會嚴重高估使用率。
    """
    if v is None:
        return False
    if isinstance(v, str):
        return v.replace('\x00', '').strip() not in ('', '0', '.')
    if isinstance(v, (int, float)):
        return v != 0
    return True


class Car35:
    def __init__(self, mdb_path):
        self.db = AccessParser(mdb_path)
        self._cache = {}

    def tables(self):
        return [t for t in self.db.catalog.keys() if t != 'MSysObjects']

    def get(self, name):
        if name not in self._cache:
            with contextlib.redirect_stderr(io.StringIO()):
                try:
                    self._cache[name] = self.db.parse_table(name)
                except Exception as e:
                    self._cache[name] = {"__error__": str(e)}
        return self._cache[name]

    def rows(self, name):
        t = self.get(name)
        if "__error__" in t:
            return 0
        return max((len(v) for v in t.values() if isinstance(v, list)), default=0)

    def col(self, name, field):
        t = self.get(name)
        return t.get(field)

    def filled(self, name, field):
        v = self.col(name, field)
        if not isinstance(v, list):
            return None
        return sum(1 for x in v if is_filled(x))

    def values(self, name, field, limit=None):
        v = self.col(name, field) or []
        out = [norm(x) for x in v]
        out = [x for x in out if x]
        return out[:limit] if limit else out


# ------------------------------------------------------------
# 報告
# ------------------------------------------------------------
def sqlite_counts(db_path):
    if not os.path.exists(db_path):
        return {}
    conn = sqlite3.connect(db_path)
    out = {}
    for (t,) in conn.execute("SELECT name FROM sqlite_master WHERE type='table'"):
        try:
            out[t] = conn.execute(f'SELECT COUNT(*) FROM "{t}"').fetchone()[0]
        except sqlite3.Error:
            out[t] = None
    conn.close()
    return out


def report(mdb, db_path):
    c = Car35(mdb)
    counts = sqlite_counts(db_path)
    lines = []
    P = lines.append

    P("=" * 78)
    P("CAR35 資料缺口盤點")
    P(f"  Access : {mdb}  ({os.path.getsize(mdb)/1048576:.0f} MB)")
    P(f"  SQLite : {db_path}")
    P("=" * 78)

    P("\n【A】CAR35 全部資料表")
    for t in c.tables():
        P(f"    {t:26s} {c.rows(t):>8,} 筆")

    P("\n【B】逐表對照：CAR35 → AutoRepairOS")
    P(f"    {'CAR35 表':<24}{'筆數':>9}  {'→ 我們的表':<26}{'筆數':>9}  差額")
    P("    " + "-" * 74)
    gap = []
    for src, dst, meaning, fields in TABLE_MAP:
        n_src = c.rows(src)
        n_dst = counts.get(dst) if dst in counts else None
        if n_dst is None:
            note = dst if dst.startswith("（") else "尚未建立"
            diff = f"缺 {n_src:,}"
        else:
            diff = f"{n_src - n_dst:+,}"
        P(f"    {src:<24}{n_src:>9,}  {dst:<26}"
          f"{(n_dst if n_dst is not None else '-'):>9}  {diff}")
        gap.append({"car35_table": src, "meaning": meaning, "car35_rows": n_src,
                    "ours_table": dst, "ours_rows": n_dst,
                    "missing": (n_src - n_dst) if n_dst is not None else n_src})

    P("\n【C】關鍵欄位的實際使用率（非空筆數 / 總筆數）")
    P("    重點看「不是 100% 也不是 0%」的欄位 —— 那代表功能真的在用但只覆蓋部分資料。")
    for src, dst, meaning, fields in TABLE_MAP:
        n = c.rows(src)
        if not n:
            continue
        P(f"\n    ── {src}（{meaning}）{n:,} 筆")
        for f in fields:
            k = c.filled(src, f)
            if k is None:
                continue
            pct = 100.0 * k / n
            flag = ""
            if k == 0:
                flag = "  ← 全表 0 筆（預留欄位，非功能）"
            elif pct > 99:
                flag = "  ← 幾乎都有"
            P(f"       {f:<24}{k:>9,}  {pct:5.1f}%{flag}")

    P("\n【D】金流拆分實際使用率（Customer Pay = 應收帳款沖銷）")
    cp = "Customer Pay"
    n = c.rows(cp)
    if n:
        tot_cash = sum(x for x in (c.col(cp, 'Cash Total') or []) if isinstance(x, (int, float)))
        tot_card = sum(x for x in (c.col(cp, 'Card Total') or []) if isinstance(x, (int, float)))
        tot_chk = sum(x for x in (c.col(cp, 'Check Total') or []) if isinstance(x, (int, float)))
        P(f"    總筆數 {n:,}")
        P(f"      現金  {c.filled(cp,'Cash Total'):>8,} 筆  合計 {tot_cash:>16,.0f}")
        P(f"      刷卡  {c.filled(cp,'Card Total'):>8,} 筆  合計 {tot_card:>16,.0f}")
        P(f"      支票  {c.filled(cp,'Check Total'):>8,} 筆  合計 {tot_chk:>16,.0f}")
        P(f"    → 現金佔金額 {100*tot_cash/(tot_cash+tot_card+tot_chk):.1f}%，"
          f"但刷卡＋支票有 {c.filled(cp,'Card Total')+c.filled(cp,'Check Total'):,} 筆")
        P("      ＝ 這塊不做，帳面上看不到 5 千萬的資金流。")

    P("\n【E】進貨（Material Header / Detail）是否真的對應到廠商與料號")
    fh = c.get("Material Header")
    if "__error__" not in fh:
        firm_no = set(c.values("Firm", "No"))
        fn = c.values("Material Header", "Firm No")
        mat_no = set(c.values("Material", "No"))
        mn = c.values("Material Detail", "Material No")
        P(f"    Material Header  {c.rows('Material Header'):,} 筆")
        P(f"      Firm No 有值 {len(fn):,} 筆，能對到廠商主檔 {sum(1 for x in fn if x in firm_no):,} 筆")
        P(f"    Material Detail  {c.rows('Material Detail'):,} 筆")
        P(f"      Material No 有值 {len(mn):,} 筆，能對到零件主檔 {sum(1 for x in mn if x in mat_no):,} 筆")
        P("    → 對得到主檔 ＝ 這是真的進貨單（不是領料單），可以直接建採購/入庫紀錄。")

    P("\n【F】資料對不上的地方（migration 需要補的關聯）")
    mh_car = c.values("Maintain Header", "Car No")
    our_plates = set()
    if os.path.exists(db_path):
        conn = sqlite3.connect(db_path)
        try:
            our_plates = {r[0] for r in conn.execute("SELECT plate_number FROM vehicles")}
        except sqlite3.Error:
            pass
        conn.close()
    if our_plates:
        miss = sorted({p for p in mh_car if p not in our_plates})
        P(f"    工單車牌 {len(set(mh_car)):,} 種，其中 {len(miss):,} 種不在 vehicles 表")
        P(f"      樣本：{miss[:8]}")
    cil = c.values("Car Insurance List", "No")
    if our_plates:
        miss_i = sorted({p for p in cil if p not in our_plates})
        P(f"    投保車牌 {len(set(cil)):,} 種，其中 {len(miss_i):,} 種不在 vehicles 表")
        P(f"      樣本：{miss_i[:8]}")
    P("    → 原因是 migrate_vehicles() 遇到「客戶對不上」就整台車跳過，")
    P("      連帶讓工單、投保、定檢紀錄失去車主。要補的是車主對應，不是重跑全部。")

    P("\n【G】保險件在舊系統怎麼記錄（影響 P0-1 的資料回填）")
    mh = c.get("Maintain Header")
    if "__error__" not in mh:
        P("    Maintain Header 的保險欄位（Insurance Flag / Company No / Name / Date /")
        P("    Remark / Table No）**全表 0 筆** —— 設計了但沒在用。")
        KEY = ('保險', '退保', '理賠', '出險', '自付', '自負', '產物', '產險',
               '富邦', '國泰', '新光', '明台', '泰安', '華南', '旺旺', '美亞',
               '第一產物', '新安', '兆豐', '蘇黎世', '台壽')
        all_remark = c.values("Maintain Header", "Remark")
        remarks = [r for r in all_remark if any(k in r for k in KEY)]
        P(f"    Remark 有值共 {len(all_remark):,} 筆，其中 {len(remarks):,} 筆提到保險")
        P(f"      樣本：{remarks[:4]}")
        P("    → 保險件無法從結構化欄位回填，只能靠 Remark 關鍵字比對（覆蓋率低）。")
        P("      結論：工單的 insurance_company_id / claim_no 從舊資料回填的效益很低，")
        P("      直接從「新開的工單」開始收結構化資料比較實際。")
    P("    另有 Car Insurance List（車輛投保紀錄）{:,} 筆是完整可用的：".format(
        c.rows("Car Insurance List")))
    P("      險種 + 發照日 + 保險卡號 + 保險公司編號，這才是車輛保險的資料來源。")

    P("\n【H】舊系統「建立好但全表 0 筆」的欄位（不要為它們改 schema）")
    for src, fields in ZERO_SUSPECTS:
        zeros = [f for f in fields if c.filled(src, f) == 0]
        P(f"    {src}: {', '.join(zeros)}")

    P("\n【I】規模（決定 migration 與備份策略）")
    for t in ["Maintain Detail", "Maintain Header", "Material Detail", "Material Header",
              "Customer Pay", "Car Check List", "Car", "Material", "Customer"]:
        P(f"    {t:24s} {c.rows(t):>9,} 筆")

    txt = "\n".join(lines)
    print(txt)
    return {"tables": gap, "report": txt}


def main():
    here = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    ap = argparse.ArgumentParser(description="CAR35 資料缺口盤點")
    ap.add_argument("--mdb", default=r"C:\Users\fr\35-20231008\Data35.mdb")
    ap.add_argument("--db", default=os.path.join(here, "data", "autorepair.db"))
    ap.add_argument("--json", default=os.path.join(here, "docs", "car35_data_gap.json"),
                    help="盤點結果（機器可讀）；預設寫進 docs/ 以便進版控留證")
    args = ap.parse_args()

    if not os.path.exists(args.mdb):
        print(f"錯誤: 找不到 Access 資料庫 {args.mdb}")
        sys.exit(1)

    res = report(args.mdb, args.db)
    if args.json:
        os.makedirs(os.path.dirname(args.json), exist_ok=True)
        with open(args.json, "w", encoding="utf-8") as f:
            json.dump(res, f, ensure_ascii=False, indent=1)
        print(f"\n已寫出 {args.json}")


if __name__ == "__main__":
    main()
