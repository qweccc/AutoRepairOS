#!/usr/bin/env python3
"""將新版 CAR35 Data35.mdb 的新增資料同步到既有 AutoRepairOS SQLite。"""

import argparse
import os
import sqlite3
import sys

from access_import_common import (
    backup_database,
    ensure_sync_schema_if_needed,
    filter_new_rows,
    load_migrator,
    preload_work_order_map,
    save_state,
)


def main():
    sys.stdout.reconfigure(encoding="utf-8")
    sys.stderr.reconfigure(encoding="utf-8")
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("access_mdb")
    parser.add_argument("sqlite_db")
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()

    for path in (args.access_mdb, args.sqlite_db):
        if not os.path.exists(path):
            parser.error(f"找不到檔案: {path}")

    module = load_migrator()
    conn = sqlite3.connect(args.sqlite_db)
    ensure_sync_schema_if_needed(conn)
    state_count = conn.execute("SELECT COUNT(*) FROM legacy_import_state").fetchone()[0]
    missing_legacy = conn.execute(
        "SELECT COUNT(*) FROM work_orders WHERE legacy_no = ''"
    ).fetchone()[0]
    if state_count == 0:
        parser.error("找不到完整匯入索引，請先執行 full_import_access.py")
    if missing_legacy:
        parser.error(
            "目前資料庫有工單沒有 legacy_no，請先用 full_import_access.py "
            "重新建立完整匯入資料庫，避免同步重複工單"
        )
    conn.close()

    data = module.extract_tables(args.access_mdb, module.ALL_TABLES)
    conn = sqlite3.connect(args.sqlite_db)
    new_data = filter_new_rows(module, data, conn)
    conn.close()

    new_counts = {
        name: len(next(iter(table.values()))) if table else 0
        for name, table in new_data.items()
    }
    total_new = sum(new_counts.values())
    print(f"來源新增資料列: {total_new:,}")
    for name, count in new_counts.items():
        if count:
            print(f"  {name}: {count:,}")
    if total_new == 0:
        print("沒有新資料，不需同步。")
        return
    if args.dry_run:
        print("DRY-RUN：未寫入資料庫。")
        return

    print(f"已備份 → {backup_database(args.sqlite_db)}")
    # 需要完整的工單／進貨單主檔來解析新增明細；工單方法會以 legacy_no 去重。
    new_data["Maintain Header"] = data["Maintain Header"]
    new_data["Material Header"] = data["Material Header"]

    migrator = module.Migrator(new_data, args.sqlite_db, dry_run=False)
    preload_work_order_map(migrator)
    # 這些表在增量模式已經先用 row_hash 去重，不能再用「整批已存在」保護。
    migrator._guard_append_only = lambda table, expected: False
    migrator.run()

    conn = sqlite3.connect(args.sqlite_db)
    save_state(conn, module, data)
    conn.close()
    print("增量同步完成，來源索引已更新。")


if __name__ == "__main__":
    main()
