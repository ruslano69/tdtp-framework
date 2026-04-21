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
import re
import sys
import time
import shutil
import sqlite3
import subprocess
import urllib.request
import xml.etree.ElementTree as ET
from pathlib import Path

# Force UTF-8 output so → and other Unicode chars work on Windows cp1251 terminals
if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")

# ─── Configuration ────────────────────────────────────────────────────────────
ROOT    = Path(__file__).resolve().parent.parent.parent   # repo root
TDTPCLI = os.environ.get("TDTPCLI_BIN", "/tmp/tdtpcli")
TEST_DB = "/tmp/tdtp_test.db"
OUTDIR  = Path("/tmp/tdtp_test_out")
CFG     = "/tmp/tdtp_sqlite_test.yaml"          # plain config (no compression)
CFG_C   = "/tmp/tdtp_sqlite_compress.yaml"      # config with compression from file
CFG_IMP = "/tmp/tdtp_sqlite_import.yaml"        # import-target config

# S3 (SeaweedFS) — optional, tests skip when weed is not running
S3_ENDPOINT   = "http://127.0.0.1:8333"
S3_BUCKET     = "tdtp-test"
S3_PREFIX     = "ci/t8"
# Credentials from weed/s3.json (IAM mode — "any"/"any" not accepted)
S3_ACCESS_KEY = os.environ.get("TDTP_S3_ACCESS_KEY", "tdtp_access")
S3_SECRET_KEY = os.environ.get("TDTP_S3_SECRET_KEY", "tdtp_secret")
CFG_S3        = "/tmp/tdtp_sqlite_s3.yaml"
CFG_ETL_S3    = "/tmp/tdtp_etl_s3.yaml"

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


