#!/usr/bin/env python3
"""Access 匯入工具冪等性守門測試（review T3.1 / T3.2 / T3.3）。

不需要 Access Parser：filter_new_rows 與 Migrator.reset 只吃 dict + sqlite conn，
因此用假資料與臨時 SQLite 庫即可覆蓋「重跑不重複、修改列不重複、reset 清收款」。

其中 test_old_hash_would_duplicate 刻意重現修正前的整列 hash 法，斷言它會把
「已存在但被修改」的列當成新資料保留 —— 證明本測試有鑑別力（先看它紅過，
再確認新程式碼把它壓掉），不是恆真斷言。

執行：cd unpublished/analysis-and-tests/tools && python -m unittest test_import_idempotency -v
"""

import os
import sqlite3
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import access_import_common as aid  # noqa: E402
import migrate_access as ma  # noqa: E402


def _base_schema(conn):
    """filter_new_rows 測試用的最小 schema：兩張交易表 + state。"""
    conn.executescript(
        """
        CREATE TABLE work_orders (
            id INTEGER PRIMARY KEY,
            total_amount REAL,
            legacy_no TEXT DEFAULT ''
        );
        CREATE TABLE receivables (
            id INTEGER PRIMARY KEY,
            pay_total REAL,
            legacy_no TEXT DEFAULT ''
        );
        CREATE TABLE legacy_import_state (
            table_name TEXT NOT NULL,
            row_hash TEXT NOT NULL,
            source_key TEXT NOT NULL DEFAULT '',
            imported_at TEXT NOT NULL,
            PRIMARY KEY (table_name, row_hash)
        );
        """
    )
    conn.commit()


def _seed_work_orders(conn, legacy_nos, totals):
    conn.executemany(
        "INSERT INTO work_orders (total_amount, legacy_no) VALUES (?, ?)",
        list(zip(totals, legacy_nos)),
    )
    conn.commit()


def _wo_data(nos, totals):
    """work_orders 的來源資料（column → list，與 extract_tables 同形）。"""
    return {
        "work_orders": {
            "No": list(nos),
            "Real Total": list(totals),
        }
    }


def _old_hash_keep(module, data, conn):
    """重現『修正前』的整列 hash 去重，回傳保留的 work_orders 來源索引。"""
    seen = {
        (r[0], r[1])
        for r in conn.execute("SELECT table_name, row_hash FROM legacy_import_state")
    }
    table = data["work_orders"]
    return [
        i for i in range(aid.row_count(table))
        if ("work_orders", aid.row_hash(table, i)) not in seen
    ]


class FilterNewRowsIdempotencyTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.db = os.path.join(self.tmp, "t.db")
        self.conn = sqlite3.connect(self.db)
        _base_schema(self.conn)

    def tearDown(self):
        self.conn.close()

    def test_modified_existing_row_is_not_duplicated(self):
        """T3.1：已存在列即使內容被修改，也不應被當新資料保留。"""
        # DB 裡已有 WO1(100)、WO2(200)；來源把 WO1 改成 999，並新增 WO3。
        _seed_work_orders(self.conn, ["WO1", "WO2"], [100.0, 200.0])
        data = _wo_data(["WO1", "WO2", "WO3"], [999.0, 200.0, 300.0])

        kept = aid.filter_new_rows(ma, data, self.conn)["work_orders"]["No"]

        # 只保留真正新的 WO3；修改過的 WO1 與既有的 WO2 都略過 → 不會重複。
        self.assertEqual(kept, ["WO3"])

    def test_old_hash_logic_would_have_duplicated(self):
        """鑑別力：修正前的 hash 法會保留修改過的 WO1（= 會重複），證明測試有效。"""
        _seed_work_orders(self.conn, ["WO1", "WO2"], [100.0, 200.0])
        data = _wo_data(["WO1", "WO2", "WO3"], [999.0, 200.0, 300.0])

        old_kept_nos = [data["work_orders"]["No"][i] for i in _old_hash_keep(ma, data, self.conn)]

        # 舊法把修改過的 WO1 當新資料保留（hash 變了）→ 重跑就重複。
        self.assertIn("WO1", old_kept_nos)
        # 新法則正確壓掉它。
        new_kept = aid.filter_new_rows(ma, data, self.conn)["work_orders"]["No"]
        self.assertNotIn("WO1", new_kept)

    def test_crash_rerun_uses_db_keys_not_state(self):
        """T3.2：state 完全沒寫回（中途中斷）時，DB 既有 legacy_no 仍是權威。"""
        # 第一次跑到一半崩潰：WO1 已寫進 DB，但 legacy_import_state 一行都沒存。
        _seed_work_orders(self.conn, ["WO1"], [100.0])
        # state 為空（模擬 save_state 沒機會執行）。
        self.assertEqual(
            self.conn.execute("SELECT COUNT(*) FROM legacy_import_state").fetchone()[0],
            0,
        )

        # 重跑：來源仍是 WO1(已存在) + WO2(新)。
        data = _wo_data(["WO1", "WO2"], [100.0, 300.0])
        kept = aid.filter_new_rows(ma, data, self.conn)["work_orders"]["No"]

        # 只保留 WO2；WO1 靠 DB 既有資料判別、不靠 state → 不重複。
        self.assertEqual(kept, ["WO2"])

    def test_empty_db_keeps_everything(self):
        """空庫（完整匯入後首跑）行為與舊版一致：全留。"""
        data = _wo_data(["WO1", "WO2"], [100.0, 200.0])
        kept = aid.filter_new_rows(ma, data, self.conn)["work_orders"]["No"]
        self.assertEqual(kept, ["WO1", "WO2"])

    def test_duplicate_keys_in_same_batch_are_kept_once(self):
        """同一批來源重複單號只應保留第一筆。"""
        _seed_work_orders(self.conn, ["WO1"], [100.0])
        data = _wo_data(["WO1", "WO2", "WO2", ""], [100.0, 200.0, 201.0, 300.0])
        kept = aid.filter_new_rows(ma, data, self.conn)["work_orders"]["No"]
        self.assertEqual(kept, ["WO2", ""])


