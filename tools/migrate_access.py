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
LOOKUP_TABLES = ['Car Class', 'Gas Class', 'Store', 'Branch', 'Class', 'Bank']

ALL_TABLES = MASTER_TABLES + TX_TABLES + LOOKUP_TABLES


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

        self.cur.execute("SELECT plate_number FROM vehicles")
        used = {r[0] for r in self.cur.fetchall()}

        rows = []
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

            rows.append((
                cid,                                            # customer_id
                plate,                                          # plate_number (UNIQUE)
                brand_model,                                    # brand_model
                clean_year(col(t, 'Year', i)),                  # manufacture_year
                clean_str(col(t, 'Body No', i)),                # vin
                clean_str(col(t, 'Engine No', i)),              # engine_number
                convert_roc_date(col(t, 'Vaild Date', i)),      # inspection_due_date
                convert_roc_date(col(t, 'Order Maintain Date', i)),  # next_maintenance_date
                clean_int(col(t, 'Last Mileage', i)) or None,   # next_maintenance_mileage
            ))
            used.add(plate)

        if self.dry_run:
            print(f"  [DRY-RUN] 將新增 {len(rows)} 筆車輛")
        else:
            self.cur.executemany("""
                INSERT INTO vehicles
                  (customer_id, plate_number, brand_model, manufacture_year, vin,
                   engine_number, inspection_due_date, next_maintenance_date,
                   next_maintenance_mileage)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
            """, rows)
            self.conn.commit()
            print(f"  新增 {len(rows)} 筆車輛")

        self.cur.execute("SELECT id, plate_number FROM vehicles")
        self.veh_id = {r[1]: r[0] for r in self.cur.fetchall()}
        self.stats['vehicles'] = len(rows)

    # ---------- 3. 廠商 ----------
    def migrate_suppliers(self):
        print("\n[3] 廠商 (Firm → suppliers)")
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
            address = ''.join(p for p in addr_parts if p) or None

            rows.append((
                key,                                            # name
                clean_str(col(t, 'Telephone-1', i)),            # phone
                clean_str(col(t, 'Sales Name', i)),             # contact_person
                address,                                        # address
                f"舊編號:{no}" if no else None,                 # notes
            ))
            used.add(key)

        if self.dry_run:
            print(f"  [DRY-RUN] 將新增 {len(rows)} 筆廠商")
        else:
            self.cur.executemany("""
                INSERT INTO suppliers (name, phone, contact_person, address, notes)
                VALUES (?, ?, ?, ?, ?)
            """, rows)
            self.conn.commit()
            print(f"  新增 {len(rows)} 筆廠商")

        self.cur.execute("SELECT id, name, notes FROM suppliers")
        for sid, name, notes in self.cur.fetchall():
            if notes and notes.startswith('舊編號:'):
                self.sup_id[notes[4:]] = sid
            self.sup_id.setdefault(name, sid)
        self.stats['suppliers'] = len(rows)

    # ---------- 4. 員工 / 保險公司 ----------
    def migrate_staff(self):
        print("\n[4] 員工/保險 (Staff → users / insurance_companies)")
        t = self.data.get('Staff')
        if not t:
            print("  無 Staff 表，跳過")
            return

        # 部門代碼 → 判斷是否為保險公司
        branch = self.data.get('Branch', {})
        branch_map = {}
        for i in range(len(branch.get('No', []))):
            branch_map[clean_str(col(branch, 'No', i))] = clean_str(col(branch, 'Name', i))

        self.cur.execute("SELECT username FROM users")
        used_users = {r[0] for r in self.cur.fetchall()}
        self.cur.execute("SELECT code, name FROM insurance_companies")
        used_ins = set()
        for code, name in self.cur.fetchall():
            used_ins.add(code)
            used_ins.add(name)

        user_rows, ins_rows = [], []
        for i in range(len(t['No'])):
            no = clean_str(col(t, 'No', i))
            name = clean_str(col(t, 'Name', i))
            if not name and not no:
                continue
            key = name or no

            dept = clean_str(col(t, 'Branch No', i))
            dept_name = branch_map.get(dept, '')

            # 部門含「保險」或「廠商」→ 保險公司；否則視為員工
            if dept_name and ('保險' in dept_name or '廠商' in dept_name):
                if key in used_ins:
                    continue
                ins_rows.append((
                    no or name,                     # code (UNIQUE)
                    name or no,                     # name
                    clean_str(col(t, 'Id No', i)),  # tax_id
                    clean_str(col(t, 'Telephone', i)),  # claim_phone
                    clean_str(col(t, 'Telephone', i)),  # phone
                    None,                           # contact_person
                    clean_str(col(t, 'Address', i)),# address
                    None,                           # postal_code
                    f"部門:{dept_name}" if dept_name else None,  # notes
                    1,                              # active
                ))
                used_ins.add(key)
            else:
                username = no or name
                if username in used_users:
                    continue
                user_rows.append((
                    username,                       # username (UNIQUE)
                    'legacy',                       # password (舊資料無密碼，需重設)
                    'technician',                   # role
                    name or no,                     # name
                ))
                used_users.add(username)

        if self.dry_run:
            print(f"  [DRY-RUN] 將新增 {len(user_rows)} 位員工, {len(ins_rows)} 家保險公司")
        else:
            if user_rows:
                self.cur.executemany("""
                    INSERT INTO users (username, password, role, name)
                    VALUES (?, ?, ?, ?)
                """, user_rows)
            if ins_rows:
                self.cur.executemany("""
                    INSERT INTO insurance_companies
                      (code, name, tax_id, claim_phone, phone, contact_person,
                       address, postal_code, notes, active)
                    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """, ins_rows)
            self.conn.commit()
            print(f"  新增 {len(user_rows)} 位員工, {len(ins_rows)} 家保險公司")

        self.cur.execute("SELECT id, username FROM users")
        for uid, username in self.cur.fetchall():
            self.user_id[username] = uid
        self.stats['users'] = len(user_rows)
        self.stats['insurance'] = len(ins_rows)

    # ---------- 5. 零件庫存 ----------
    def migrate_inventory(self):
        print("\n[5] 零件 (Material → inventory_items)")
        t = self.data.get('Material')
        if not t:
            print("  無 Material 表，跳過")
            return

        self.cur.execute("SELECT part_number FROM inventory_items")
        used = {r[0] for r in self.cur.fetchall()}

        rows = []
        for i in range(len(t['No'])):
            no = clean_str(col(t, 'No', i))
            name = clean_str(col(t, 'Name', i))
            if not no or no in used:
                continue

            firm_no = clean_str(col(t, 'Firm No-1', i)) or clean_str(col(t, 'Firm No-2', i))
            sid = self.sup_id.get(firm_no)

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
            ))
            used.add(no)

        if self.dry_run:
            print(f"  [DRY-RUN] 將新增 {len(rows)} 筆零件")
        else:
            self.cur.executemany("""
                INSERT INTO inventory_items
                  (supplier_id, part_number, barcode, name, bin_location,
                   cost_price, selling_price, stock_quantity, min_safety_stock)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
            """, rows)
            self.conn.commit()
            print(f"  新增 {len(rows)} 筆零件")

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
        self.cur.execute("DELETE FROM users WHERE password = 'legacy'")
        print(f"  users(legacy): -{self.cur.rowcount}")
        self.cur.execute("DELETE FROM insurance_companies WHERE id > 4")
        print(f"  insurance_companies: -{self.cur.rowcount}")
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
