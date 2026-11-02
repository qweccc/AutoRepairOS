#!/usr/bin/env python3
"""Shared helpers for the CAR35 Access import tools."""

import hashlib
import importlib.util
import json
import os
import shutil
import sqlite3
from datetime import datetime


SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
MIGRATOR_PATH = os.path.join(SCRIPT_DIR, "migrate_access.py")


def load_migrator():
    spec = importlib.util.spec_from_file_location("migrate_access", MIGRATOR_PATH)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def ensure_sync_schema(conn):
    conn.execute(
        "ALTER TABLE work_orders ADD COLUMN legacy_no TEXT NOT NULL DEFAULT ''"
    )
    conn.commit()


def ensure_sync_schema_if_needed(conn):
    columns = {row[1] for row in conn.execute("PRAGMA table_info(work_orders)")}
    if "legacy_no" not in columns:
        ensure_sync_schema(conn)
    worklog_columns = {
        row[1] for row in conn.execute("PRAGMA table_info(technician_worklogs)")
    }
    if "legacy_key" not in worklog_columns:
        conn.execute(
            "ALTER TABLE technician_worklogs ADD COLUMN legacy_key TEXT NOT NULL DEFAULT ''"
        )
    conn.execute(
        """
        CREATE TABLE IF NOT EXISTS legacy_import_state (
            table_name TEXT NOT NULL,
            row_hash TEXT NOT NULL,
            source_key TEXT NOT NULL DEFAULT '',
            imported_at TEXT NOT NULL,
            PRIMARY KEY (table_name, row_hash)
        )
        """
    )
    conn.commit()


def backup_database(path):
    stamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    backup = f"{path}.backup_{stamp}"
    shutil.copy2(path, backup)
    return backup


def row_count(table):
    if not table:
        return 0
    return len(next(iter(table.values())))


def row_hash(table, index):
    row = {key: table[key][index] for key in sorted(table)}
    payload = json.dumps(row, ensure_ascii=False, sort_keys=True, default=str)
    return hashlib.sha256(payload.encode("utf-8")).hexdigest()


def source_key(module, table, index):
    for key in ("No", "Sequence No", "Name"):
        if key in table:
            value = module.clean_str(module.col(table, key, index))
            if value:
                return value
    return ""


def all_state_rows(module, data):
    for table_name, table in data.items():
        for index in range(row_count(table)):
            yield (
                table_name,
                row_hash(table, index),
                source_key(module, table, index),
            )


def save_state(conn, module, data):
    now = datetime.now().isoformat(timespec="seconds")
    conn.executemany(
        """
        INSERT OR IGNORE INTO legacy_import_state
            (table_name, row_hash, source_key, imported_at)
        VALUES (?, ?, ?, ?)
        """,
        ((table, digest, key, now) for table, digest, key in all_state_rows(module, data)),
    )
    conn.commit()


def table_key_col(table):
    """來源主鍵欄位：多數表用 'No'，支出用 'Sequence No'，主檔用 'Name'。"""
    for key in ("No", "Sequence No", "Name"):
        if key in table:
            return key
    return None


def existing_db_keys(conn, table_name, db_col="legacy_no"):
    """從 DB 現有資料讀取已匯入的來源主鍵集合。

    DB 本身才是「已匯入過什麼」的權威：即使 legacy_import_state 因同步中斷
    沒寫回（save_state 在 run() 之後才執行），重跑時照樣能從 DB 已有的
    legacy_no 判別哪些列已匯入，因此不會重複（review T3.2）。
    DB 無此欄位（尚未 ensure_sync_schema 的表）或表不存在時回傳空集，
    由呼叫端退回 row_hash 法。
    """
    try:
        return {
            r[0] for r in conn.execute(
                f"SELECT {db_col} FROM {table_name} WHERE {db_col} <> ''"
            )
            if r[0]
        }
    except sqlite3.Error:
        return set()


def filter_new_rows(module, data, conn):
    """挑出來源中「DB 裡還沒有」的列。

    去重鍵改為來源主鍵（legacy_no），不再用整列 hash：
      * 整列 hash 的缺點是來源任一欄位修改後 hash 就變，舊列會被當新資料
        重複插入（review T3.1）。改用主鍵後，已存在的列一律略過，從根上
        消除重複。
      * 主鍵取自 DB 現有資料（existing_db_keys），不依賴 legacy_import_state，
        因此同步中途中斷、state 沒寫回時重跑也不會重複（review T3.2）。

    沒有可用主鍵、或 DB 該表無 legacy_no 欄位時，退回既有的 row_hash 法，
    與舊行為一致。
    """
    filtered = {}
    for table_name, table in data.items():
        key_col = table_key_col(table)
        keep = None
        if key_col:
            seen = existing_db_keys(conn, table_name)
            if seen:  # DB 有既有資料才用主鍵法；空庫等同全留，與舊行為一致。
                batch_seen = set()
                state_hashes = {
                    (row[0], row[1])
                    for row in conn.execute(
                        "SELECT table_name, row_hash FROM legacy_import_state"
                    )
                }
                keep = []
                for i in range(row_count(table)):
                    key = module.clean_str(module.col(table, key_col, i))
                    if not key:
                        if (table_name, row_hash(table, i)) not in state_hashes:
                            keep.append(i)
                        continue
                    if key not in seen and key not in batch_seen:
                        keep.append(i)
                        batch_seen.add(key)
                seen.update(
                    module.clean_str(module.col(table, key_col, i))
                    for i in keep
                    if module.clean_str(module.col(table, key_col, i))
                )
        if keep is None:  # 退回 hash 法
            seen = {
                (row[0], row[1])
                for row in conn.execute(
                    "SELECT table_name, row_hash FROM legacy_import_state"
                )
            }
            keep = [
                i for i in range(row_count(table))
                if (table_name, row_hash(table, i)) not in seen
            ]
        filtered[table_name] = {
            column: [values[i] for i in keep]
            for column, values in table.items()
        }
    return filtered


def preload_work_order_map(migrator):
    migrator.cur.execute(
        "SELECT legacy_no, id FROM work_orders WHERE legacy_no <> ''"
    )
    migrator.wo_by_legacy_no.update(
        {legacy_no: work_order_id for legacy_no, work_order_id in migrator.cur.fetchall()}
    )
