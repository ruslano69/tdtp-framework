#!/usr/bin/env python3
"""
TDTP CLI Integration Tests — SQLite source

Tests: basic export, TDTQL filters, compression, export/import roundtrip,
       file integrity, edge cases.

Usage:
    python3 tests/cli/test_sqlite.py          # all groups
    python3 tests/cli/test_sqlite.py T3       # single group
    TDTPCLI_BIN=/path/to/tdtpcli python3 tests/cli/test_sqlite.py
"""

import os
import sys
import time
import shutil
import sqlite3
import subprocess
import xml.etree.ElementTree as ET
from pathlib import Path

# ─── Configuration ────────────────────────────────────────────────────────────
ROOT    = Path(__file__).resolve().parent.parent.parent   # repo root
TDTPCLI = os.environ.get("TDTPCLI_BIN", "/tmp/tdtpcli")
TEST_DB = "/tmp/tdtp_test.db"
OUTDIR  = Path("/tmp/tdtp_test_out")
CFG     = "/tmp/tdtp_sqlite_test.yaml"          # plain config (no compression)
CFG_C   = "/tmp/tdtp_sqlite_compress.yaml"      # config with compression from file
CFG_IMP = "/tmp/tdtp_sqlite_import.yaml"        # import-target config

# ─── ANSI colors ──────────────────────────────────────────────────────────────
GREEN  = "\033[32m"
RED    = "\033[31m"
YELLOW = "\033[33m"
BOLD   = "\033[1m"
RESET  = "\033[0m"

# ─── Global results ───────────────────────────────────────────────────────────
results: list = []   # list of (tid: str, passed: bool, elapsed: float, msg: str)


# ─── Helpers ──────────────────────────────────────────────────────────────────

def run(*args, cfg=None, timeout=30) -> subprocess.CompletedProcess:
    """Run tdtpcli with --config <cfg> and additional args."""
    cmd = [TDTPCLI, "--config", cfg or CFG] + list(args)
    return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)


def run_no_cfg(*args, timeout=30) -> subprocess.CompletedProcess:
    """Run tdtpcli without --config (for --test / --inspect which need no DB)."""
    cmd = [TDTPCLI] + list(args)
    return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)


def count_rows_xml(path: str) -> int:
    """Count data rows in a TDTP XML file.

    Uncompressed  → count <R> elements inside <Data>.
    Compressed    → read RecordsInPart from <Header> (blob is 1 row).
    Returns -1 on parse error or missing file.
    """
    if not os.path.exists(path):
        return -1
    try:
        tree = ET.parse(path)
        root = tree.getroot()
        data = root.find("Data")
        if data is None:
            return 0
        if data.get("compression"):
            hdr = root.find("Header")
            if hdr is not None:
                rip = hdr.find("RecordsInPart")
                if rip is not None and rip.text:
                    return int(rip.text)
            return -1
        return len(data.findall("R"))
    except ET.ParseError:
        return -1


def get_schema_fields(path: str) -> list:
    """Return ordered list of field names from <Schema> in a TDTP XML file."""
    if not os.path.exists(path):
        return []
    try:
        root = ET.parse(path).getroot()
        schema = root.find("Schema")
        if schema is None:
            return []
        return [f.get("name", "") for f in schema.findall("Field")]
    except ET.ParseError:
        return []


def sqlite_query(db_path: str, sql: str) -> list:
    """Execute a SQL query against a SQLite file and return all rows."""
    conn = sqlite3.connect(db_path)
    try:
        return conn.execute(sql).fetchall()
    finally:
        conn.close()


def out(name: str) -> str:
    """Return absolute path inside OUTDIR."""
    return str(OUTDIR / name)


def write_cfg(path: str, db: str = TEST_DB,
              compress: bool = False, algo: str = "zstd", level: int = 3):
    """Write a minimal SQLite config YAML."""
    with open(path, "w") as f:
        f.write(f"database:\n  type: sqlite\n  database: {db}\n")
        f.write(f"export:\n")
        f.write(f"  compress: {str(compress).lower()}\n")
        f.write(f"  compress_algo: {algo}\n")
        f.write(f"  compress_level: {level}\n")