def modify_xml_field(src: str, dst: str, id_val: str, field_idx: int, new_value: str):
    """Modify one field in TDTP XML pipe-separated rows where field[0] == id_val.

    Parses <R>v0|v1|v2|...</R> elements using regex, changes field at field_idx
    in the first matching row (where field[0] == id_val), writes result to dst.
    Does not affect rows where field[0] != id_val.
    """
    with open(src) as fh:
        content = fh.read()

    def _replacer(m: "re.Match[str]") -> str:
        vals = m.group(1).split("|")
        if vals[0] == id_val:
            vals[field_idx] = new_value
        return "<R>" + "|".join(vals) + "</R>"

    new_content = re.sub(r"<R>([^<]+)</R>", _replacer, content)
    with open(dst, "w") as fh:
        fh.write(new_content)


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

    # Table with spaces and special characters in column names (MSSQL/Access export style)
    c.execute("""CREATE TABLE "complex_fields" (
        "Order ID" INTEGER PRIMARY KEY,
        "Customer Name" TEXT,
        "Total Cost $" REAL,
        "Discount %" REAL,
        "Is Active?" INTEGER
    )""")
    c.executemany('INSERT INTO "complex_fields" VALUES (?,?,?,?,?)', [
        (1, "Alice",  150.00, 0.10, 1),
        (2, "Bob",    200.00, 0.00, 1),
        (3, "Carol",   80.00, 0.20, 0),
        (4, "Dave",   320.00, 0.05, 1),
        (5, "Eve",     50.00, 0.00, 0),
    ])

    # Table with $ in the table name (NAV/BC-style ERP tables)
    c.execute("""CREATE TABLE "ERP$Entry" (
        "No_" INTEGER PRIMARY KEY,
        "Document Type" INTEGER,
        "Posting Date" DATE,
        "Description" TEXT,
        "Amount" REAL
    )""")
    c.executemany('INSERT INTO "ERP$Entry" VALUES (?,?,?,?,?)', [
        (1, 1, "2025-01-10", "Invoice payment",   1200.00),
        (2, 2, "2025-01-15", "Credit memo",        -300.00),
        (3, 1, "2025-02-03", "Invoice payment",    800.00),
        (4, 3, "2025-02-20", "Reminder fee",         15.00),
        (5, 1, "2025-03-01", "Invoice payment",   2500.00),
        (6, 2, "2025-03-10", "Credit memo",       -150.00),
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

    # T2.8 — bracket-quoted field name (MSSQL/Access style): [Order ID] > 3 → rows 4,5
    t = time.monotonic()
    p = run("--export", "complex_fields",
            "--where", "[Order ID] > 3",
            "--output", out("t2_8.xml"))
    rows = count_rows_xml(out("t2_8.xml"))
    record("T2.8 bracket-quoted WHERE [Order ID] > 3 → 2 rows",
           p.returncode == 0 and rows == 2,
           time.monotonic() - t, f"rc={p.returncode} rows={rows}")

    # T2.9 — bracket-quoted name with special char: [Is Active?] = 1 → rows 1,2,4
    t = time.monotonic()
    p = run("--export", "complex_fields",
            "--where", "[Is Active?] = 1",
            "--output", out("t2_9.xml"))
    rows = count_rows_xml(out("t2_9.xml"))
    record("T2.9 bracket-quoted WHERE [Is Active?] = 1 → 3 rows",
           p.returncode == 0 and rows == 3,
           time.monotonic() - t, f"rc={p.returncode} rows={rows}")


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

    # T4.6 — bracket-quoted table name with $ (ERP$Entry) export → import
    t = time.monotonic()
    run("--export", "[ERP$Entry]", "--output", out("t4_erp.xml"))
    p = _import(out("t4_erp.xml"), "erp_entry_copy")
    rows = sqlite_query(IMPORT_DB, "SELECT COUNT(*) FROM erp_entry_copy")[0][0] \
           if p.returncode == 0 else -1
    record("T4.6 bracket-quoted table [ERP$Entry] roundtrip: 6 rows",
           p.returncode == 0 and rows == 6,
           time.monotonic() - t, f"rc={p.returncode} rows={rows}")

    # T4.7 — bracket-quoted --fields with spaces and $ from complex_fields
    t = time.monotonic()
    run("--export", "[complex_fields]",
        "--fields", "[Order ID],[Customer Name],[Total Cost $]",
        "--output", out("t4_complex_proj.xml"))
    p = _import(out("t4_complex_proj.xml"), "complex_proj")
    cols = [c[1] for c in sqlite_query(IMPORT_DB, "PRAGMA table_info(complex_proj)")] \
           if p.returncode == 0 else []
    expected = ["Order ID", "Customer Name", "Total Cost $"]
    record("T4.7 bracket-quoted --fields [Order ID],[Customer Name],[Total Cost $]",
           p.returncode == 0 and cols == expected,
           time.monotonic() - t, f"cols={cols}")

    # T4.8 — bracket-quoted --where filter on field with $ (3 rows where Total Cost $ > 100)
    t = time.monotonic()
    p = run("--export", "[complex_fields]",
            "--where", "[Total Cost $] > 100",
            "--output", out("t4_complex_where.xml"))
    rows = count_rows_xml(out("t4_complex_where.xml"))
    record("T4.8 --where [Total Cost $] > 100 → 3 rows",
           p.returncode == 0 and rows == 3,
           time.monotonic() - t, f"rc={p.returncode} rows={rows}")

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


# ─── T8 S3 Object Storage ─────────────────────────────────────────────────────

def s3_available() -> bool:
    """Return True if SeaweedFS S3 gateway is responding at S3_ENDPOINT.

    Unauthenticated GET / returns HTTP 403 AccessDenied when IAM is enabled —
    that still means the gateway is up.  Accept both the anonymous case
    (ListAllMyBucketsResult) and the IAM-enforced case (403 / AccessDenied).
    """
    try:
        req = urllib.request.Request(S3_ENDPOINT + "/")
        try:
            with urllib.request.urlopen(req, timeout=2) as r:
                body = r.read(512)
                return b"ListAllMyBucketsResult" in body or b"AccessDenied" in body
        except urllib.error.HTTPError as e:
            return e.code in (400, 403)   # auth required → gateway is up
    except Exception:
        return False


def write_s3_cfg(path: str, db: str = TEST_DB):
    """Write a SQLite config YAML with S3 storage section."""
    with open(path, "w") as f:
        f.write(f"database:\n  type: sqlite\n  database: {db}\n")
        f.write(f"storage:\n  type: s3\n  s3:\n")
        f.write(f"    endpoint: \"{S3_ENDPOINT}\"\n")
        f.write(f"    region: \"us-east-1\"\n")
        f.write(f"    bucket: \"{S3_BUCKET}\"\n")
        f.write(f"    access_key: \"{S3_ACCESS_KEY}\"\n")
        f.write(f"    secret_key: \"{S3_SECRET_KEY}\"\n")
        f.write(f"    path_style: true\n")
        f.write(f"    disable_ssl: true\n")


def write_etl_s3_pipeline(path: str, algo: str = "kanzi", level: int = 6):
    """Write an ETL pipeline YAML: SQLite → S3 with given compress_algo.

    Uses a cross-join to produce 100 rows (~10KB) — enough to exceed the
    ETL compressor's 1KB minimum threshold.
    """
    dest = f"s3://{S3_BUCKET}/{S3_PREFIX}/etl_{algo}.tdtp.xml"
    with open(path, "w") as f:
        f.write(f"""name: test-s3-etl
version: "1.0"
sources:
  - name: users
    type: sqlite
    dsn: {TEST_DB}
    query: "SELECT a.ID*10+b.ID AS ID, a.Name, a.Email, a.Balance, a.IsActive, a.City, a.CreatedAt, a.LastLoginAt FROM users a, users b LIMIT 100"
workspace:
  type: sqlite
  mode: memory
transform:
  sql: "SELECT * FROM users"
  result_table: result
output:
  type: tdtp
  tdtp:
    format: xml
    compress: true
    compress_algo: {algo}
    compress_level: {level}
    destination: "{dest}"
    s3:
      endpoint: "{S3_ENDPOINT}"
      region: "us-east-1"
      bucket: "{S3_BUCKET}"
      access_key: "{S3_ACCESS_KEY}"
      secret_key: "{S3_SECRET_KEY}"
      path_style: true
      disable_ssl: true
""")
    return dest


def test_T8_s3():
    print(f"\n{BOLD}=== T8 S3 Object Storage ==={RESET}")

    if not s3_available():
        print(f"  {YELLOW}SKIP{RESET} S3 not available at {S3_ENDPOINT}")
        print(f"        Start weed: see CLAUDE.md → SeaweedFS S3 section")
        return

    write_s3_cfg(CFG_S3)
    s3_plain = f"s3://{S3_BUCKET}/{S3_PREFIX}/users_plain.tdtp.xml"
    s3_hash  = f"s3://{S3_BUCKET}/{S3_PREFIX}/users_hash.tdtp.xml"

    # T8.1 — export to S3 → --inspect shows table name
    t = time.monotonic()
    p  = run("--export", "users", "--output", s3_plain, cfg=CFG_S3)
    pi = run("--inspect", s3_plain, cfg=CFG_S3)
    has_table = "users" in pi.stdout
    record("T8.1 export to S3 → --inspect shows table",
           p.returncode == 0 and has_table,
           time.monotonic() - t, f"rc={p.returncode} inspect_ok={has_table}")

    # T8.2 — export --compress --hash → --test s3:// checksum OK
    t = time.monotonic()
    p  = run("--export", "users", "--compress", "--hash",
             "--output", s3_hash, cfg=CFG_S3)
    pt = run("--test", s3_hash, cfg=CFG_S3)
    checksum_ok = "checksum OK" in pt.stdout
    record("T8.2 S3 export --hash → --test checksum OK",
           p.returncode == 0 and pt.returncode == 0 and checksum_ok,
           time.monotonic() - t, f"test_rc={pt.returncode}")

    # T8.3 — import from S3 → 10 rows roundtrip
    t = time.monotonic()
    import_db = "/tmp/tdtp_s3_import.db"
    if os.path.exists(import_db):
        os.remove(import_db)
    write_s3_cfg("/tmp/tdtp_s3_import_cfg.yaml", db=import_db)
    p = subprocess.run(
        [TDTPCLI, "--config", "/tmp/tdtp_s3_import_cfg.yaml",
         "--import", s3_plain, "--table", "users_s3"],
        capture_output=True, text=True, timeout=30,
    )
    rows = sqlite_query(import_db, "SELECT COUNT(*) FROM users_s3")[0][0] \
           if p.returncode == 0 else -1
    record("T8.3 import from S3 → 10 rows roundtrip",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rows={rows}")
    if os.path.exists(import_db):
        os.remove(import_db)

    # T8.4 — ETL pipeline with compress_algo: kanzi → --inspect shows kanzi
    #         (verifies the ETL compress_algo bug fix in pkg/etl/exporter.go)
    t = time.monotonic()
    s3_kanzi = write_etl_s3_pipeline(CFG_ETL_S3, algo="kanzi", level=6)
    p  = subprocess.run(
        [TDTPCLI, "--pipeline", CFG_ETL_S3],
        capture_output=True, text=True, timeout=60,
    )
    pi = run("--inspect", s3_kanzi, cfg=CFG_S3)
    algo_ok = "kanzi" in pi.stdout
    record("T8.4 ETL compress_algo: kanzi → inspect shows kanzi",
           p.returncode == 0 and algo_ok,
           time.monotonic() - t, f"pipeline_rc={p.returncode} algo_ok={algo_ok}")

    # T8.5 — --test on nonexistent S3 key → error exit code
    t = time.monotonic()
    p = run("--test", f"s3://{S3_BUCKET}/ci/no_such_file_xyz.tdtp.xml", cfg=CFG_S3)
    record("T8.5 --test nonexistent S3 key → error",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode}")


# ─── T11 MSMQ Broker ─────────────────────────────────────────────────────────

# MSMQ queue paths (node zt-2075).  Override via env vars for other machines.
MSMQ_QUEUE = os.environ.get("TDTP_MSMQ_QUEUE", r".\private$\tdtp_test")
CFG_MSMQ_EXP = "/tmp/tdtp_msmq_export.yaml"
CFG_MSMQ_IMP = "/tmp/tdtp_msmq_import.yaml"


def msmq_available() -> bool:
    """Return True if MSMQ service is running on the local machine."""
    try:
        result = subprocess.run(
            ["powershell", "-NoProfile", "-Command",
             "(Get-Service -Name MSMQ -ErrorAction SilentlyContinue).Status"],
            capture_output=True, text=True, timeout=5,
        )
        return result.stdout.strip() == "Running"
    except Exception:
        return False


def write_msmq_cfg(path: str, db: str = TEST_DB, queue: str = MSMQ_QUEUE):
    """Write a SQLite config YAML with MSMQ broker section."""
    queue_yaml = queue.replace("\\", "\\\\")  # YAML requires \\ for literal backslash
    with open(path, "w") as f:
        f.write(f"database:\n  type: sqlite\n  database: {db}\n")
        f.write(f"broker:\n  type: msmq\n")
        f.write(f"  queue_path: \"{queue_yaml}\"\n")


def test_T11_msmq():
    print(f"\n{BOLD}=== T11 MSMQ Broker ==={RESET}")

    if not msmq_available():
        print(f"  {YELLOW}SKIP{RESET} MSMQ service not running")
        print(f"        Enable: Control Panel → Programs → Windows Features → MSMQ")
        return

    write_msmq_cfg(CFG_MSMQ_EXP)

    # T11.1 — export-broker → queue receives packets (exit 0)
    t = time.monotonic()
    p = run("--export-broker", "users", cfg=CFG_MSMQ_EXP, timeout=30)
    record("T11.1 export-broker users → exit 0",
           p.returncode == 0,
           time.monotonic() - t, p.stderr.strip()[:120] if p.returncode != 0 else "")

    # T11.2 — import-broker → 10 rows roundtrip in SQLite
    t = time.monotonic()
    import_db = "/tmp/tdtp_msmq_import.db"
    if os.path.exists(import_db):
        os.remove(import_db)
    write_msmq_cfg(CFG_MSMQ_IMP, db=import_db)
    p = subprocess.run(
        [TDTPCLI, "--config", CFG_MSMQ_IMP,
         "--import-broker", "--table", "users_msmq"],
        capture_output=True, text=True, timeout=30,
    )
    rows = -1
    if p.returncode == 0 and os.path.exists(import_db):
        try:
            rows = sqlite_query(import_db, "SELECT COUNT(*) FROM users_msmq")[0][0]
        except Exception:
            pass
    record("T11.2 import-broker → 10 rows roundtrip",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rows={rows}")
    if os.path.exists(import_db):
        os.remove(import_db)

    # T11.3 — export-broker --compress → import-broker decompresses correctly
    t = time.monotonic()
    import_db2 = "/tmp/tdtp_msmq_import2.db"
    if os.path.exists(import_db2):
        os.remove(import_db2)
    p_exp = run("--export-broker", "users", "--compress", cfg=CFG_MSMQ_EXP, timeout=30)
    write_msmq_cfg(CFG_MSMQ_IMP, db=import_db2)
    p_imp = subprocess.run(
        [TDTPCLI, "--config", CFG_MSMQ_IMP,
         "--import-broker", "--table", "users_msmq_c"],
        capture_output=True, text=True, timeout=30,
    )
    rows2 = -1
    if p_imp.returncode == 0 and os.path.exists(import_db2):
        try:
            rows2 = sqlite_query(import_db2, "SELECT COUNT(*) FROM users_msmq_c")[0][0]
        except Exception:
            pass
    record("T11.3 export-broker --compress → import-broker roundtrip",
           p_exp.returncode == 0 and p_imp.returncode == 0 and rows2 == 10,
           time.monotonic() - t, f"rows={rows2}")
    if os.path.exists(import_db2):
        os.remove(import_db2)

    # T11.4 — export-broker to nonexistent queue → non-zero exit
    t = time.monotonic()
    bad_cfg = "/tmp/tdtp_msmq_bad.yaml"
    write_msmq_cfg(bad_cfg)
    with open(bad_cfg, "a") as f:
        pass
    # Overwrite with bad queue path
    with open(bad_cfg, "w") as f:
        f.write("database:\n  type: sqlite\n  database: /tmp/no.db\n")
        f.write("broker:\n  type: msmq\n  queue_path: \".\\\\private$\\\\tdtp_no_such_queue_xyz\"\n")
    p = run("--export-broker", "users", cfg=bad_cfg, timeout=10)
    record("T11.4 export-broker bad queue → non-zero exit",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode}")


# ─── T9 Diff ─────────────────────────────────────────────────────────────────

def test_T9_diff():
    print(f"\n{BOLD}=== T9 Diff ==={RESET}")

    # ── Setup: export files used across all T9 subtests ──────────────────────
    # Full 10-row users export (used as "baseline A")
    run("--export", "users", "--output", out("t9_all.xml"))
    # Subset: first 7 rows (IDs 1–7)
    run("--export", "users", "--where", "ID <= 7", "--output", out("t9_7.xml"))
    # Modified: change Balance (field index 3) of ID=1 from its original value to 9999
    #   users schema: ID | Name | Email | Balance | IsActive | City | CreatedAt | LastLoginAt
    #   field index:   0     1      2        3          4        5        6             7
    modify_xml_field(out("t9_all.xml"), out("t9_modif.xml"), "1", 3, "9999")

    # T9.1 — identical exports: same file diffed against itself
    t = time.monotonic()
    p = run("--diff", out("t9_all.xml"), out("t9_all.xml"))
    record("T9.1 diff identical files → 'Files are identical'",
           p.returncode == 0
           and "Files are identical" in p.stdout
           and "Added:      0" in p.stdout
           and "Removed:    0" in p.stdout,
           time.monotonic() - t, p.stdout.strip()[-80:])

    # T9.2 — A has 10 rows, B has 7 → 3 rows removed in B relative to A
    t = time.monotonic()
    p = run("--diff", out("t9_all.xml"), out("t9_7.xml"))
    record("T9.2 diff A(10) vs B(7) → Removed: 3, Files differ",
           p.returncode == 0
           and "Removed:    3" in p.stdout
           and "Files differ" in p.stdout,
           time.monotonic() - t, p.stdout.strip()[-80:])

    # T9.3 — A has 7 rows, B has 10 → 3 rows added in B relative to A
    t = time.monotonic()
    p = run("--diff", out("t9_7.xml"), out("t9_all.xml"))
    record("T9.3 diff A(7) vs B(10) → Added: 3, Files differ",
           p.returncode == 0
           and "Added:      3" in p.stdout
           and "Files differ" in p.stdout,
           time.monotonic() - t, p.stdout.strip()[-80:])

    # T9.4 — one row has a modified field value → Modified: 1
    t = time.monotonic()
    p = run("--diff", out("t9_all.xml"), out("t9_modif.xml"))
    record("T9.4 diff with one modified Balance → Modified: 1",
           p.returncode == 0
           and "Modified:   1" in p.stdout
           and "Files differ" in p.stdout,
           time.monotonic() - t, p.stdout.strip()[-80:])

    # T9.5 — --ignore-fields skips the changed field → identical despite modification
    # NOTE: filter flags (--ignore-fields, --key-fields) must appear BEFORE --diff
    # because the second file path is a positional arg consumed by flag.Args();
    # flags placed AFTER the second positional get re-parsed, clearing flag.Args().
    t = time.monotonic()
    p = run("--ignore-fields", "Balance",
            "--diff", out("t9_all.xml"), out("t9_modif.xml"))
    record("T9.5 --ignore-fields Balance → Files are identical",
           p.returncode == 0 and "Files are identical" in p.stdout,
           time.monotonic() - t, p.stdout.strip()[-60:])

    # T9.6 — --key-fields explicit (overrides schema auto-detect), identical files
    t = time.monotonic()
    p = run("--key-fields", "ID",
            "--diff", out("t9_all.xml"), out("t9_all.xml"))
    record("T9.6 --key-fields ID explicit → 'Files are identical'",
           p.returncode == 0 and "Files are identical" in p.stdout,
           time.monotonic() - t, f"rc={p.returncode}")

    # T9.7 — error: first file does not exist → exit != 0
    t = time.monotonic()
    p = run("--diff", out("t9_nonexistent_xyz.xml"), out("t9_all.xml"))
    record("T9.7 diff nonexistent file → error exit code",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode}")


# ─── T10 Merge ────────────────────────────────────────────────────────────────

def test_T10_merge():
    print(f"\n{BOLD}=== T10 Merge ==={RESET}")

    # ── Setup: export slices of users for various overlap scenarios ───────────
    # Non-overlapping halves: IDs 1–5 and IDs 6–10
    run("--export", "users", "--where", "ID <= 5",
        "--output", out("t10_1to5.xml"))    # 5 rows
    run("--export", "users", "--where", "ID > 5",
        "--output", out("t10_6to10.xml"))   # 5 rows

    # Overlapping halves: IDs 1–6 and IDs 5–10 (IDs 5,6 in both)
    run("--export", "users", "--where", "ID <= 6",
        "--output", out("t10_1to6.xml"))    # 6 rows
    run("--export", "users", "--where", "ID >= 5",
        "--output", out("t10_5to10.xml"))   # 6 rows

    # Three non-overlapping parts for 3-file union test
    run("--export", "users", "--where", "ID <= 3",
        "--output", out("t10_p1.xml"))      # 3 rows: IDs 1,2,3
    run("--export", "users", "--where", "ID > 3", "--where", "ID <= 7",
        "--output", out("t10_p2.xml"))      # 4 rows: IDs 4,5,6,7
    run("--export", "users", "--where", "ID > 7",
        "--output", out("t10_p3.xml"))      # 3 rows: IDs 8,9,10

    # Different table for schema-mismatch error test
    run("--export", "orders", "--output", out("t10_orders.xml"))

    # T10.1 — union of non-overlapping halves → 10 unique rows, no duplicates
    t = time.monotonic()
    files = f"{out('t10_1to5.xml')},{out('t10_6to10.xml')}"
    p = run("--merge", files, "--output", out("t10_union_nooverlap.xml"))
    rows = count_rows_xml(out("t10_union_nooverlap.xml")) if p.returncode == 0 else -1
    record("T10.1 union non-overlapping (5+5) → 10 rows",
           p.returncode == 0 and rows == 10
           and "Merged file saved" in p.stdout
           and "Duplicates:     0" in p.stdout,
           time.monotonic() - t, f"rows={rows}")

    # T10.2 — union of overlapping halves (IDs 5,6 shared) → 10 unique rows, 2 duplicates
    t = time.monotonic()
    files = f"{out('t10_1to6.xml')},{out('t10_5to10.xml')}"
    p = run("--merge", files, "--output", out("t10_union_overlap.xml"))
    rows = count_rows_xml(out("t10_union_overlap.xml")) if p.returncode == 0 else -1
    record("T10.2 union overlapping (6+6, 2 shared) → 10 rows, Duplicates: 2",
           p.returncode == 0 and rows == 10 and "Duplicates:     2" in p.stdout,
           time.monotonic() - t, f"rows={rows}")

    # T10.3 — intersection of overlapping → only rows present in both = 2 rows (IDs 5,6)
    t = time.monotonic()
    files = f"{out('t10_1to6.xml')},{out('t10_5to10.xml')}"
    p = run("--merge", files, "--merge-strategy", "intersection",
            "--output", out("t10_intersection.xml"))
    rows = count_rows_xml(out("t10_intersection.xml")) if p.returncode == 0 else -1
    record("T10.3 intersection → 2 rows (IDs 5,6 only)",
           p.returncode == 0 and rows == 2,
           time.monotonic() - t, f"rows={rows}")

    # T10.4 — append: all rows concatenated without dedup → 6+6=12 rows
    t = time.monotonic()
    files = f"{out('t10_1to6.xml')},{out('t10_5to10.xml')}"
    p = run("--merge", files, "--merge-strategy", "append",
            "--output", out("t10_append.xml"))
    rows = count_rows_xml(out("t10_append.xml")) if p.returncode == 0 else -1
    record("T10.4 append (no dedup) → 12 rows",
           p.returncode == 0 and rows == 12,
           time.monotonic() - t, f"rows={rows}")

    # T10.5 — left priority with --show-conflicts: duplicates keep first file's row
    #   IDs 5,6 appear in both files (same values) → conflict registered, kept_existing
    t = time.monotonic()
    files = f"{out('t10_1to6.xml')},{out('t10_5to10.xml')}"
    p = run("--merge", files, "--merge-strategy", "left", "--show-conflicts",
            "--output", out("t10_left.xml"))
    rows = count_rows_xml(out("t10_left.xml")) if p.returncode == 0 else -1
    kept_ok = "kept_existing" in p.stdout
    record("T10.5 left priority + --show-conflicts → 10 rows, 'kept_existing'",
           p.returncode == 0 and rows == 10 and kept_ok,
           time.monotonic() - t, f"rows={rows} kept={kept_ok}")

    # T10.6 — right priority with --show-conflicts: duplicates overwritten by second file
    t = time.monotonic()
    files = f"{out('t10_1to6.xml')},{out('t10_5to10.xml')}"
    p = run("--merge", files, "--merge-strategy", "right", "--show-conflicts",
            "--output", out("t10_right.xml"))
    rows = count_rows_xml(out("t10_right.xml")) if p.returncode == 0 else -1
    used_ok = "used_new" in p.stdout
    record("T10.6 right priority + --show-conflicts → 10 rows, 'used_new'",
           p.returncode == 0 and rows == 10 and used_ok,
           time.monotonic() - t, f"rows={rows} used={used_ok}")

    # T10.7 — 3-file union (non-overlapping: 3+4+3=10) → 10 rows, Packets merged: 3
    t = time.monotonic()
    files = f"{out('t10_p1.xml')},{out('t10_p2.xml')},{out('t10_p3.xml')}"
    p = run("--merge", files, "--output", out("t10_3way.xml"))
    rows = count_rows_xml(out("t10_3way.xml")) if p.returncode == 0 else -1
    packets_ok = "Packets merged: 3" in p.stdout
    record("T10.7 3-file union (3+4+3) → 10 rows, 'Packets merged: 3'",
           p.returncode == 0 and rows == 10 and packets_ok,
           time.monotonic() - t, f"rows={rows} pkt={packets_ok}")

    # T10.8 — error: only 1 file provided (no comma) → "merge requires at least 2 files"
    t = time.monotonic()
    p = run("--merge", out("t10_1to5.xml"), "--output", out("t10_err1.xml"))
    record("T10.8 merge with 1 file → error exit code",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode}")

    # T10.9 — error: incompatible table names (users vs orders) → exit != 0
    t = time.monotonic()
    files = f"{out('t10_1to5.xml')},{out('t10_orders.xml')}"
    p = run("--merge", files, "--output", out("t10_err2.xml"))
    record("T10.9 merge incompatible table names → error exit code",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode}")


# ─── Runner ───────────────────────────────────────────────────────────────────

GROUPS = [
    ("T1", test_T1_basic_export),
    ("T2", test_T2_filters),
    ("T3", test_T3_compression),
    ("T4", test_T4_roundtrip),
    ("T5", test_T5_integrity),
    ("T6", test_T6_edge_cases),
    ("T7", test_T7_compact),
    ("T8", test_T8_s3),
    ("T9", test_T9_diff),
    ("T10", test_T10_merge),
    ("T11", test_T11_msmq),
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
