#!/usr/bin/env python3
"""第一次完整匯入 CAR35 Data35.mdb 到 AutoRepairOS SQLite。"""

import argparse
import os
import sqlite3
import sys

from access_import_common import (
    backup_database,
    ensure_sync_schema_if_needed,
    load_migrator,
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

    # --dry-run 不得修改 schema（review T3.5）：ensure_sync_schema_if_needed 會
    # ALTER TABLE / CREATE TABLE，因此只在真正寫入時才呼叫。
    backup = None
    if not args.dry_run:
        backup = backup_database(args.sqlite_db)
        print(f"已備份 → {backup}")
        conn = sqlite3.connect(args.sqlite_db)
        ensure_sync_schema_if_needed(conn)
        conn.close()

    data = module.extract_tables(args.access_mdb, module.ALL_TABLES)
    migrator = module.Migrator(data, args.sqlite_db, dry_run=args.dry_run)
    try:
        if not args.dry_run:
            migrator.reset()
            migrator.cur.execute("DELETE FROM legacy_import_state")
            migrator.conn.commit()
        migrator.run()
    except Exception:
        if not args.dry_run:
            migrator.conn.rollback()
            migrator.conn.close()
            import shutil
            shutil.copy2(backup, args.sqlite_db)
            print(f"匯入失敗，已從備份復原 → {args.sqlite_db}", file=sys.stderr)
        raise

    if not args.dry_run:
        conn = sqlite3.connect(args.sqlite_db)
        save_state(conn, module, data)
        conn.close()
        print("已建立 legacy_import_state，之後可使用增量同步工具。")


if __name__ == "__main__":
    main()