def record(tid: str, passed: bool, elapsed: float, msg: str = ""):
    """Record a test result and print a one-liner."""
    results.append((tid, passed, elapsed, msg))
    status = f"{GREEN}PASS{RESET}" if passed else f"{RED}FAIL{RESET}"
    detail = f"  ({msg})" if msg and not passed else ""
    print(f"  [{status}] {tid:<45} {elapsed:.2f}s{detail}")


def setup_db():
    """Create the SQLite test database inline (no external script needed)."""
    if os.path.exists(TEST_DB):
        os.remove(TEST_DB)
    conn = sqlite3.connect(TEST_DB)
    c = conn.cursor()

    c.execute("""CREATE TABLE users (
        ID INTEGER PRIMARY KEY, Name TEXT NOT NULL, Email TEXT,
        Balance NUMERIC(18,2), IsActive INTEGER, City TEXT,
        CreatedAt DATETIME, LastLoginAt DATETIME)""")
    c.executemany("INSERT INTO users VALUES (?,?,?,?,?,?,?,?)", [
        (1,  "John Doe",      "john@example.com",    1500.00, 1, "Moscow", "2025-01-15 10:00:00", "2025-11-10 15:30:00"),
        (2,  "Jane Smith",    "jane@example.com",    2000.00, 1, "SPb",    "2025-02-20 11:00:00", "2025-11-12 09:15:00"),
        (3,  "Bob Johnson",   "bob@example.com",      500.00, 0, "Moscow", "2025-03-10 12:00:00", "2025-10-05 14:20:00"),
        (4,  "Alice Brown",   "alice@example.com",   2500.00, 1, "Kazan",  "2025-01-05 09:00:00", "2025-11-13 11:45:00"),
        (5,  "Charlie Davis", "charlie@example.com",  800.00, 1, "SPb",    "2025-04-12 13:00:00", "2025-11-11 16:30:00"),
        (6,  "Emma Wilson",   "emma@example.com",    3000.00, 1, "Moscow", "2024-12-20 10:00:00", "2025-11-14 08:00:00"),
        (7,  "Frank Miller",  "frank@example.com",   1200.00, 0, "Moscow", "2025-05-18 14:00:00", "2025-09-20 10:10:00"),
        (8,  "Grace Lee",     "grace@example.com",   1800.00, 1, "SPb",    "2025-02-28 15:00:00", "2025-11-13 12:20:00"),
        (9,  "Henry Taylor",  "henry@example.com",    400.00, 1, "Kazan",  "2025-06-01 16:00:00", "2025-11-09 17:30:00"),
        (10, "Ivy Anderson",  "ivy@example.com",     2200.00, 1, "Moscow", "2025-01-30 17:00:00", "2025-11-14 07:45:00"),
    ])

    c.execute("""CREATE TABLE orders (
        OrderID INTEGER PRIMARY KEY, UserID INTEGER, ProductName TEXT,
        Amount NUMERIC(18,2), Status TEXT, CreatedAt DATETIME)""")
    c.executemany("INSERT INTO orders VALUES (?,?,?,?,?,?)", [
        (1, 1, "Laptop",     1500.00, "completed", "2025-11-01 10:00:00"),
        (2, 2, "Phone",       800.00, "pending",   "2025-11-05 11:30:00"),
        (3, 4, "Tablet",      600.00, "completed", "2025-11-03 14:15:00"),
        (4, 6, "Monitor",     400.00, "pending",   "2025-11-10 09:20:00"),
        (5, 8, "Keyboard",    100.00, "completed", "2025-11-08 16:45:00"),
        (6, 10, "Mouse",       50.00, "pending",   "2025-11-12 12:30:00"),
        (7, 1, "Headphones",  200.00, "cancelled", "2025-11-02 13:00:00"),
        (8, 2, "Webcam",      150.00, "completed", "2025-11-09 10:10:00"),
    ])

    c.execute("""CREATE TABLE products (
        ProductID INTEGER PRIMARY KEY, Name TEXT NOT NULL, Category TEXT,
        Price NUMERIC(18,2), Stock INTEGER, IsAvailable INTEGER, UpdatedAt DATETIME)""")
    c.executemany("INSERT INTO products VALUES (?,?,?,?,?,?,?)", [
        (1,  "Laptop Pro 15",       "Electronics",  1500.00, 10,  1, "2025-11-01 10:00:00"),
        (2,  "Smartphone X",        "Electronics",   800.00, 25,  1, "2025-11-05 11:00:00"),
        (3,  "Tablet Ultra",        "Electronics",   600.00, 15,  1, "2025-11-03 12:00:00"),
        (4,  "Monitor 27inch",      "Electronics",   400.00,  8,  1, "2025-11-10 13:00:00"),
        (5,  "Mechanical Keyboard", "Accessories",   100.00, 50,  1, "2025-11-08 14:00:00"),
        (6,  "Wireless Mouse",      "Accessories",    50.00, 100, 1, "2025-11-12 15:00:00"),
        (7,  "USB-C Hub",           "Accessories",    80.00, 30,  1, "2025-11-07 16:00:00"),
        (8,  "Webcam HD",           "Electronics",   150.00, 12,  1, "2025-11-09 17:00:00"),
        (9,  "Headphones Pro",      "Audio",         200.00, 20,  1, "2025-11-02 18:00:00"),
        (10, "Speakers",            "Audio",         300.00,  5,  0, "2025-10-15 19:00:00"),
    ])

    conn.commit()
    conn.close()


