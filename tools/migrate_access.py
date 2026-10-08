#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Access → SQLite 遷移工具 (AutoRepair OS)

將舊版 AutoRepair (Car35 / Data35.mdb, Access 97) 轉換為新版 SQLite 資料庫。

舊版資料庫為 Big5 編碼的 Access 97 格式，現代 ODBC 驅動無法開啟，
因此使用 access-parser 直接解析二進位檔。

主鍵策略（依使用者指示）：
  - 客戶：姓名 (Customer.Name) 為主；姓名空時退回客戶編號
  - 車輛：車牌 (Car.No) 為主鍵
  - 廠商：Firm.Name
  - 零件：Material.No

用法:
    python migrate_access.py <access.mdb> <sqlite.db> [選項]

選項:
    --dry-run          模擬執行，不寫入資料庫
    --backup           遷移前備份 SQLite
    --reset            先清空已遷移的資料（保留測試資料 id<=N）
    --lookups <path>   匯出 lookup 表 (車別/油別/倉別/部門/科目/銀行) 為 JSON

範例:
    python tools/migrate_access.py "C:/Users/fr/35-20231008/Data35.mdb" \
                                   "C:/Users/fr/AutoRepairOS/data/autorepair.db" \
                                   --backup --reset --lookups data/legacy_lookups.json
"""

import argparse
import contextlib
import io
import json
import os
import shutil
import sqlite3
import sys
from collections import defaultdict
from datetime import datetime

try:
    from access_parser import AccessParser
except ImportError:
    print("錯誤: 需要 access-parser。安裝: pip install access-parser")
    sys.exit(1)


# ============================================================
# 舊版 Access 表定義
# ============================================================

# 主檔表
MASTER_TABLES = ['Customer', 'Car', 'Firm', 'Staff', 'Material']

# 交易表
TX_TABLES = ['Maintain Header', 'Maintain Detail',
             'Material Header', 'Material Detail', 'Customer Pay',
             'Car Insurance List', 'Car Check List',
             'Maintain Record Header', 'Maintain Record Detail',
             'Spend List']

# 查表 (lookup)
# 注意：Insurance Company 是「保險公司主檔」（含統編、地址），
# 與 Insurance Class（險種）不同，兩者都要匯出。
LOOKUP_TABLES = ['Car Class', 'Gas Class', 'Store', 'Branch', 'Class',
                 'Unit Class', 'Insurance Class', 'Insurance Company',
                 'Model', 'OEM', 'Color Class', 'Bank']

ALL_TABLES = MASTER_TABLES + TX_TABLES + LOOKUP_TABLES

# CAR35 零件類別（Material.Class No → 可讀名稱）。
# 來源：Class 查表（A 引擎 / B 電機音響 / …），但 Material 實際用到更多單碼
# （M 變速箱 / N 烤漆細項 / P 塗料 / L 鈑金 / Z 其他），依實際資料補齊。
PART_CATEGORY = {
    'A': '引擎',
    'B': '電機音響',
    'C': '傳動轉向',
    'D': '制動',
    'E': '車身鈑金',
    'F': '輪胎',
    'G': '工資',
    'H': '烤漆',
    'I': '其他',
    'L': '鈑金',
    'M': '變速箱',
    'N': '烤漆',
    'P': '塗料',
    'Z': '其他',
}

# CAR35 部門（Branch）→ 判斷人員身分
# A 引擎 / B 電機 / C 鈑金 / D 烤漆 / E 行政 / F 零件 = 員工
# G = 眷屬（車行家族成員，不是技師）
# H = 廠商（外包商，不該當技師）
STAFF_BRANCHES = {'A', 'B', 'C', 'D', 'E', 'F'}
FAMILY_BRANCH = 'G'
VENDOR_BRANCH = 'H'

# 保險公司名稱特徵（Branch 為空時用來辨識）
INSURANCE_HINT = ('產物', '保險', '產險', '人壽', '壽險')

# 外包廠商名稱特徵（用來把協力廠商與真員工分開）
#
# 為什麼需要：Branch A-F 表面上是「員工部門」，但裡面混了協力廠商
# （上上汽車、元發電機、精修汽車、嘉隆汽車…）。工單的 Staff No 同時
# 記錄「誰做的」，可能是指派員工也可能是外包商。
OUTSOURCE_HINT = ('汽車', '電機', '幫浦', '輪胎', '鈑金', '烤漆', '實業',
                  '企業', '興業', '貿易', '自動', '帆布', '廣告', '吊車',
                  '玻璃', '塗料', '事務', '修護', '保養廠', '材料')


# ============================================================
# 編碼 / 型別轉換
# ============================================================

def fix_encoding(s):
    """access-parser 以 latin-1 讀出 Big5 位元組，這裡還原為正確中文。"""
    if not isinstance(s, str) or not s:
        return s
    try:
        return s.encode('latin-1').decode('big5')
    except (UnicodeEncodeError, UnicodeDecodeError, LookupError):
        return s


def clean_str(s):
    """清理字串：修正編碼、去空白、空字串轉 None。"""
    s = fix_encoding(s)
    if s is None:
        return None
    s = str(s).replace('\x00', '').strip()
    return s if s else None


def clean_num(n):
    if n is None:
        return 0.0
    try:
        return float(n)
    except (TypeError, ValueError):
        return 0.0


def clean_int(n):
    if n is None:
        return 0
    try:
        return int(float(n))
    except (TypeError, ValueError):
        return 0


def clean_bool(b):
    if b is None:
        return 0
    if isinstance(b, bool):
        return 1 if b else 0
    try:
        return int(b)
    except (TypeError, ValueError):
        return 0


def clean_phone(s):
    """清理電話：移除中文雜訊（如 '公司'、'老板'、'ATF'），只留數字與 - # ( )。"""
    s = clean_str(s)
    if not s:
        return None
    import re
    s = re.sub(r'[^\d\-#()+\s]', '', s).strip()
    s = re.sub(r'\s+', '', s)
    return s or None


def clean_address(*parts):
    """合併地址欄位並去除重複。
    舊系統常把同一地址重複填進 Address-1/2/3，這裡只保留相異的部分。"""
    seen = []
    for p in parts:
        p = clean_str(p)
        if p and p not in seen:
            seen.append(p)
    if not seen:
        return None
    # 若其中一段已包含另一段，取最長的那個（避免 "台中市南屯區" + "台中市南屯區南屯路"）
    seen.sort(key=len, reverse=True)
    return seen[0]


def clean_year(v):
    """舊版 Year 欄位格式為 YYYYMM（如 200207）→ 取年份 2002。
    也容忍純年份（如 2002）或空值。"""
    v = clean_str(v)
    if not v:
        return None
    digits = ''.join(ch for ch in v if ch.isdigit())
    if len(digits) >= 4:
        y = int(digits[:4])
        if 1900 <= y <= 2100:
            return y
    return None


def convert_roc_date(d):
    """民國年 → 西元年。 '105/02/22' → '2016/02/22'

    舊資料有大量打字錯誤，實測需處理：
      '990/01/31'  → 多打一個 0，應為 99 年（2010）
      '105/11/31'  → 11 月沒有 31 日，夾到月底 11/30
      '176/28/93'  → 年月日全錯，無法救，回 None
      '63361/10/'  → 年份爆走，回 None

    年份範圍設 1..130（民國 130 = 2041）：超過必為打字錯誤而非真實日期，
    直接捨棄比產生 2069 年這種荒謬值好。
    """
    import calendar

    d = clean_str(d)
    if not d:
        return None
    parts = d.split('/')
    if len(parts) != 3:
        return None
    try:
        y = int(parts[0])
        m = int(parts[1])
        day = int(parts[2])
    except ValueError:
        return None

    # 年份修正：'990' → 99（多打的 0）
    while y > 130 and y % 10 == 0:
        y //= 10
    if not (1 <= y <= 130):
        return None
    if not (1 <= m <= 12):
        return None
    # 日超出該月天數 → 夾到月底（如 11/31 → 11/30、2/31 → 2/28）
    last_day = calendar.monthrange(2000, m)[1]
    if day < 1:
        return None
    if day > last_day:
        day = last_day

    return f"{y + 1911:04d}/{m:02d}/{day:02d}"


def norm_staff_no(s):
    """正規化人員編號，用於比對舊系統的各種變體。

    實測同一人的寫法有：E003 / e003 / E0003 / E03 / RE003 / `E003。
    規則：去非英數 → 大寫 → 去掉開頭的 R/W 前綴 → 去掉字母後多餘的 0 →
    數字補零到 3 碼。
    """
    import re
    s = re.sub(r'[^A-Za-z0-9]', '', s or '').upper()
    s = re.sub(r'^[RW]+', '', s)
    s = re.sub(r'^([A-Z])0+(\d)', r'\1\2', s)
    m = re.match(r'^([A-Z])(\d+)$', s)
    if m:
        s = m.group(1) + m.group(2).zfill(3)
    return s


# 模組層級別名（供 Migrator 方法內呼叫）
_norm_staff_no = norm_staff_no