class ResetClearsReceivablesTest(unittest.TestCase):
    """T3.3：reset() 必須清除 receivables 與 technician_worklogs。"""

    # reset() 會 DELETE 的所有表；測試要先把它們建出來。
    _SCHEMA = """
        CREATE TABLE work_order_items (id INTEGER PRIMARY KEY);
        CREATE TABLE work_orders (id INTEGER PRIMARY KEY);
        CREATE TABLE vehicles (id INTEGER PRIMARY KEY);
        CREATE TABLE customers (id INTEGER PRIMARY KEY);
        CREATE TABLE suppliers (id INTEGER PRIMARY KEY);
        CREATE TABLE inventory_items (id INTEGER PRIMARY KEY);
        CREATE TABLE inventory_movements (id INTEGER PRIMARY KEY);
        CREATE TABLE vehicle_insurances (id INTEGER PRIMARY KEY);
        CREATE TABLE vehicle_inspections (id INTEGER PRIMARY KEY);
        CREATE TABLE maintenance_template_items (id INTEGER PRIMARY KEY);
        CREATE TABLE maintenance_templates (id INTEGER PRIMARY KEY);
        CREATE TABLE expenses (id INTEGER PRIMARY KEY);
        CREATE TABLE users (id INTEGER PRIMARY KEY, password TEXT);
        CREATE TABLE insurance_companies (id INTEGER PRIMARY KEY, code TEXT);
        CREATE TABLE receivables (id INTEGER PRIMARY KEY, pay_total REAL);
        CREATE TABLE technician_worklogs (id INTEGER PRIMARY KEY);
        """

    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.db = os.path.join(self.tmp, "t.db")
        self.conn = sqlite3.connect(self.db)
        self.conn.executescript(self._SCHEMA)
        self.conn.executescript(
            """
            INSERT INTO receivables (pay_total) VALUES (100), (200);
            INSERT INTO technician_worklogs (id) VALUES (1), (2);
            INSERT INTO insurance_companies (code) VALUES ('ACME-001'), ('OLD-9');
            INSERT INTO users (password) VALUES ('legacy'), ('owner');
            """
        )
        self.conn.commit()

    def tearDown(self):
        self.conn.close()

    def test_reset_clears_receivables_and_worklogs(self):
        migrator = ma.Migrator(data={}, sqlite_path=self.db, dry_run=False)
        migrator.reset()
        self.conn.rollback()

        recv = self.conn.execute("SELECT COUNT(*) FROM receivables").fetchone()[0]
        logs = self.conn.execute("SELECT COUNT(*) FROM technician_worklogs").fetchone()[0]
        self.assertEqual(recv, 0, "reset 未清除 receivables（review T3.3）")
        self.assertEqual(logs, 0, "reset 未清除 technician_worklogs")

        # 既有的去重保護仍成立：seed 保險公司保留、舊的刪掉。
        ic = self.conn.execute(
            "SELECT code FROM insurance_companies"
        ).fetchall()
        self.assertEqual(ic, [("ACME-001",)])
        usr = self.conn.execute(
            "SELECT password FROM users"
        ).fetchall()
        self.assertEqual(usr, [("owner",)])  # password='legacy' 被清、owner 保留