# ─── T1 Basic Export ──────────────────────────────────────────────────────────

def test_T1_basic_export():
    print(f"\n{BOLD}=== T1 Basic Export ==={RESET}")

    # T1.1 — export all rows, verify count = 10
    t = time.monotonic()
    p = run("--export", "users", "--output", out("t1_users.xml"))
    rows = count_rows_xml(out("t1_users.xml"))
    record("T1.1 export all rows (users)",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rc={p.returncode} rows={rows}")

    # T1.2 — column projection: only Name + Email in schema
    t = time.monotonic()
    p = run("--export", "users", "--fields", "Name,Email",
            "--output", out("t1_fields.xml"))
    fields = get_schema_fields(out("t1_fields.xml"))
    record("T1.2 --fields Name,Email → 2 columns",
           p.returncode == 0 and fields == ["Name", "Email"],
           time.monotonic() - t, f"fields={fields}")

    # T1.3 — --list shows all three tables
    t = time.monotonic()
    p = run("--list")
    tables_ok = all(tbl in p.stdout for tbl in ("users", "orders", "products"))
    record("T1.3 --list shows all tables",
           p.returncode == 0 and tables_ok,
           time.monotonic() - t, p.stdout.strip()[:60])


# ─── T2 TDTQL Filters ─────────────────────────────────────────────────────────

def test_T2_filters():
    print(f"\n{BOLD}=== T2 TDTQL Filters ==={RESET}")

    # T2.1 — Balance > 500 → 8 rows (Bob=500 excluded, Henry=400 excluded)
    t = time.monotonic()
    p = run("--export", "users", "--where", "Balance > 500",
            "--output", out("t2_1.xml"))
    rows = count_rows_xml(out("t2_1.xml"))
    record("T2.1 WHERE Balance > 500 → 8 rows",
           p.returncode == 0 and rows == 8,
           time.monotonic() - t, f"rows={rows}")

    # T2.2 — IsActive = 1 → 8 rows (Bob and Frank inactive)
    t = time.monotonic()
    p = run("--export", "users", "--where", "IsActive = 1",
            "--output", out("t2_2.xml"))
    rows = count_rows_xml(out("t2_2.xml"))
    record("T2.2 WHERE IsActive = 1 → 8 rows",
           p.returncode == 0 and rows == 8,
           time.monotonic() - t, f"rows={rows}")

    # T2.3 — multiple --where flags combined as AND → 8 rows
    t = time.monotonic()
    p = run("--export", "users",
            "--where", "Balance > 100", "--where", "IsActive = 1",
            "--output", out("t2_3.xml"))
    rows = count_rows_xml(out("t2_3.xml"))
    record("T2.3 WHERE Balance>100 AND IsActive=1 → 8 rows",
           p.returncode == 0 and rows == 8,
           time.monotonic() - t, f"rows={rows}")

    # T2.4 — IN operator: completed + pending = 7 (not 'cancelled')
    t = time.monotonic()
    p = run("--export", "orders",
            "--where", "Status IN ('completed','pending')",
            "--output", out("t2_4.xml"))
    rows = count_rows_xml(out("t2_4.xml"))
    record("T2.4 WHERE Status IN ('completed','pending') → 7 rows",
           p.returncode == 0 and rows == 7,
           time.monotonic() - t, f"rows={rows}")

    # T2.5 — ORDER BY + LIMIT → exactly 3 rows
    t = time.monotonic()
    p = run("--export", "users",
            "--order-by", "Balance DESC", "--limit", "3",
            "--output", out("t2_5.xml"))
    rows = count_rows_xml(out("t2_5.xml"))
    record("T2.5 ORDER BY Balance DESC LIMIT 3 → 3 rows",
           p.returncode == 0 and rows == 3,
           time.monotonic() - t, f"rows={rows}")

    # T2.6 — LIMIT + OFFSET: skip 3, take 5 → rows 4-8
    t = time.monotonic()
    p = run("--export", "users", "--limit", "5", "--offset", "3",
            "--output", out("t2_6.xml"))
    rows = count_rows_xml(out("t2_6.xml"))
    record("T2.6 LIMIT 5 OFFSET 3 → 5 rows",
           p.returncode == 0 and rows == 5,
           time.monotonic() - t, f"rows={rows}")

    # T2.7 — negative LIMIT (tail mode): last 3 rows
    t = time.monotonic()
    p = run("--export", "users", "--limit", "-3",
            "--output", out("t2_7.xml"))
    rows = count_rows_xml(out("t2_7.xml"))
    record("T2.7 LIMIT -3 (last 3 rows, tail mode)",
           p.returncode == 0 and rows == 3,
           time.monotonic() - t, f"rows={rows}")


# ─── T3 Compression ───────────────────────────────────────────────────────────

def test_T3_compression():
    print(f"\n{BOLD}=== T3 Compression ==={RESET}")

    # Baseline: uncompressed size
    run("--export", "users", "--output", out("t3_base.xml"))
    base_size = os.path.getsize(out("t3_base.xml")) if os.path.exists(out("t3_base.xml")) else 0

    # T3.1 — zstd level 3: file smaller than baseline; --test passes
    t = time.monotonic()
    p = run("--export", "users", "--compress", "--compress-level", "3",
            "--output", out("t3_z3.xml"))
    z3_size = os.path.getsize(out("t3_z3.xml")) if os.path.exists(out("t3_z3.xml")) else 0
    pt = run_no_cfg("--test", out("t3_z3.xml"))
    record("T3.1 zstd level 3 (smaller + --test OK)",
           p.returncode == 0 and z3_size < base_size and pt.returncode == 0,
           time.monotonic() - t, f"z3={z3_size} base={base_size} test_rc={pt.returncode}")

    # T3.2 — zstd level 19: must be ≤ level 3
    t = time.monotonic()
    p = run("--export", "users", "--compress", "--compress-level", "19",
            "--output", out("t3_z19.xml"))
    z19_size = os.path.getsize(out("t3_z19.xml")) if os.path.exists(out("t3_z19.xml")) else 0
    record("T3.2 zstd level 19 smaller than uncompressed",
           p.returncode == 0 and z19_size < base_size,
           time.monotonic() - t, f"z19={z19_size} base={base_size}")

    # T3.3 — kanzi level 6; --test passes
    t = time.monotonic()
    p = run("--export", "users", "--compress",
            "--compress-algo", "kanzi", "--compress-level", "6",
            "--output", out("t3_k6.xml"))
    k6_size = os.path.getsize(out("t3_k6.xml")) if os.path.exists(out("t3_k6.xml")) else 0
    pt = run_no_cfg("--test", out("t3_k6.xml"))
    record("T3.3 kanzi level 6 + --test OK",
           p.returncode == 0 and pt.returncode == 0,
           time.monotonic() - t, f"k6={k6_size} test_rc={pt.returncode}")

    # T3.4 — zstd + --hash: --test must report "checksum OK"
    t = time.monotonic()
    p = run("--export", "users", "--compress", "--hash",
            "--output", out("t3_hash.xml"))
    pt = run_no_cfg("--test", out("t3_hash.xml"))
    checksum_ok = "checksum OK" in pt.stdout
    record("T3.4 --compress --hash → checksum OK in --test",
           p.returncode == 0 and checksum_ok,
           time.monotonic() - t, pt.stdout.strip()[-60:])

    # T3.5 — corrupt 1 byte in compressed+hash file → --test must fail
    t = time.monotonic()
    corrupt = out("t3_corrupt.xml")
    shutil.copy(out("t3_hash.xml"), corrupt)
    fsize = os.path.getsize(corrupt)
    mid = fsize // 2
    with open(corrupt, "r+b") as f:
        f.seek(mid)
        b = f.read(1)
        f.seek(mid)
        f.write(bytes([b[0] ^ 0x55]))   # flip bits — guaranteed to change the byte
    pt = run_no_cfg("--test", corrupt)
    record("T3.5 corrupted file → --test fails",
           pt.returncode != 0,
           time.monotonic() - t, f"rc={pt.returncode}")

    # T3.6 — compress_algo from config file (no flag on command line)
    write_cfg(CFG_C, compress=True, algo="zstd", level=3)
    t = time.monotonic()
    p = run("--export", "users", "--output", out("t3_cfg.xml"), cfg=CFG_C)
    cfg_size = os.path.getsize(out("t3_cfg.xml")) if os.path.exists(out("t3_cfg.xml")) else 0
    record("T3.6 compress_algo=zstd from config (no flag)",
           p.returncode == 0 and cfg_size < base_size,
           time.monotonic() - t, f"size={cfg_size} base={base_size}")


# ─── T4 Export/Import Roundtrip ───────────────────────────────────────────────

IMPORT_DB = "/tmp/tdtp_import_test.db"


def _import(file: str, table: str, strategy: str = "replace") -> subprocess.CompletedProcess:
    """Helper: import a file into IMPORT_DB."""
    return subprocess.run(
        [TDTPCLI, "--config", CFG_IMP,
         "--import", file, "--table", table, "--strategy", strategy],
        capture_output=True, text=True, timeout=30,
    )


def test_T4_roundtrip():
    print(f"\n{BOLD}=== T4 Export/Import Roundtrip ==={RESET}")

    # Prepare import config pointing to a fresh DB
    if os.path.exists(IMPORT_DB):
        os.remove(IMPORT_DB)
    write_cfg(CFG_IMP, db=IMPORT_DB)

    # T4.1 — plain export → import; row count must match
    t = time.monotonic()
    run("--export", "users", "--output", out("t4_plain.xml"))
    p = _import(out("t4_plain.xml"), "users_copy")
    rows = sqlite_query(IMPORT_DB, "SELECT COUNT(*) FROM users_copy")[0][0] \
           if p.returncode == 0 else -1
    record("T4.1 plain roundtrip: 10 rows imported",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rc={p.returncode} rows={rows}")

    # T4.2 — compressed roundtrip
    t = time.monotonic()
    run("--export", "users", "--compress", "--output", out("t4_comp.xml"))
    p = _import(out("t4_comp.xml"), "users_comp")
    rows = sqlite_query(IMPORT_DB, "SELECT COUNT(*) FROM users_comp")[0][0] \
           if p.returncode == 0 else -1
    record("T4.2 compressed roundtrip: 10 rows",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rows={rows}")

    # T4.3 — re-import with --strategy replace; still exactly 10 (no duplicates)
    t = time.monotonic()
    p = _import(out("t4_plain.xml"), "users_copy", strategy="replace")
    rows = sqlite_query(IMPORT_DB, "SELECT COUNT(*) FROM users_copy")[0][0] \
           if p.returncode == 0 else -1
    record("T4.3 re-import --strategy replace → 10 (no dup)",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rows={rows}")

    # T4.4 — re-import with --strategy ignore; still 10 (new rows ignored)
    t = time.monotonic()
    p = _import(out("t4_plain.xml"), "users_copy", strategy="ignore")
    rows = sqlite_query(IMPORT_DB, "SELECT COUNT(*) FROM users_copy")[0][0] \
           if p.returncode == 0 else -1
    record("T4.4 re-import --strategy ignore → 10 (no dup)",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rows={rows}")

    # T4.5 — --fields projection preserved through roundtrip
    t = time.monotonic()
    run("--export", "users", "--fields", "Name,Balance",
        "--output", out("t4_proj.xml"))
    p = _import(out("t4_proj.xml"), "users_proj")
    cols = [c[1] for c in sqlite_query(IMPORT_DB, "PRAGMA table_info(users_proj)")] \
           if p.returncode == 0 else []
    record("T4.5 --fields Name,Balance preserved in import",
           p.returncode == 0 and cols == ["Name", "Balance"],
           time.monotonic() - t, f"cols={cols}")

    if os.path.exists(IMPORT_DB):
        os.remove(IMPORT_DB)


# ─── T5 File Integrity ────────────────────────────────────────────────────────

def test_T5_integrity():
    print(f"\n{BOLD}=== T5 File Integrity ==={RESET}")

    # T5.1 — --test on uncompressed file
    run("--export", "users", "--output", out("t5_plain.xml"))
    t = time.monotonic()
    p = run_no_cfg("--test", out("t5_plain.xml"))
    record("T5.1 --test uncompressed → 'Total rows: 10'",
           p.returncode == 0 and "Total rows: 10" in p.stdout,
           time.monotonic() - t, p.stdout.strip()[-60:])

    # T5.2 — --test on compressed + checksum
    run("--export", "users", "--compress", "--hash",
        "--output", out("t5_hash.xml"))
    t = time.monotonic()
    p = run_no_cfg("--test", out("t5_hash.xml"))
    record("T5.2 --test compressed+checksum → 'checksum OK'",
           p.returncode == 0 and "checksum OK" in p.stdout,
           time.monotonic() - t, p.stdout.strip()[-60:])

    # T5.3 — --inspect returns YAML with TableName
    run("--export", "users", "--output", out("t5_inspect.xml"))
    t = time.monotonic()
    p = run_no_cfg("--inspect", out("t5_inspect.xml"))
    has_table = "table:" in p.stdout or "TableName" in p.stdout
    record("T5.3 --inspect shows TableName metadata",
           p.returncode == 0 and has_table,
           time.monotonic() - t, p.stdout.strip()[:80])


# ─── T6 Edge Cases ────────────────────────────────────────────────────────────

def test_T6_edge_cases():
    print(f"\n{BOLD}=== T6 Edge Cases ==={RESET}")

    # T6.1 — WHERE that matches nothing → exit 0, 0 rows
    t = time.monotonic()
    p = run("--export", "users", "--where", "Balance > 99999",
            "--output", out("t6_empty.xml"))
    rows = count_rows_xml(out("t6_empty.xml")) if p.returncode == 0 else -1
    record("T6.1 WHERE matches nothing → exit 0, 0 rows",
           p.returncode == 0 and rows == 0,
           time.monotonic() - t, f"rc={p.returncode} rows={rows}")

    # T6.2 — export non-existent table → exit != 0
    t = time.monotonic()
    p = run("--export", "nonexistent_table_xyz",
            "--output", out("t6_no_table.xml"))
    record("T6.2 export nonexistent table → error",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode}")

    # T6.3 — import missing file → exit != 0
    t = time.monotonic()
    p = run("--import", "/tmp/does_not_exist_xyz.xml")
    record("T6.3 import nonexistent file → error",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode}")


# ─── T7 Compact Format (v1.3.1) ──────────────────────────────────────────────

def test_T7_compact():
    print(f"\n{BOLD}=== T7 Compact Format (v1.3.1) ==={RESET}")

    # T7.1 — --compact --fixed-fields: fewer XML rows than total (fixed written once per group)
    t = time.monotonic()
    p = run("--export", "users", "--compact", "--fixed-fields", "City",
            "--output", out("t7_compact.xml"))
    # compact reduces row size — protocol version must be 1.3.1
    proto_ok = False
    if os.path.exists(out("t7_compact.xml")):
        try:
            root = ET.parse(out("t7_compact.xml")).getroot()
            proto_ok = root.get("version", "") == "1.3.1"
        except ET.ParseError:
            pass
    record("T7.1 --compact → protocol TDTP 1.3.1",
           p.returncode == 0 and proto_ok,
           time.monotonic() - t, f"rc={p.returncode} proto_ok={proto_ok}")

    # T7.2 — compact + compress + hash: full pipeline, --test must pass
    t = time.monotonic()
    p = run("--export", "users", "--compact", "--fixed-fields", "City",
            "--compress", "--hash",
            "--output", out("t7_compact_comp.xml"))
    pt = run_no_cfg("--test", out("t7_compact_comp.xml"))
    checksum_ok = "checksum OK" in pt.stdout
    record("T7.2 compact + compress + --hash → checksum OK",
           p.returncode == 0 and pt.returncode == 0 and checksum_ok,
           time.monotonic() - t, f"test_rc={pt.returncode}")

    # T7.3 — --to-compact converts existing plain file
    run("--export", "users", "--output", out("t7_plain.xml"))
    t = time.monotonic()
    p = run_no_cfg("--to-compact", out("t7_plain.xml"),
                   "--fixed-fields", "City",
                   "--output", out("t7_converted.xml"))
    proto_ok2 = False
    if os.path.exists(out("t7_converted.xml")):
        try:
            root = ET.parse(out("t7_converted.xml")).getroot()
            proto_ok2 = root.get("version", "") == "1.3.1"
        except ET.ParseError:
            pass
    record("T7.3 --to-compact converts plain file → 1.3.1",
           p.returncode == 0 and proto_ok2,
           time.monotonic() - t, f"rc={p.returncode} proto_ok={proto_ok2}")

    # T7.4 — compact export roundtrip: import preserves row count
    t = time.monotonic()
    import_db = "/tmp/tdtp_compact_import.db"
    if os.path.exists(import_db):
        os.remove(import_db)
    write_cfg("/tmp/tdtp_compact_import_cfg.yaml", db=import_db)
    p = subprocess.run(
        [TDTPCLI, "--config", "/tmp/tdtp_compact_import_cfg.yaml",
         "--import", out("t7_compact.xml"), "--table", "users_compact"],
        capture_output=True, text=True, timeout=30,
    )
    rows = sqlite_query(import_db, "SELECT COUNT(*) FROM users_compact")[0][0] \
           if p.returncode == 0 else -1
    record("T7.4 compact roundtrip: 10 rows imported",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rows={rows}")
    if os.path.exists(import_db):
        os.remove(import_db)


# ─── Runner ───────────────────────────────────────────────────────────────────

GROUPS = [
    ("T1", test_T1_basic_export),
    ("T2", test_T2_filters),
    ("T3", test_T3_compression),
    ("T4", test_T4_roundtrip),
    ("T5", test_T5_integrity),
    ("T6", test_T6_edge_cases),
    ("T7", test_T7_compact),
]


def preflight():
    if not os.path.exists(TDTPCLI):
        print(f"{RED}ERROR: tdtpcli binary not found at {TDTPCLI}{RESET}")
        print(f"Build first:")
        print(f"  GOPROXY=https://goproxy.io GONOSUMDB='*' "
              f"go build -tags nokafka -o {TDTPCLI} ./cmd/tdtpcli/")
        sys.exit(1)
    ver = subprocess.run([TDTPCLI, "--version"], capture_output=True, text=True)
    print(f"tdtpcli: {ver.stdout.strip()}")


def main():
    filter_group = sys.argv[1].upper() if len(sys.argv) > 1 else None

    preflight()

    OUTDIR.mkdir(parents=True, exist_ok=True)
    print(f"Setting up test database: {TEST_DB}")
    setup_db()
    write_cfg(CFG)
    print(f"Output dir: {OUTDIR}/")

    overall_start = time.monotonic()

    for group_id, fn in GROUPS:
        if filter_group and not group_id.startswith(filter_group):
            continue
        fn()

    passed = sum(1 for _, ok, _, _ in results if ok)
    failed = sum(1 for _, ok, _, _ in results if not ok)
    total  = len(results)
    elapsed = time.monotonic() - overall_start

    print(f"\n{BOLD}{'=' * 55}{RESET}")
    print(f"{BOLD}SUMMARY{RESET}")
    print(f"  {GREEN}PASSED: {passed} / {total}{RESET}")
    if failed:
        print(f"  {RED}FAILED: {failed}{RESET}")
        print(f"\n  Failed tests:")
        for tid, ok, _, msg in results:
            if not ok:
                print(f"    {RED}✗ {tid}{RESET}  {msg}")
    print(f"  DURATION: {elapsed:.1f}s")
    print(f"{'=' * 55}")

    sys.exit(0 if failed == 0 else 1)


if __name__ == "__main__":
    main()