def add_months(date_str, months):
    """日期加 N 個月，回傳 YYYY-MM-DD。用於推算下次保養日。

    以純字串運算，不引入 dateutil 依賴。
    """
    if not date_str:
        return None
    try:
        y, m, d = (int(x) for x in date_str[:10].replace('/', '-').split('-'))
    except (ValueError, AttributeError):
        return None
    total = y * 12 + (m - 1) + int(months)
    ny, nm = divmod(total, 12)
    nm += 1
    # 處理月底（如 1/31 + 1 個月 → 2/28）
    import calendar
    last = calendar.monthrange(ny, nm)[1]
    nd = min(d, last)
    return f"{ny:04d}-{nm:02d}-{nd:02d}"


# ============================================================
# 資料提取
# ============================================================

def extract_tables(mdb_path, table_names):
    """從 Access 資料庫提取指定表的所有資料（修正編碼）。"""
    buf = io.StringIO()
    with contextlib.redirect_stdout(buf), contextlib.redirect_stderr(buf):
        parser = AccessParser(mdb_path)

    result = {}
    for name in table_names:
        try:
            with contextlib.redirect_stdout(buf), contextlib.redirect_stderr(buf):
                raw = parser.parse_table(name)
        except Exception as e:
            print(f"  跳過 {name}: {e}")
            continue

        table = {}
        for col, vals in raw.items():
            if isinstance(vals, list):
                table[col] = [fix_encoding(v) if isinstance(v, str) else v for v in vals]
            else:
                table[col] = fix_encoding(vals) if isinstance(vals, str) else vals

        n = len(next(iter(table.values()))) if table else 0
        result[name] = table
        print(f"  提取 {name}: {len(table)} 欄位, {n} 列")

    return result


def col(table, name, idx, default=None):
    """安全取值：table[欄位][idx]，欄位不存在或越界回傳 default。"""
    vals = table.get(name)
    if not vals or idx >= len(vals):
        return default
    return vals[idx]


# ============================================================
# 遷移
# ============================================================

