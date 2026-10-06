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
             'Material Header', 'Material Detail', 'Customer Pay']

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
    容忍格式異常（如 '106/11/3'、'108/02/2'）。"""
    d = clean_str(d)
    if not d:
        return None
    parts = d.split('/')
    if len(parts) == 3:
        try:
            y = int(parts[0])
            m = int(parts[1])
            day = int(parts[2])
            if 1 <= m <= 12 and 1 <= day <= 31 and 1 <= y <= 200:
                return f"{y + 1911:04d}/{m:02d}/{day:02d}"
        except ValueError:
            pass
    return None


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
        self.cust_id = {}       # Customer.No  → customers.id
        self.veh_id = {}        # Car.No       → vehicles.id
        self.sup_id = {}        # Firm.No      → suppliers.id
        self.user_id = {}       # Staff.No     → users.id
        self.inv_id = {}        # Material.No  → inventory_items.id
        self.wo_id = {}         # Maintain Header index → work_orders.id

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
            cid = self.cust_id.get(cust_no)
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
                       address, postal_code, notes, active)
                    VALUES (?,?,?,?,?,?,?,?,?,?)
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
            cid = self.cust_id.get(cust_no)
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
        self.cur.execute("DELETE FROM users WHERE password = 'legacy'")
        print(f"  users(legacy): -{self.cur.rowcount}")
        # 只保留 seed 建立的非舊資料保險公司，其餘（含被誤灌的廠商）全部清掉
        self.cur.execute("DELETE FROM insurance_companies WHERE COALESCE(notes,'') LIKE '%舊系統%' OR COALESCE(notes,'') LIKE '%部門:%' OR code IN ('005','01','02','05','07','08','10','12','13','14')")
        print(f"  insurance_companies(舊資料): -{self.cur.rowcount}")
        self.conn.commit()

    def run(self, lookups_path=None):
        self.migrate_customers()
        self.migrate_vehicles()
        self.migrate_suppliers()
        self.migrate_staff()
        self.migrate_inventory()
        self.migrate_work_orders()
        self.migrate_work_order_items()
        if lookups_path:
            self.export_lookups(lookups_path)

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
