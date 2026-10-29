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


def filter_new_rows(module, data, conn):
    seen = {
        (row[0], row[1])
        for row in conn.execute("SELECT table_name, row_hash FROM legacy_import_state")
    }
    filtered = {}
    for table_name, table in data.items():
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