class Migrator:
    def __init__(self, data, sqlite_path, dry_run=False):
        self.data = data
        self.sqlite_path = sqlite_path
        self.dry_run = dry_run
        self.conn = sqlite3.connect(sqlite_path)
        self.cur = self.conn.cursor()
        self.stats = defaultdict(int)

        # 編號 → 新 id 對應表
        self.cust_id = {}       # Customer.No  → customers.id（相容用，見 _build_customer_resolver）
        self.veh_id = {}        # Car.No       → vehicles.id
        self.sup_id = {}        # Firm.No      → suppliers.id
        self.user_id = {}       # Staff.No     → users.id
        self.inv_id = {}        # Material.No  → inventory_items.id
        self.wo_id = {}         # Maintain Header index → work_orders.id
        # 舊單號 → work_orders.id。收款沖銷靠這個關聯工單
        # （work_orders 本身沒有存舊單號，必須在遷移當下記下）。
        self.wo_by_legacy_no = {}

        # 客戶解析器（notes 精確 + 姓名 fallback），見 _build_customer_resolver
        self.no2name = {}       # Customer.No  → Customer.Name
        self.cust_by_notes = {}  # Customer.No  → customers.id（精確）
        self.cust_by_name = {}   # Customer.Name → customers.id
        # 統計每個解析路徑命中次數，讓「救回多少」可從輸出看出來
        self.resolve_via = defaultdict(int)

    # ---------- 1. 客戶 ----------
    def migrate_customers(self):
        print("\n[1] 客戶 (Customer → customers)")
        t = self.data.get('Customer')
        if not t:
            print("  無 Customer 表，跳過")
            return

        self.cur.execute("SELECT phone, name FROM customers")
        used_phones = set()
        for phone, name in self.cur.fetchall():
            used_phones.add(phone)
            used_phones.add(name)   # 姓名也當唯一鍵用

        def unique_phone(candidates, fallback):
            """挑一個未被使用的 phone（customers.phone 有 UNIQUE 限制）。"""
            for c in candidates:
                if c and c not in used_phones:
                    used_phones.add(c)
                    return c
            # 全部撞號 → 用編號加序號
            base = fallback or 'legacy'
            k = 1
            while f"{base}#{k}" in used_phones:
                k += 1
            val = f"{base}#{k}"
            used_phones.add(val)
            return val

        rows = []
        for i in range(len(t['No'])):
            no = clean_str(col(t, 'No', i))
            name = clean_str(col(t, 'Name', i))
            if not name and not no:
                continue

            # 主鍵策略：姓名優先，姓名空則用編號
            key = name or no
            if key in used_phones:
                # 已存在 → 只建立對應（不新增）
                continue

            # 電話：市話 → 市話2 → 手機（清理雜訊；撞號則自動加序號）
            phone = unique_phone(
                [clean_phone(col(t, 'Telephone-1', i)),
                 clean_phone(col(t, 'Telephone-2', i)),
                 clean_phone(col(t, 'Hand Telephone', i))],
                no)

            address = clean_address(
                col(t, 'Address-1', i),
                col(t, 'Address-2', i),
                col(t, 'Address-3', i))

            rows.append((
                name or no,                         # name
                phone,                              # phone (UNIQUE)
                None,                               # line_user_id
                address,                            # address
                'line',                             # notification_preference
                no,                                 # notes ← 保留舊客戶編號
                0, 0,                               # total_spent, unpaid_amount
            ))
            used_phones.add(key)

        if self.dry_run:
            print(f"  [DRY-RUN] 將新增 {len(rows)} 筆客戶")
        else:
            self.cur.executemany("""
                INSERT INTO customers
                  (name, phone, line_user_id, address, notification_preference,
                   notes, total_spent, unpaid_amount)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?)
            """, rows)
            self.conn.commit()
            print(f"  新增 {len(rows)} 筆客戶")

        # 建立 Customer.No → customers.id
        self.cur.execute("SELECT id, notes, name, phone FROM customers")
        for cid, notes, name, phone in self.cur.fetchall():
            if notes:
                self.cust_id[notes] = cid
            if name:
                self.cust_id.setdefault(name, cid)
        self.stats['customers'] = len(rows)

    # ---------- 2. 車輛 ----------
    def _build_customer_resolver(self):
        """建立「CAR35 客戶編號 → customers.id」的解析器（含姓名 fallback）。

        為什麼需要姓名 fallback：
          CAR35 的 Customer 表有 8,118 筆（8,117 個不同編號），但只有 6,511 個
          不同姓名 —— 同名客戶在遷移時被合併成同一筆 customers（name 為鍵）。
          因此 8,117 個編號中有約 1,600 個對不到 customers.notes（只有被選為
          代表的那個編號會被寫進 notes）。

          實際後果：車輛遷移用 cust_id.get(customer_no) 找車主，這 1,600 個
          編號全部落空 → 整台車被跳過。實測 2,024 台車因此沒遷進來。

          修法：先查 notes（精確），查不到再用「CAR35 編號 → 姓名 → customers.name」
          兩段解析。實測可救回 2,000/2,024 台（86.5% → 99.8%）。

        注意：姓名 fallback 會讓同名客戶的車輛全部掛到同一筆 customers，
        這是既有合併策略的結果（無法還原成兩筆），但「車有車主」遠優於
        「車不存在」—— 車主資訊仍可從 CAR35 的車主姓名欄核對。
        """
        # CAR35 編號 → 姓名
        cu = self.data.get('Customer')
        self.no2name = {}
        if cu:
            for i in range(len(cu['No'])):
                no = clean_str(col(cu, 'No', i))
                name = clean_str(col(cu, 'Name', i))
                if no and name:
                    self.no2name.setdefault(no, name)

        # notes（CAR35 編號）→ customers.id
        self.cust_by_notes = {}
        # name → customers.id（取 id 最小者，與 migrate_customers 的 setdefault 一致）
        self.cust_by_name = {}
        self.cur.execute("SELECT id, notes, name FROM customers")
        for cid, notes, name in self.cur.fetchall():
            if notes:
                self.cust_by_notes.setdefault(notes, cid)
            if name:
                self.cust_by_name.setdefault(name, cid)

        # 相容：cust_id 仍保留（notes + name 兩種鍵），舊呼叫端不需改
        self.cust_id = dict(self.cust_by_notes)
        for k, v in self.cust_by_name.items():
            self.cust_id.setdefault(k, v)

        print(f"  客戶解析器：notes {len(self.cust_by_notes):,} 筆、"
              f"姓名 {len(self.cust_by_name):,} 筆、"
              f"CAR35 編號→姓名 {len(self.no2name):,} 筆")

    def resolve_customer(self, cust_no):
        """CAR35 客戶編號 → customers.id，查不到回 None。

        三段解析：notes 精確 → 編號轉姓名 → 姓名。
        """
        if not cust_no:
            return None
        cid = self.cust_by_notes.get(cust_no)
        if cid is not None:
            self.resolve_via['notes'] += 1
            return cid
        name = self.no2name.get(cust_no)
        if name:
            cid = self.cust_by_name.get(name)
            if cid is not None:
                self.resolve_via['name'] += 1
                return cid
        self.resolve_via['miss'] += 1
        return None

    def migrate_vehicles(self):
        print("\n[2] 車輛 (Car → vehicles)")
        t = self.data.get('Car')
        if not t:
            print("  無 Car 表，跳過")
            return

        # 以工單歷史推算保養基準：最後一張有里程的工單日期/里程最可靠。
        # 舊系統的 Car.Last Maintain Date 停在 2023 且與工單對不上，
        # 而工單是實際發生的事實，優先採用。
        last_by_plate = self._last_maintenance_by_plate()

        self.cur.execute("SELECT plate_number FROM vehicles")
        used = {r[0] for r in self.cur.fetchall()}

        rows = []
        from_orders = 0
        for i in range(len(t['No'])):
            plate = clean_str(col(t, 'No', i))
            if not plate or plate in used:
                continue

            cust_no = clean_str(col(t, 'Customer No', i))
            cid = self.resolve_customer(cust_no)
            if cid is None:
                continue    # 沒有對應客戶，跳過（避免 FK 錯誤）

            model = clean_str(col(t, 'Model No', i))
            brand = clean_str(col(t, 'OEM No', i))
            brand_model = model or brand or '未登錄'

            # 保養基準：工單歷史優先，退回舊系統的 Last Maintain Date / Last Mileage
            base_date = None
            base_mileage = None
            if plate in last_by_plate:
                base_date, base_mileage = last_by_plate[plate]
                from_orders += 1
            if not base_date:
                base_date = convert_roc_date(col(t, 'Last Maintain Date', i))
            if not base_mileage:
                lm = clean_int(col(t, 'Last Mileage', i))
                base_mileage = lm if lm > 0 else None

            # 保養間隔：舊系統 Maintain Month 幾乎全空，有值就用，否則用預設 6 個月
            months = clean_int(col(t, 'Maintain Month', i)) or 6
            if months <= 0 or months > 60:
                months = 6
            km = 5000

            rows.append((
                cid,                                            # customer_id
                plate,                                          # plate_number (UNIQUE)
                brand_model,                                    # brand_model
                clean_year(col(t, 'Year', i)),                  # manufacture_year
                clean_str(col(t, 'Body No', i)),                # vin
                clean_str(col(t, 'Engine No', i)),              # engine_number
                convert_roc_date(col(t, 'Vaild Date', i)),      # inspection_due_date
                None,                                           # next_maintenance_date（稍後由基準推算）
                None,                                           # next_maintenance_mileage
                base_date,                                      # last_maintenance_date
                base_mileage,                                   # last_maintenance_mileage
                months,                                         # next_maintenance_interval_months
                km,                                             # next_maintenance_interval_km
            ))
            used.add(plate)

        if self.dry_run:
            print(f"  [DRY-RUN] 將新增 {len(rows)} 筆車輛（{from_orders} 台有工單保養基準）")
        else:
            self.cur.executemany("""
                INSERT INTO vehicles
                  (customer_id, plate_number, brand_model, manufacture_year, vin,
                   engine_number, inspection_due_date, next_maintenance_date,
                   next_maintenance_mileage, last_maintenance_date,
                   last_maintenance_mileage, next_maintenance_interval_months,
                   next_maintenance_interval_km)
                VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
            """, rows)
            self.conn.commit()
            print(f"  新增 {len(rows)} 筆車輛（{from_orders} 台以工單歷史為保養基準）")
            self._derive_next_maintenance()

        self.cur.execute("SELECT id, plate_number FROM vehicles")
        self.veh_id = {r[1]: r[0] for r in self.cur.fetchall()}
        self.stats['vehicles'] = len(rows)

    def _last_maintenance_by_plate(self):
        """從工單歷史取每台車最後一次保養的（日期, 里程）。

        只採計有里程紀錄的工單 —— 沒里程的工單無法推算下次保養里程。
        """
        mh = self.data.get('Maintain Header')
        car = self.data.get('Car')
        if not mh or not car:
            return {}

        # Car No → 車牌（mh 用 Car No 參照 Car 表）
        no_to_plate = {}
        for i in range(len(car['No'])):
            no_to_plate[clean_str(col(car, 'No', i))] = clean_str(col(car, 'No', i))

        best = {}   # plate → (date, mileage)
        for i in range(len(mh['No'])):
            plate = clean_str(col(mh, 'Car No', i))
            if not plate:
                continue
            mileage = clean_int(col(mh, 'Mileage', i))
            if mileage <= 0:
                continue
            date = convert_roc_date(col(mh, 'Date', i))
            if not date:
                continue
            cur_best = best.get(plate)
            # 取日期最新的；同日期取里程較大者
            if (cur_best is None or date > cur_best[0]
                    or (date == cur_best[0] and mileage > cur_best[1])):
                best[plate] = (date, mileage)
        return best

    def _derive_next_maintenance(self):
        """由「最後保養基準 + 間隔」推算下次保養日期與里程。

        下次保養日期 = 最後保養日 + 間隔月數
        下次保養里程 = 最後保養里程 + 間隔公里數
        （舊系統的 Order Maintain Date 只有 24 筆有值，無法直接用）
        """
        rows = self.cur.execute("""
            SELECT id, last_maintenance_date, last_maintenance_mileage,
                   next_maintenance_interval_months, next_maintenance_interval_km
            FROM vehicles
            WHERE COALESCE(last_maintenance_date,'') <> ''
               OR COALESCE(last_maintenance_mileage,0) > 0
        """).fetchall()

        updates = []
        for vid, base_date, base_km, months, km in rows:
            next_date = None
            if base_date:
                next_date = add_months(base_date, months or 6)
            next_km = None
            if base_km and base_km > 0:
                next_km = base_km + (km or 5000)
            if next_date or next_km:
                updates.append((next_date, next_km, vid))

        if updates:
            self.cur.executemany(
                "UPDATE vehicles SET next_maintenance_date=?, next_maintenance_mileage=? WHERE id=?",
                updates)
            self.conn.commit()
        print(f"  推算下次保養: {len(updates)} 台")

    # ---------- 3. 廠商 ----------
    def migrate_suppliers(self):
        print("\n[3] 材料商 (Firm → suppliers)")
        t = self.data.get('Firm')
        if not t:
            print("  無 Firm 表，跳過")
            return

        self.cur.execute("SELECT name FROM suppliers")
        used = {r[0] for r in self.cur.fetchall()}

        rows = []
        for i in range(len(t['No'])):
            name = clean_str(col(t, 'Name', i))
            no = clean_str(col(t, 'No', i))
            key = name or no
            if not key or key in used:
                continue

            addr_parts = [clean_str(col(t, f'Address-{n}', i)) for n in (1, 2, 3)]
            address = clean_address(*addr_parts)

            rows.append((
                key,                                            # name
                clean_phone(col(t, 'Telephone-1', i)),          # phone
                clean_str(col(t, 'Sales Name', i)),             # contact_person
                address,                                        # address
                f"舊編號:{no}" if no else None,                 # notes
                'material',                                     # vendor_type：材料商
            ))
            used.add(key)

        if self.dry_run:
            print(f"  [DRY-RUN] 將新增 {len(rows)} 筆廠商")
        else:
            self.cur.executemany("""
                INSERT INTO suppliers (name, phone, contact_person, address, notes, vendor_type)
                VALUES (?, ?, ?, ?, ?, ?)
            """, rows)
            self.conn.commit()
            print(f"  新增 {len(rows)} 筆廠商")

        self.cur.execute("SELECT id, name, notes FROM suppliers")
        for sid, name, notes in self.cur.fetchall():
            if notes and notes.startswith('舊編號:'):
                self.sup_id[notes[4:]] = sid
            self.sup_id.setdefault(name, sid)
        self.stats['suppliers'] = len(rows)

    # ---------- 4. 人員分類：員工 / 廠商 / 保險公司 / 眷屬 ----------
    def migrate_staff(self):
        """舊系統的 Staff 表把四種東西混在一起，這裡按 Branch 拆開。

        實測 Branch 分布（246 筆）：
            H (86) 廠商      — 久葆汽車、大立輪胎、上豪…（外包商，不是技師）
            '' (52) 混雜     — 保險公司（華山產物）+ 廠商（大統、上方廣告）
            G (40) 眷屬      — 謝惠芳、嚴金猜…（車行家族，不該當技師）
            A-F (68) 員工    — 王彩秀被工單引用 21,969 次，是真正的技師

        舊版把 Branch 含「保險」或「廠商」的當保險公司，結果 86 家廠商
        被灌進保險公司表；正確做法是用專用的 Insurance Company 表（15 家含統編），
        而 Staff 表的廠商併入 suppliers。
        """
        print("\n[4] 人員分類 (Staff → users / suppliers / insurance_companies)")
        t = self.data.get('Staff')
        if not t:
            print("  無 Staff 表，跳過")
            return

        branch = self.data.get('Branch', {})
        branch_map = {}
        for i in range(len(branch.get('No', []))):
            branch_map[clean_str(col(branch, 'No', i))] = clean_str(col(branch, 'Name', i))

        # 既有資料（避免重複）
        self.cur.execute("SELECT username FROM users")
        used_users = {r[0] for r in self.cur.fetchall()}
        self.cur.execute("SELECT code FROM insurance_companies")
        used_ins = {r[0] for r in self.cur.fetchall()}
        self.cur.execute("SELECT name FROM suppliers")
        used_sup = {r[0] for r in self.cur.fetchall()}

        # 工單引用次數 → 判斷誰是真的被指派過的人員
        ref_count = self._staff_reference_counts()
        used_by_orders = set(ref_count)

        user_rows, ins_rows, sup_rows = [], [], []
        kind_stat = defaultdict(int)

        for i in range(len(t['No'])):
            no = clean_str(col(t, 'No', i))
            name = clean_str(col(t, 'Name', i))
            if not name and not no:
                continue
            br = clean_str(col(t, 'Branch No', i)) or ''
            dept_name = branch_map.get(br, '')

            is_insurance = any(h in (name or '') for h in INSURANCE_HINT)
            looks_vendor = any(h in (name or '') for h in OUTSOURCE_HINT)
            times_used = ref_count.get(no, 0)

            # 身分判定：
            #   1. 工單引用 ≥ 3 次 → 實際被當人員指派，保留為員工
            #   2. 名稱像廠商（汽車/電機/輪胎…）→ 協力廠商
            #   3. Branch G → 眷屬；Branch H → 廠商
            #   4. 其餘 Branch A-F → 員工
            if times_used >= 3 and br not in (FAMILY_BRANCH, VENDOR_BRANCH):
                kind = 'employee'
            elif br == FAMILY_BRANCH:
                kind = 'family'
            elif br == VENDOR_BRANCH or looks_vendor:
                kind = 'vendor'
            elif is_insurance:
                kind = 'vendor'          # 保險公司在 Staff 表只是往來對象
            elif br in STAFF_BRANCHES:
                kind = 'employee'
            elif times_used > 0:
                kind = 'employee'
            else:
                kind = 'vendor'

            # 保險公司：只認專用 Insurance Company 表（含統編、正式全名）。
            # Staff 表裡的保險公司只是名字，灌進去會與專用表重複。
            if is_insurance:
                continue

            if kind in ('employee', 'family'):
                username = no or name
                if username in used_users:
                    continue
                user_rows.append((
                    username,
                    'legacy',                       # 舊資料無密碼，需重設
                    'technician',
                    name or no,
                    br,                             # staff_branch
                    'dependent' if kind == 'family' else kind,  # staff_kind
                    clean_str(col(t, 'Id No', i)),          # national_id（身分證）
                    convert_roc_date(col(t, 'Birthday', i)),# birthday
                    convert_roc_date(col(t, 'Start Date', i)),  # insurance_start 加保日
                    convert_roc_date(col(t, 'End Date', i)),    # insurance_end 退保日
                    clean_str(col(t, 'Bank No', i)),        # bank_account
                ))
                used_users.add(username)
                kind_stat[kind] += 1
                continue

            # 協力廠商 / 往來對象 → suppliers
            key = name or no
            if key in used_sup:
                continue
            sup_rows.append((
                key,
                clean_phone(col(t, 'Telephone', i)),
                None,
                clean_str(col(t, 'Address', i)),
                f"舊編號:{no}" if no else None,
                'vendor',
            ))
            used_sup.add(key)
            kind_stat['supplier_vendor'] += 1

        # 專用保險公司表（優先，含統編與正式全名）
        official_ins = self._official_insurance_companies(used_ins)
        ins_rows.extend(official_ins)

        if self.dry_run:
            print(f"  [DRY-RUN] 員工 {len(user_rows)}、廠商 {len(sup_rows)}、保險公司 {len(ins_rows)}")
        else:
            if user_rows:
                self.cur.executemany("""
                    INSERT INTO users (username, password, role, name, staff_branch,
                       staff_kind, national_id, birthday, insurance_start,
                       insurance_end, bank_account)
                    VALUES (?,?,?,?,?,?,?,?,?,?,?)
                """, user_rows)
            if sup_rows:
                self.cur.executemany("""
                    INSERT INTO suppliers (name, phone, contact_person, address, notes, vendor_type)
                    VALUES (?,?,?,?,?,?)
                """, sup_rows)
            if ins_rows:
                self.cur.executemany("""
                    INSERT INTO insurance_companies
                      (code, name, tax_id, claim_phone, phone, contact_person,
                       address, postal_code, notes, active, legacy_no)
                    VALUES (?,?,?,?,?,?,?,?,?,?,?)
                """, ins_rows)
            self.conn.commit()
            print(f"  新增 員工 {len(user_rows)}、廠商 {len(sup_rows)}、保險公司 {len(ins_rows)}")

        self.cur.execute("SELECT id, username FROM users")
        for uid, username in self.cur.fetchall():
            self.user_id[username] = uid
        # 工單的 Staff No 有大小寫/前綴變體（e003 / E0003 / RE003），做別名對應
        self._alias_staff_ids()
        # 眷屬依附的主被保險人（E03-1 → E003）
        self._link_dependents()
        self.stats['users'] = len(user_rows)
        self.stats['insurance'] = len(ins_rows)
        self.stats['supplier_vendors'] = len(sup_rows)

    def _link_dependents(self):
        """把健保眷屬連到主被保險人。

        舊系統用編號後綴表示眷屬關係：
            E03-1 嚴一舒 → E003 王彩秀
            G11-1 蔡雪雲 → G11  黃協湯
        去掉 -N 後綴再正規化編號，即可對應到主員工。
        """
        import re as _re

        self.cur.execute("""SELECT id, username FROM users
            WHERE COALESCE(staff_kind,'') = 'dependent'""")
        deps = self.cur.fetchall()
        if not deps:
            return

        # 建立 正規化編號 → id（僅主被保險人，排除眷屬本身）
        self.cur.execute("""SELECT id, username FROM users
            WHERE COALESCE(staff_kind,'') <> 'dependent'""")
        by_norm = {}
        for uid, uname in self.cur.fetchall():
            by_norm.setdefault(_norm_staff_no(uname), uid)

        updates = []
        for uid, uname in deps:
            m = _re.match(r'^(.*?)-\d+$', uname or '')
            if not m:
                continue
            parent = by_norm.get(_norm_staff_no(m.group(1)))
            if parent and parent != uid:
                updates.append((parent, uid))

        if updates:
            self.cur.executemany("UPDATE users SET dependent_of=? WHERE id=?", updates)
            self.conn.commit()
        print(f"  健保眷屬依附: {len(updates)}/{len(deps)} 筆對應到主被保險人")

    def _staff_reference_counts(self):
        """統計每個 Staff No 被工單引用的次數。

        這是判斷「誰是真的被指派的人員」最可靠的依據 —— 名稱與 Branch
        都可能誤導（Branch A-F 混了協力廠商），但被工單引用 21,969 次
        的王彩秀無疑是實際在做事的技師。
        """
        mh = self.data.get('Maintain Header')
        if not mh:
            return {}
        counts = defaultdict(int)
        for i in range(len(mh.get('No', []))):
            no = clean_str(col(mh, 'Staff No', i))
            if no:
                counts[no] += 1
        return counts

    def _staff_referenced_by_orders(self):
        """回傳被工單引用過的 Staff No 集合。"""
        return set(self._staff_reference_counts())

    def _alias_staff_ids(self):
        """把工單裡的 Staff No 變體對應到正確的 user id。

        實測工單引用的代碼有 E003 / e003 / E0003 / E03 / RE003 / `E003 等變體，
        全都是同一位技師。原樣比對會讓 21,969 筆工單的技師變成空白。
        """
        mh = self.data.get('Maintain Header')
        if not mh:
            return

        # username → id
        self.cur.execute("SELECT id, username FROM users")
        by_username = {}
        for uid, uname in self.cur.fetchall():
            by_username[uname] = uid

        # 建立 正規化 → id
        norm_map = {}
        for uname, uid in by_username.items():
            norm_map.setdefault(norm_staff_no(uname), uid)

        # 走訪所有工單引用過的代碼，補上別名
        seen = set()
        for i in range(len(mh.get('No', []))):
            raw = clean_str(col(mh, 'Staff No', i))
            if not raw or raw in seen:
                continue
            seen.add(raw)
            if raw in by_username:
                continue
            n = norm_staff_no(raw)
            if n in norm_map:
                self.user_id[raw] = norm_map[n]
        self.stats['staff_alias'] = len(self.user_id) - len(by_username)

    def _official_insurance_companies(self, used_codes):
        """從專用的 Insurance Company 表匯入保險公司（含統編、正式全名）。

        這張表才是真正的保險公司主檔（15 家），舊版漏掉它，
        導致 86 家廠商被誤灌進保險公司。
        """
        t = self.data.get('Insurance Company')
        if not t:
            return []
        rows = []
        for i in range(len(t.get('No', []))):
            no = clean_str(col(t, 'No', i))
            name = clean_str(col(t, 'Name', i))
            short = clean_str(col(t, 'Short Name', i))
            if not name and not no:
                continue
            code = short or no
            if not code or code in used_codes:
                continue

            addr_parts = [clean_str(col(t, f'Address-{n}', i)) for n in (1, 2)]
            addr = clean_address(*addr_parts)

            rows.append((
                code,                                   # code（簡稱，如「富邦」）
                name,                                   # 正式全名
                clean_str(col(t, 'Tax No', i)),         # tax_id
                clean_str(col(t, 'Telephone-1', i)),    # claim_phone（打報案用）
                clean_str(col(t, 'Telephone-2', i)),    # phone
                clean_str(col(t, 'Comm Name', i)),      # contact_person
                addr,                                   # address
                None,                                   # postal_code
                clean_str(col(t, 'Remark', i)),         # notes（含業務員手機等）
                1,
                no,                                     # legacy_no：舊編號（投保表用它對應）
            ))
            used_codes.add(code)
        print(f"  專用保險公司表: {len(rows)} 家（含統編）")
        return rows

    # ---------- 5. 零件庫存 ----------
    def migrate_inventory(self):
        print("\n[5] 零件 (Material → inventory_items)")
        t = self.data.get('Material')
        if not t:
            print("  無 Material 表，跳過")
            return

        self.cur.execute("SELECT part_number FROM inventory_items")
        used = {r[0] for r in self.cur.fetchall()}

        # CAR35 用 Material.Class（0/1/2）表示性質，Class No（單碼）表示類別。
        # 實測：Class=0 多為零件、1 為鈑金烤漆工項、2 為工資/服務項目。
        kind_map = {0: 'part', 1: 'labor', 2: 'labor'}

        rows = []
        cat_stat = defaultdict(int)
        for i in range(len(t['No'])):
            no = clean_str(col(t, 'No', i))
            name = clean_str(col(t, 'Name', i))
            if not no or no in used:
                continue

            firm_no = clean_str(col(t, 'Firm No-1', i)) or clean_str(col(t, 'Firm No-2', i))
            sid = self.sup_id.get(firm_no)

            # 零件類別：Class No 單碼 → 可讀名稱
            cn = (clean_str(col(t, 'Class No', i)) or '').upper()
            category = PART_CATEGORY.get(cn, '')
            cat_stat[category or '(未分類)'] += 1

            # 性質：0 零件 / 1-2 工資服務
            try:
                cval = int(t['Class'][i]) if t.get('Class') and t['Class'][i] is not None else 0
            except (TypeError, ValueError):
                cval = 0
            item_kind = kind_map.get(cval, 'part')

            rows.append((
                sid,                                            # supplier_id
                no,                                             # part_number (UNIQUE)
                None,                                           # barcode
                name or no,                                     # name
                clean_str(col(t, 'Store No', i)) or '',         # bin_location
                clean_num(col(t, 'In Price', i)),               # cost_price
                clean_num(col(t, 'Out Price', i)),              # selling_price
                clean_int(col(t, 'Current Amount', i)),         # stock_quantity
                clean_int(col(t, 'Safe Amount', i)) or 5,       # min_safety_stock
                category,                                       # category
                item_kind,                                      # item_kind
            ))
            used.add(no)

        if self.dry_run:
            print(f"  [DRY-RUN] 將新增 {len(rows)} 筆零件")
        else:
            self.cur.executemany("""
                INSERT INTO inventory_items
                  (supplier_id, part_number, barcode, name, bin_location,
                   cost_price, selling_price, stock_quantity, min_safety_stock,
                   category, item_kind)
                VALUES (?,?,?,?,?,?,?,?,?,?,?)
            """, rows)
            self.conn.commit()
            print(f"  新增 {len(rows)} 筆零件")
            print("  類別分布: " + "、".join(
                f"{k} {v}" for k, v in sorted(cat_stat.items(), key=lambda x: -x[1])[:8]))

        self.cur.execute("SELECT id, part_number FROM inventory_items")
        self.inv_id = {r[1]: r[0] for r in self.cur.fetchall()}
        self.stats['inventory'] = len(rows)

    # ---------- 6. 工單 ----------
    def migrate_work_orders(self):
        print("\n[6] 工單 (Maintain Header → work_orders)")
        t = self.data.get('Maintain Header')
        if not t:
            print("  無 Maintain Header 表，跳過")
            return

        n = len(t['No'])
        for i in range(n):
            car_no = clean_str(col(t, 'Car No', i))
            cust_no = clean_str(col(t, 'Customer No', i))

            vid = self.veh_id.get(car_no)
            cid = self.resolve_customer(cust_no)
            if vid is None or cid is None:
                continue

            pay_flag = clean_bool(col(t, 'Pay Flag', i))
            complete = convert_roc_date(col(t, 'Complete Date', i))
            consign = convert_roc_date(col(t, 'Consign Date', i))
            if pay_flag:
                status = 'paid'
            elif complete:
                status = 'completed'
            elif consign:
                status = 'in_progress'
            else:
                status = 'pending'

            date = convert_roc_date(col(t, 'Date', i))
            ins_flag = clean_bool(col(t, 'Insurance Flag', i))
            ins_name = clean_str(col(t, 'Insurance Name', i))

            values = (
                vid,                                        # vehicle_id
                status,                                     # status
                clean_int(col(t, 'Mileage', i)),            # current_mileage
                None,                                       # customer_complaint
                'not_required',                             # approval_status
                clean_num(col(t, 'Real Total', i)),         # total_amount
                0,                                          # sop_completed
                cid,                                        # customer_id
                self.user_id.get(clean_str(col(t, 'Staff No', i))),  # technician_id
                clean_str(col(t, 'Remark', i)),             # notes
                date,                                       # created_at
                date,                                       # updated_at
                None,                                       # insurance_company_id
                clean_str(col(t, 'Insurance Table No', i)) or '',  # claim_no
                ins_flag,                                   # is_insurance
                '',                                         # accident_date
                clean_num(col(t, 'Insurance Remark', i)),   # deductible_amount
            )

            if self.dry_run:
                self.wo_id[i] = -1
                continue

            self.cur.execute("""
                INSERT INTO work_orders
                  (vehicle_id, status, current_mileage, customer_complaint,
                   approval_status, total_amount, sop_completed, customer_id,
                   technician_id, notes, created_at, updated_at,
                   insurance_company_id, claim_no, is_insurance,
                   accident_date, deductible_amount)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """, values)
            self.wo_id[i] = self.cur.lastrowid
            # 舊單號 → work_orders.id。
            # 收款沖銷（Customer Pay.Maintain No）用舊單號關聯工單，
            # 而 work_orders 沒有存舊單號，所以必須在遷移當下就記下對照。
            no = clean_str(col(t, 'No', i))
            if no:
                self.wo_by_legacy_no[no] = self.wo_id[i]

        if self.dry_run:
            print(f"  [DRY-RUN] 將新增 {len(self.wo_id)} 筆工單")
        else:
            self.conn.commit()
            print(f"  新增 {len(self.wo_id)} 筆工單")
        self.stats['work_orders'] = len(self.wo_id)

    # ---------- 7. 工單明細 ----------
    def migrate_work_order_items(self):
        print("\n[7] 工單明細 (Maintain Detail → work_order_items)")
        md = self.data.get('Maintain Detail')
        mh = self.data.get('Maintain Header')
        if not md or not mh:
            print("  缺少 Maintain Detail/Header，跳過")
            return

        # 建立 工單編號 → 工單索引 佇列（處理重複編號）
        no_to_idx = defaultdict(list)
        for i in range(len(mh['No'])):
            no = clean_str(col(mh, 'No', i))
            if no:
                no_to_idx[no].append(i)

        # 序號 → 已分配到第幾個索引
        no_cursor = defaultdict(int)

        rows = []
        for i in range(len(md['No'])):
            no = clean_str(col(md, 'No', i))
            if not no:
                continue

            indices = no_to_idx.get(no)
            if not indices:
                continue

            # 依序分配，避免全部塞給同一張工單
            pos = min(no_cursor[no], len(indices) - 1)
            wo_idx = indices[pos]
            no_cursor[no] += 1

            wo_id = self.wo_id.get(wo_idx)
            if not wo_id or wo_id < 0:
                continue

            part_no = clean_str(col(md, 'Material No', i))
            remark = clean_str(col(md, 'Remark', i))
            price = clean_num(col(md, 'Price', i))
            qty = clean_num(col(md, 'Amount', i)) or 1

            # 判斷是零件或工資：能對到庫存 → part，否則 labor
            inv = self.inv_id.get(part_no) if part_no else None
            item_type = 'part' if inv else 'labor'

            rows.append((
                wo_id,                                      # work_order_id
                item_type,                                  # item_type
                inv,                                        # inventory_item_id
                part_no or remark or '未登錄',               # description
                int(qty),                                   # quantity
                price,                                      # unit_price
                price * qty,                                # subtotal
                self.user_id.get(clean_str(col(md, 'Staff No', i))),  # primary_technician_id
                None,                                       # assistant_technician_id
                None,                                       # photo_url
            ))

        if self.dry_run:
            print(f"  [DRY-RUN] 將新增 {len(rows)} 筆工單明細")
        else:
            BATCH = 10000
            for s in range(0, len(rows), BATCH):
                self.cur.executemany("""
                    INSERT INTO work_order_items
                      (work_order_id, item_type, inventory_item_id, description,
                       quantity, unit_price, subtotal, primary_technician_id,
                       assistant_technician_id, photo_url)
                    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """, rows[s:s + BATCH])
                self.conn.commit()
            print(f"  新增 {len(rows)} 筆工單明細")
        self.stats['work_order_items'] = len(rows)

    # ---------- 8. 查表匯出 ----------
    def export_lookups(self, path):
        print(f"\n[8] 匯出查表 → {path}")
        out = {}
        for name in LOOKUP_TABLES:
            t = self.data.get(name)
            if not t:
                continue
            n = len(next(iter(t.values()))) if t else 0
            rows = []
            for i in range(n):
                rows.append({k: clean_str(col(t, k, i)) for k in t})
            out[name] = rows
            print(f"  {name}: {len(rows)} 筆")

        os.makedirs(os.path.dirname(os.path.abspath(path)), exist_ok=True)
        with open(path, 'w', encoding='utf-8') as f:
            json.dump(out, f, ensure_ascii=False, indent=2)
        print(f"  已寫入 {path}")

    def _guard_append_only(self, table, expected):
        """append-only 表的防重複檢查。

        為什麼需要：vehicle_insurances / vehicle_inspections / expenses 這三張表
        在舊系統是完整歷史，遷移是「整批 append」。若在已有資料的庫上重跑
        （例如只補車輛卻連帶重跑全部），筆數會變成 2 倍或 3 倍，而且不會報錯
        —— 實測重跑一次就從 2,438 變 5,022 筆。

        這裡在寫入前檢查：已有資料且接近預期筆數時，直接跳過並提示，
        避免靜默地把歷史資料翻倍。要重新遷移請加 --reset。
        """
        have = 0
        try:
            self.cur.execute(f"SELECT COUNT(1) FROM {table}")
            have = self.cur.fetchone()[0]
        except sqlite3.Error:
            return False
        if have <= 0:
            return False
        # 容忍小幅差異（先前遷移可能因對應失敗少幾筆）
        if have >= expected * 0.9:
            print(f"  已有 {have:,} 筆（預期約 {expected:,}），跳過以免重複。"
                  f"要重新遷移請加 --reset")
            self.stats[table] = 0
            return True
        return False

    # ---------- 8. 車輛投保紀錄 ----------
    def migrate_vehicle_insurances(self):
        """Car Insurance List → vehicle_insurances（2,667 筆）。

        舊系統用 Company No 參照保險公司，但該編號與 Insurance Company 表的
        No 不完全一致，因此同時保留 company_no 原文，能對應才填 company_id。
        """
        print("\n[8] 車輛投保 (Car Insurance List → vehicle_insurances)")
        t = self.data.get('Car Insurance List')
        if not t:
            print("  無 Car Insurance List 表，跳過")
            return
        if not self.dry_run and self._guard_append_only('vehicle_insurances', len(t['No'])):
            return

        # 險種代碼 → 中文（Insurance Class 查表）
        ic = self.data.get('Insurance Class', {})
        class_map = {}
        for i in range(len(ic.get('No', []))):
            class_map[(clean_str(col(ic, 'No', i)) or '').upper()] = clean_str(col(ic, 'Name', i))

        # 保險公司編號 → id
        #
        # 舊系統有兩套編號：主檔的 No（'07'）與 code（簡稱「泰安產物」）。
        # Car Insurance List 的 Company No 用的是主檔 No，因此要用 legacy_no 對應。
        self.cur.execute("SELECT id, code, COALESCE(legacy_no,'') FROM insurance_companies")
        comp_map = {}
        for cid, code, legacy in self.cur.fetchall():
            comp_map[clean_str(code)] = cid
            if legacy:
                comp_map[clean_str(legacy)] = cid

        # 車牌 → vehicle id
        plate_map = {}
        self.cur.execute("SELECT id, plate_number FROM vehicles")
        for vid, plate in self.cur.fetchall():
            plate_map[plate] = vid

        rows = []
        skip_no_vehicle = 0
        for i in range(len(t['No'])):
            plate = clean_str(col(t, 'No', i))
            vid = plate_map.get(plate)
            if not vid:
                skip_no_vehicle += 1
                continue

            comp_no = clean_str(col(t, 'Company No', i)) or ''
            cls = (clean_str(col(t, 'Insurance Class', i)) or '').upper()

            rows.append((
                vid,
                comp_map.get(comp_no),                          # insurance_company_id
                comp_no,                                        # company_no（原文保留）
                cls,                                            # insurance_class
                class_map.get(cls, ''),                         # insurance_class_name
                convert_roc_date(col(t, 'License Date', i)),    # license_date
                clean_str(col(t, 'Card No', i)),                # card_no
                clean_str(col(t, 'Remark', i)),                 # amount_raw
                None,                                           # notes
            ))

        if self.dry_run:
            print(f"  [DRY-RUN] 將新增 {len(rows)} 筆投保紀錄（{skip_no_vehicle} 筆無對應車輛）")
        else:
            self.cur.executemany("""
                INSERT INTO vehicle_insurances
                  (vehicle_id, insurance_company_id, company_no, insurance_class,
                   insurance_class_name, license_date, card_no, amount_raw, notes)
                VALUES (?,?,?,?,?,?,?,?,?)
            """, rows)
            self.conn.commit()
            print(f"  新增 {len(rows)} 筆投保紀錄（{skip_no_vehicle} 筆無對應車輛）")
        self.stats['vehicle_insurances'] = len(rows)

    # ---------- 9. 車輛定檢排程 ----------
    def migrate_vehicle_inspections(self):
        """Car Check List → vehicle_inspections（21,176 筆）。

        舊系統只記錄「排定檢驗日」，Check Date 幾乎全空（只有 3 筆），
        所以 check_date 為空代表「尚未完成檢驗」。
        """
        print("\n[9] 定檢排程 (Car Check List → vehicle_inspections)")
        t = self.data.get('Car Check List')
        if not t:
            print("  無 Car Check List 表，跳過")
            return
        if not self.dry_run and self._guard_append_only('vehicle_inspections', len(t['No'])):
            return

        plate_map = {}
        self.cur.execute("SELECT id, plate_number FROM vehicles")
        for vid, plate in self.cur.fetchall():
            plate_map[plate] = vid

        rows = []
        skip_no_vehicle = 0
        for i in range(len(t['No'])):
            plate = clean_str(col(t, 'No', i))
            vid = plate_map.get(plate)
            if not vid:
                skip_no_vehicle += 1
                continue
            rows.append((
                vid,
                convert_roc_date(col(t, 'Order Date', i)),
                convert_roc_date(col(t, 'Check Date', i)),
                clean_str(col(t, 'Firm No', i)),
                clean_str(col(t, 'Remark', i)),
            ))

        if self.dry_run:
            print(f"  [DRY-RUN] 將新增 {len(rows)} 筆定檢排程（{skip_no_vehicle} 筆無對應車輛）")
        else:
            BATCH = 5000
            for s in range(0, len(rows), BATCH):
                self.cur.executemany("""
                    INSERT INTO vehicle_inspections
                      (vehicle_id, order_date, check_date, firm_no, notes)
                    VALUES (?,?,?,?,?)
                """, rows[s:s + BATCH])
                self.conn.commit()
            print(f"  新增 {len(rows)} 筆定檢排程（{skip_no_vehicle} 筆無對應車輛）")
        self.stats['vehicle_inspections'] = len(rows)

    # ---------- 10. 保養套餐範本 ----------
    def migrate_maintenance_templates(self):
        """Maintain Record Header/Detail → maintenance_templates(_items)。

        開工單時可一鍵套用標準項目（如「5千公里定期保養」）。
        """
        print("\n[10] 保養套餐 (Maintain Record * → maintenance_templates)")
        h = self.data.get('Maintain Record Header')
        if not h:
            print("  無 Maintain Record Header 表，跳過")
            return
        d = self.data.get('Maintain Record Detail', {})

        self.cur.execute("SELECT code FROM maintenance_templates")
        used = {r[0] for r in self.cur.fetchall()}

        tpl_rows = []
        for i in range(len(h['No'])):
            code = clean_str(col(h, 'No', i))
            name = clean_str(col(h, 'Name', i))
            if not code or code in used:
                continue
            tpl_rows.append((code, name or code, clean_str(col(h, 'Remark', i))))
            used.add(code)

        if self.dry_run:
            print(f"  [DRY-RUN] 將新增 {len(tpl_rows)} 個套餐")
        else:
            if tpl_rows:
                self.cur.executemany(
                    "INSERT INTO maintenance_templates (code, name, notes) VALUES (?,?,?)",
                    tpl_rows)
                self.conn.commit()

        # code → template id
        self.cur.execute("SELECT id, code FROM maintenance_templates")
        tpl_map = {code: tid for tid, code in self.cur.fetchall()}

        item_rows = []
        for i in range(len(d.get('No', []))):
            tcode = clean_str(col(d, 'No', i))
            tid = tpl_map.get(tcode)
            if not tid:
                continue
            part = clean_str(col(d, 'Material No', i))
            item_rows.append((
                tid,
                clean_int(col(d, 'Sequence No', i)),
                part or '',
                part or '',                                 # description 先帶料號
                clean_num(col(d, 'Amount', i)) or 1,        # quantity
                clean_num(col(d, 'Price', i)),              # unit_price
            ))

        if self.dry_run:
            print(f"  [DRY-RUN] 將新增 {len(item_rows)} 個套餐項目")
        else:
            if item_rows:
                self.cur.executemany("""
                    INSERT INTO maintenance_template_items
                      (template_id, sequence_no, part_number, description, quantity, unit_price)
                    VALUES (?,?,?,?,?,?)
                """, item_rows)
                self.conn.commit()
            print(f"  新增 {len(tpl_rows)} 個套餐、{len(item_rows)} 個項目")
        self.stats['maintenance_templates'] = len(tpl_rows)
        self.stats['maintenance_template_items'] = len(item_rows)

    # ---------- 11. 零用金／雜支支出 ----------
    # ---------- 12. 收款沖銷 ----------
    def migrate_receivables(self):
        """Customer Pay → receivables（90,762 筆）。

        舊系統的「應收帳款沖銷作業」。這是金額最大的缺口：
        現金 3.48 億、刷卡 4,417 萬、支票 714 萬。

        金額單位是「元」（實測 Cash Total 500 = 500 元，與工單同級），
        不是「分」—— 這點與 Spend List 不同，勿混淆。

        對應關係：
          customer_id    ← Customer No（用 resolve_customer，含姓名 fallback）
          vehicle_id     ← Car No
          work_order_id  ← Maintain No（90,759 筆可對到工單）
          voided         ← Delete Flag（9 筆作廢）
          source         = 'legacy'（讓日後能只清舊帳重跑）
        """
        print("\n[12] 收款沖銷 (Customer Pay → receivables)")
        t = self.data.get('Customer Pay')
        if not t:
            print("  無 Customer Pay 表，跳過")
            return
        if not self.dry_run and self._guard_append_only('receivables', len(t['No'])):
            return

        # 車牌 → vehicle id
        plate_map = {}
        self.cur.execute("SELECT id, plate_number FROM vehicles")
        for vid, plate in self.cur.fetchall():
            plate_map[plate] = vid

        # 舊工單號 → work_orders.id
        #
        # work_orders 沒有存舊系統的單號，所以對照表是在 migrate_work_orders
        # 寫入時順手記下的（見該函式的 wo_by_legacy_no）。這裡只取用。
        # 對不到就留空 —— 收款紀錄本身仍有價值（金額與客戶都對得上），
        # 硬猜一個工單反而會把錢記到錯的帳上。
        wo_map = self.wo_by_legacy_no

        rows = []
        skip_no_cust = 0
        no_wo = 0
        for i in range(len(t['No'])):
            cust_no = clean_str(col(t, 'Customer No', i))
            cid = self.resolve_customer(cust_no)
            if cid is None:
                skip_no_cust += 1
                continue

            plate = clean_str(col(t, 'Car No', i))
            vid = plate_map.get(plate)

            legacy_wo = clean_str(col(t, 'Maintain No', i))
            wid = wo_map.get(legacy_wo)
            if legacy_wo and wid is None:
                no_wo += 1

            rows.append((
                clean_str(col(t, 'No', i)) or '',                 # voucher_no
                cid,                                              # customer_id
                vid,                                              # vehicle_id
                wid,                                              # work_order_id
                # pay_date 是 NOT NULL，但舊資料有 5 筆空值 →
                # convert_roc_date 會回 None，直接塞會違反約束。留空字串。
                convert_roc_date(col(t, 'Pay Date', i)) or '',    # pay_date
                clean_num(col(t, 'Detail Total', i)),             # detail_total
                clean_num(col(t, 'Real Total', i)),               # real_total
                clean_num(col(t, 'Discount Price', i)),           # discount
                clean_num(col(t, 'Tax Price', i)),                # tax
                clean_num(col(t, 'Pay Total', i)),                # pay_total
                clean_num(col(t, 'Cash Total', i)),               # cash
                clean_num(col(t, 'Card Total', i)),               # card
                clean_num(col(t, 'Check Total', i)),              # check_amount
                clean_str(col(t, 'Card No', i)),                  # card_no
                clean_str(col(t, 'Check No', i)),                 # check_no
                # 舊資料的 Check Date 是 '1000131'（無分隔符），
                # convert_roc_date 解析不了會回 None；此欄可空，沒問題。
                convert_roc_date(col(t, 'Check Date', i)) or '',  # check_date
                clean_str(col(t, 'Bank Class', i)),               # bank
                clean_str(col(t, 'Pay Remark', i)),               # notes
                1 if clean_bool(col(t, 'Delete Flag', i)) else 0,  # voided
                clean_str(col(t, 'No', i)) or '',                 # legacy_no
            ))

        if self.dry_run:
            print(f"  [DRY-RUN] 將新增 {len(rows):,} 筆收款"
                  f"（{skip_no_cust} 筆無客戶、{no_wo} 筆無對應工單）")
        else:
            self.cur.executemany("""
                INSERT INTO receivables
                  (voucher_no, customer_id, vehicle_id, work_order_id, pay_date,
                   detail_total, real_total, discount, tax, pay_total,
                   cash, card, check_amount, card_no, check_no, check_date, bank,
                   notes, voided, source, legacy_no)
                VALUES (?,?,?,?,?, ?,?,?,?,?, ?,?,?,?,?,?,?, ?,?,'legacy',?)
            """, rows)
            self.conn.commit()
            print(f"  新增 {len(rows):,} 筆收款"
                  f"（{skip_no_cust} 筆無客戶、{no_wo} 筆無對應工單）")
        self.stats['receivables'] = len(rows)

    # ---------- 13. 技師分工回填 ----------
    def migrate_technician_worklogs(self):
        """Maintain Detail 的 Staff No → technician_worklogs。

        舊系統 231,188/390,425 筆明細有 Staff No，但 technician_worklogs 只有 32 筆。
        對不到 user 的 Staff No 自動建立影子用戶（staff_kind = 'legacy_tech'）。
        """
        print("\n[13] 技師分工 (Maintain Detail → technician_worklogs)")
        md = self.data.get('Maintain Detail')
        mh = self.data.get('Maintain Header')
        if not md or not mh:
            print("  缺少 Maintain Detail/Header，跳過")
            return

        # 建立 工單編號 → 工單索引 佇列
        no_to_idx = defaultdict(list)
        for i in range(len(mh['No'])):
            no = clean_str(col(mh, 'No', i))
            if no:
                no_to_idx[no].append(i)
        no_cursor = defaultdict(int)

        # 收集所有需要的 Staff No
        needed_staff = set()
        wo_id_found = 0
        wo_id_miss = 0
        for i in range(len(md['No'])):
            no = clean_str(col(md, 'No', i))
            if not no:
                continue
            indices = no_to_idx.get(no)
            if not indices:
                continue
            pos = min(no_cursor[no], len(indices) - 1)
            wo_idx = indices[pos]
            no_cursor[no] += 1
            wo_id = self.wo_id.get(wo_idx)
            if not wo_id:
                wo_id_miss += 1
                continue
            if wo_id < 0 and not self.dry_run:
                wo_id_miss += 1
                continue
            wo_id_found += 1
            staff_no = clean_str(col(md, 'Staff No', i))
            if staff_no:
                needed_staff.add(staff_no)
        print(f"  wo_id 命中: {wo_id_found}, 未命中: {wo_id_miss}")
        print(f"  needed_staff: {len(needed_staff)}")

        # 建立影子用戶
        shadow_users = []
        for staff_no in needed_staff:
            if staff_no not in self.user_id:
                shadow_users.append((
                    staff_no,
                    'legacy',
                    'technician',
                    staff_no,
                    '',
                    'legacy_tech',
                    None, None, None, None, None,
                ))

        if self.dry_run:
            print(f"  [DRY-RUN] 將建立 {len(shadow_users)} 個影子用戶")
        else:
            if shadow_users:
                self.cur.executemany("""
                    INSERT INTO users (username, password, role, name, staff_branch,
                       staff_kind, national_id, birthday, insurance_start,
                       insurance_end, bank_account)
                    VALUES (?,?,?,?,?,?,?,?,?,?,?)
                """, shadow_users)
                self.conn.commit()
                # 更新 user_id 映射
                self.cur.execute("SELECT id, username FROM users")
                for uid, username in self.cur.fetchall():
                    self.user_id[username] = uid
            print(f"  建立 {len(shadow_users)} 個影子用戶")

        # 建立 work_order_id → staff_no 映射
        wo_staff = {}  # work_order_id → staff_no
        no_cursor2 = defaultdict(int)
        staff_no_count = 0
        for i in range(len(md['No'])):
            no = clean_str(col(md, 'No', i))
            if not no:
                continue
            indices = no_to_idx.get(no)
            if not indices:
                continue
            pos = min(no_cursor2[no], len(indices) - 1)
            wo_idx = indices[pos]
            no_cursor2[no] += 1
            wo_id = self.wo_id.get(wo_idx)
            if not wo_id:
                continue
            if wo_id < 0 and not self.dry_run:
                continue
            staff_no = clean_str(col(md, 'Staff No', i))
            if staff_no:
                staff_no_count += 1
                wo_staff[wo_id] = staff_no
        print(f"  staff_no_count: {staff_no_count}")

        # 更新 work_order_items 的 primary_technician_id
        print(f"  wo_staff: {len(wo_staff)}")
        updates = []
        for wo_id, staff_no in wo_staff.items():
            tech_id = self.user_id.get(staff_no)
            if tech_id:
                updates.append((tech_id, wo_id))
        print(f"  updates: {len(updates)}")

        if self.dry_run:
            print(f"  [DRY-RUN] 將更新 {len(updates)} 筆 work_order_items")
        else:
            if updates:
                # 批量更新：每個 work_order_id 的所有 items 都更新
                self.cur.executemany("""
                    UPDATE work_order_items
                    SET primary_technician_id = ?
                    WHERE work_order_id = ? AND primary_technician_id IS NULL
                """, updates)
                self.conn.commit()
            print(f"  更新 {len(updates)} 筆 work_order_items")

        self.stats['technician_worklogs'] = len(updates)

    def migrate_expenses(self):
        """Spend List → expenses（3,390 筆）。

        金額沿用舊系統原值（分），依指示不換算。
        """
        print("\n[11] 零用金支出 (Spend List → expenses)")
        t = self.data.get('Spend List')
        if not t:
            print("  無 Spend List 表，跳過")
            return
        if not self.dry_run and self._guard_append_only('expenses', len(t.get('Sequence No', []))):
            return

        key = 'Sequence No'
        n = len(t.get(key, []))
        rows = []
        for i in range(n):
            date = clean_str(col(t, 'Date', i))
            # 舊資料是 2 碼年（88/01/01 = 民國 88 年）→ 轉西元
            d = convert_roc_date(date) if date else None
            rows.append((
                clean_int(col(t, 'Sequence No', i)),
                d,
                clean_num(col(t, 'In Price', i)),
                clean_num(col(t, 'Out Price', i)),
                clean_str(col(t, 'Material No', i)),
                clean_str(col(t, 'Remark', i)),
            ))

        if self.dry_run:
            print(f"  [DRY-RUN] 將新增 {len(rows)} 筆支出")
        else:
            BATCH = 2000
            for s in range(0, len(rows), BATCH):
                self.cur.executemany("""
                    INSERT INTO expenses
                      (sequence_no, spend_date, amount_in, amount_out, part_number, notes)
                    VALUES (?,?,?,?,?,?)
                """, rows[s:s + BATCH])
                self.conn.commit()
            print(f"  新增 {len(rows)} 筆支出（金額沿用原單位）")
        self.stats['expenses'] = len(rows)

    # ---------- 清理 ----------
    def reset(self, keep_orders=7, keep_items=21):
        """清空先前遷移的資料，保留測試資料。"""
        print("\n[重置] 清空既有遷移資料")
        self.cur.execute("DELETE FROM work_order_items WHERE id > ?", (keep_items,))
        print(f"  work_order_items: -{self.cur.rowcount}")
        self.cur.execute("DELETE FROM work_orders WHERE id > ?", (keep_orders,))
        print(f"  work_orders: -{self.cur.rowcount}")
        self.cur.execute("DELETE FROM vehicles WHERE id > 1")
        print(f"  vehicles: -{self.cur.rowcount}")
        self.cur.execute("DELETE FROM customers WHERE id > 1")
        print(f"  customers: -{self.cur.rowcount}")
        self.cur.execute("DELETE FROM suppliers")
        print(f"  suppliers: -{self.cur.rowcount}")
        self.cur.execute("DELETE FROM inventory_items WHERE id > 1")
        print(f"  inventory_items: -{self.cur.rowcount}")
        self.cur.execute("DELETE FROM inventory_movements")
        print(f"  inventory_movements: -{self.cur.rowcount}")
        # 新遷移的表（整表重建，無測試資料需保留）
        for tbl in ('vehicle_insurances', 'vehicle_inspections',
                    'maintenance_template_items', 'maintenance_templates',
                    'expenses'):
            self.cur.execute(f"DELETE FROM {tbl}")
            print(f"  {tbl}: -{self.cur.rowcount}")
        self.cur.execute("DELETE FROM users WHERE password = 'legacy'")
        print(f"  users(legacy): -{self.cur.rowcount}")
        # 清掉舊系統匯入的保險公司，只保留 seed 建立的示範資料（ACME-001）。
        #
        # 不能用 notes 條件判斷（舊版這樣做，導致 16 家沒被清掉 → 重跑時
        # 去重邏輯全部跳過 → legacy_no 永遠填不進去 → 投保紀錄對不到保險公司）。
        # 改用「非 seed 代碼」判定：seed 只建 ACME-001。
        self.cur.execute("DELETE FROM insurance_companies WHERE code <> 'ACME-001'")
        print(f"  insurance_companies(舊資料): -{self.cur.rowcount}")
        self.conn.commit()

    def run(self, lookups_path=None):
        self.migrate_customers()
        # 客戶全部就位後才建解析器：車輛與工單都要用它找車主。
        self._build_customer_resolver()
        self.migrate_vehicles()
        self.migrate_suppliers()
        self.migrate_staff()
        self.migrate_inventory()
        self.migrate_work_orders()
        self.migrate_work_order_items()
        self.migrate_vehicle_insurances()
        self.migrate_vehicle_inspections()
        self.migrate_maintenance_templates()
        self.migrate_expenses()
        self.migrate_receivables()
        self.migrate_technician_worklogs()
        if lookups_path:
            self.export_lookups(lookups_path)

        # 客戶解析路徑統計：讓「姓名 fallback 救回多少」可被稽核，
        # 而不是只有一個總筆數看不出來源。
        if sum(self.resolve_via.values()) > 0:
            print("\n客戶解析路徑統計：")
            print(f"  notes 精確命中 : {self.resolve_via['notes']:,}")
            print(f"  姓名 fallback  : {self.resolve_via['name']:,}")
            print(f"  對不到（略過） : {self.resolve_via['miss']:,}")

        if not self.dry_run:
            self.cur.execute("PRAGMA wal_checkpoint(TRUNCATE)")
        self.conn.close()

        print("\n" + "=" * 50)
        print("遷移完成")
        print("=" * 50)
        for k, v in self.stats.items():
            print(f"  {k:<20} {v}")
        if self.dry_run:
            print("\n(此為 DRY-RUN，未實際寫入)")