class InsuranceCompanyResolveTest(unittest.TestCase):
    """T3.4：工單 Insurance Name（自由文字）→ insurance_companies.id。"""

    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.db = os.path.join(self.tmp, "t.db")
        self.conn = sqlite3.connect(self.db)
        self.conn.executescript(
            """
            CREATE TABLE insurance_companies (
                id INTEGER PRIMARY KEY, code TEXT, name TEXT
            );
            INSERT INTO insurance_companies (code, name) VALUES
                ('FUBON', '富邦產物保險股份有限公司'),
                ('TAIAN', '泰安產物保險'),
                ('07', '國產實業保險');
            """
        )
        self.conn.commit()
        self.mig = ma.Migrator(data={}, sqlite_path=self.db, dry_run=False)

    def tearDown(self):
        self.conn.close()

    def test_exact_name_and_code(self):
        self.assertEqual(
            self.mig.resolve_insurance_company("富邦產物保險股份有限公司"),
            self._name_of_id(1))
        # 簡稱 code 也解得到
        self.assertIsNotNone(self.mig.resolve_insurance_company("FUBON"))

    def test_whitespace_normalized(self):
        # 客戶手寫的空白不影響
        self.assertIsNotNone(
            self.mig.resolve_insurance_company("  富邦產物保險   股份有限公司 "))

    def test_short_name_containment(self):
        # 工單寫簡稱「富邦」→ 包含備援命中富邦
        cid = self.mig.resolve_insurance_company("富邦")
        self.assertEqual(cid, self._name_of_id(1))

    def test_unknown_and_empty_return_none(self):
        self.assertIsNone(self.mig.resolve_insurance_company("不知道哪間"))
        self.assertIsNone(self.mig.resolve_insurance_company(""))
        self.assertIsNone(self.mig.resolve_insurance_company(None))

    def _name_of_id(self, cid):
        return self.conn.execute(
            "SELECT id FROM insurance_companies WHERE name=?",
            ("富邦產物保險股份有限公司",),
        ).fetchone()[0]


class WorkOrderInsuranceInsertTest(unittest.TestCase):
    """T3.4 端到端：migrate_work_orders 把 ins_name 真正寫進 insurance_company_id。"""

    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.db = os.path.join(self.tmp, "t.db")
        self.conn = sqlite3.connect(self.db)
        self.conn.executescript(
            """
            CREATE TABLE customers (id INTEGER PRIMARY KEY);
            CREATE TABLE vehicles (id INTEGER PRIMARY KEY);
            CREATE TABLE insurance_companies (id INTEGER PRIMARY KEY, code TEXT, name TEXT);
            CREATE TABLE work_orders (
                id INTEGER PRIMARY KEY, vehicle_id INTEGER, status TEXT,
                current_mileage INTEGER, customer_complaint TEXT, approval_status TEXT,
                total_amount REAL, sop_completed INTEGER, customer_id INTEGER,
                technician_id INTEGER, notes TEXT, created_at TEXT, updated_at TEXT,
                insurance_company_id INTEGER, claim_no TEXT, is_insurance INTEGER,
                accident_date TEXT, deductible_amount REAL, legacy_no TEXT
            );
            INSERT INTO customers (id) VALUES (1);
            INSERT INTO vehicles (id) VALUES (1);
            INSERT INTO insurance_companies (code, name) VALUES
                ('FUBON', '富邦產物保險股份有限公司');
            """
        )
        self.conn.commit()
        self.data = {
            "Maintain Header": {
                "No": ["WO-INS", "WO-NORMAL"],
                "Car No": ["V1", "V1"],
                "Customer No": ["C1", "C1"],
                "Pay Flag": [0, 0],
                "Complete Date": ["115/10/14", ""],
                "Consign Date": ["", ""],
                "Date": ["", ""],
                "Insurance Flag": [1, 0],
                "Insurance Name": ["富邦產物保險股份有限公司", ""],
                "Mileage": [1000, 2000],
                "Real Total": [5000, 3000],
                "Staff No": ["S1", "S1"],
                "Remark": ["", ""],
                "Insurance Table No": ["CLM1", ""],
                "Insurance Remark": [1000, 0],
            }
        }

    def tearDown(self):
        self.conn.close()

    def _mig(self):
        mig = ma.Migrator(data=self.data, sqlite_path=self.db, dry_run=False)
        # 直接餵解析結果，繞過 _build_customer_resolver。
        mig.veh_id = {"V1": 1}
        mig.cust_by_notes = {"C1": 1}
        mig.no2name = {}
        mig.cust_by_name = {}
        mig.user_id = {"S1": 1}
        return mig

    def test_insurance_wo_gets_company_id(self):
        mig = self._mig()
        mig.migrate_work_orders()  # 內部會 commit
        ic_id = mig.conn.execute(
            "SELECT id FROM insurance_companies WHERE code='FUBON'"
        ).fetchone()[0]
        row = mig.conn.execute(
            "SELECT insurance_company_id, is_insurance FROM work_orders "
            "WHERE legacy_no='WO-INS'"
        ).fetchone()
        self.assertIsNotNone(row, "工單 WO-INS 沒寫進去（INSERT 掛了？）")
        self.assertEqual(row[0], ic_id, "保險公司關聯沒填對")
        self.assertEqual(row[1], 1)
        self.assertEqual(mig.ins_wo_hit, 1)

    def test_non_insurance_wo_keeps_null(self):
        mig = self._mig()
        mig.migrate_work_orders()
        row = mig.conn.execute(
            "SELECT insurance_company_id FROM work_orders WHERE legacy_no='WO-NORMAL'"
        ).fetchone()
        self.assertIsNone(row[0], "非保險工單不該有 insurance_company_id")
        self.assertEqual(mig.ins_wo_miss, 0)


if __name__ == "__main__":
    unittest.main(verbosity=2)