# ============================================================
# 主程式
# ============================================================

def main():
    ap = argparse.ArgumentParser(
        description='AutoRepair Access → SQLite 遷移工具',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__)
    ap.add_argument('access_mdb', help='Access 資料庫路徑 (.mdb)')
    ap.add_argument('sqlite_db', help='SQLite 資料庫路徑 (.db)')
    ap.add_argument('--dry-run', action='store_true', help='模擬執行，不寫入')
    ap.add_argument('--backup', action='store_true', help='遷移前備份 SQLite')
    ap.add_argument('--reset', action='store_true', help='先清空既有遷移資料')
    ap.add_argument('--lookups', metavar='PATH', help='匯出查表 JSON 的路徑')
    args = ap.parse_args()

    for path, label in ((args.access_mdb, 'Access'), (args.sqlite_db, 'SQLite')):
        if not os.path.exists(path):
            print(f"錯誤: {label} 資料庫不存在: {path}")
            sys.exit(1)

    if args.backup and not args.dry_run:
        stamp = datetime.now().strftime('%Y%m%d_%H%M%S')
        backup = f"{args.sqlite_db}.backup_{stamp}"
        shutil.copy2(args.sqlite_db, backup)
        print(f"已備份 → {backup}")

    print(f"讀取 Access: {args.access_mdb}")
    data = extract_tables(args.access_mdb, ALL_TABLES)

    m = Migrator(data, args.sqlite_db, dry_run=args.dry_run)
    if args.reset and not args.dry_run:
        m.reset()
    m.run(lookups_path=args.lookups)


if __name__ == '__main__':
    main()
