#!/usr/bin/env python3
"""
TDTP CLI Integration Tests — MySQL source

Same test matrix as test_sqlite.py but against MySQL 8 in Docker.
Tests cover: basic export, TDTQL filters, compression, export/import
roundtrip (MySQL→MySQL, MySQL→SQLite), file integrity, edge cases,
compact format, diff, merge.

Prerequisites:
    docker compose up -d mysql          # starts tdtp-mysql-test on port 3306
    # no extra scripts needed — setup_db() creates test data inline

Usage:
    python3 tests/cli/test_mysql.py          # all groups
    python3 tests/cli/test_mysql.py T3       # single group
    TDTPCLI_BIN=/path/to/tdtpcli python3 tests/cli/test_mysql.py

Environment overrides:
    TDTPCLI_BIN        path to tdtpcli binary    (default: /tmp/tdtpcli)
    MYSQL_CONTAINER    Docker container name      (default: tdtp-mysql-test)
    MYSQL_HOST         MySQL host                 (default: 127.0.0.1)
    MYSQL_PORT         MySQL port                 (default: 3306)
    MYSQL_USER         MySQL user                 (default: tdtp_test)
    MYSQL_PASSWORD     MySQL password             (default: tdtp_test_password)
    MYSQL_DB           MySQL database             (default: tdtp_test_db)
"""

import os
import re
import sys
import time
import shutil
import sqlite3
import subprocess
import xml.etree.ElementTree as ET
from pathlib import Path

# Force UTF-8 output so → and other Unicode chars work on Windows cp1251 terminals
if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")

# ─── Configuration ────────────────────────────────────────────────────────────
TDTPCLI          = os.environ.get("TDTPCLI_BIN",     "/tmp/tdtpcli")
OUTDIR           = Path("/tmp/tdtp_mysql_test_out")
CFG              = "/tmp/tdtp_mysql_test.yaml"
CFG_C            = "/tmp/tdtp_mysql_compress.yaml"
CFG_IMP          = "/tmp/tdtp_mysql_import.yaml"       # import target: MySQL
CFG_IMP_SQLITE   = "/tmp/tdtp_mysql_import_sqlite.yaml"# import target: SQLite

MYSQL_CONTAINER  = os.environ.get("MYSQL_CONTAINER", "tdtp-mysql-test")
MYSQL_HOST       = os.environ.get("MYSQL_HOST",      "127.0.0.1")
MYSQL_PORT       = int(os.environ.get("MYSQL_PORT",  "3306"))
MYSQL_USER       = os.environ.get("MYSQL_USER",      "tdtp_test")
MYSQL_PASSWORD   = os.environ.get("MYSQL_PASSWORD",  "tdtp_test_password")
MYSQL_DB         = os.environ.get("MYSQL_DB",        "tdtp_test_db")

SQLITE_IMPORT_DB = "/tmp/tdtp_mysql_import_sqlite.db"

# ─── ANSI colors ──────────────────────────────────────────────────────────────
GREEN  = "\033[32m"
RED    = "\033[31m"
YELLOW = "\033[33m"
BOLD   = "\033[1m"
RESET  = "\033[0m"

# ─── Global results ───────────────────────────────────────────────────────────
results: list = []


# ─── MySQL helpers (via docker exec — no pymysql needed) ─────────────────────

def mysql_exec(sql: str, timeout: int = 15) -> str:
    """Execute SQL in the MySQL container, return stdout (stripped)."""
    p = subprocess.run(
        ["docker", "exec", MYSQL_CONTAINER,
         "mysql", "-u", MYSQL_USER, f"-p{MYSQL_PASSWORD}", MYSQL_DB,
         "-N", "--silent", "-e", sql],
        capture_output=True, text=True, timeout=timeout,
    )
    return p.stdout.strip()


def mysql_exec_multi(sql: str, timeout: int = 30) -> bool:
    """Pipe a multi-statement SQL script to MySQL via stdin. Returns True on success."""
    p = subprocess.run(
        ["docker", "exec", "-i", MYSQL_CONTAINER,
         "mysql", "-u", MYSQL_USER, f"-p{MYSQL_PASSWORD}", MYSQL_DB],
        input=sql, capture_output=True, text=True, timeout=timeout,
    )
    if p.returncode != 0:
        raise RuntimeError(f"mysql_exec_multi failed:\n{p.stderr}")
    return True


def mysql_count(table: str) -> int:
    """Return row count from MySQL table, -1 on error."""
    try:
        return int(mysql_exec(f"SELECT COUNT(*) FROM `{table}`;").strip())
    except (ValueError, Exception):
        return -1


def mysql_columns(table: str) -> list:
    """Return ordered list of column names from MySQL table."""
    result = mysql_exec(
        f"SELECT COLUMN_NAME FROM INFORMATION_SCHEMA.COLUMNS "
        f"WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='{table}' "
        f"ORDER BY ORDINAL_POSITION;"
    )
    return [c.strip() for c in result.splitlines() if c.strip()]


def mysql_available() -> bool:
    """Return True if MySQL container is reachable."""
    try:
        p = subprocess.run(
            ["docker", "exec", MYSQL_CONTAINER,
             "mysql", "-u", MYSQL_USER, f"-p{MYSQL_PASSWORD}", MYSQL_DB,
             "-N", "-e", "SELECT 1;"],
            capture_output=True, text=True, timeout=10,
        )
        return p.returncode == 0 and "1" in p.stdout
    except Exception:
        return False


def setup_db():
    """Create test tables and insert fixture data in MySQL — no external script needed."""
    sql = """
DROP TABLE IF EXISTS `users`;
CREATE TABLE `users` (
    `ID`          INT            NOT NULL AUTO_INCREMENT PRIMARY KEY,
    `Name`        VARCHAR(100)   NOT NULL,
    `Email`       VARCHAR(100),
    `Balance`     DECIMAL(18,2),
    `IsActive`    TINYINT(1),
    `City`        VARCHAR(50),
    `CreatedAt`   DATETIME,
    `LastLoginAt` DATETIME
);
INSERT INTO `users` VALUES
 (1,  'John Doe',      'john@example.com',    1500.00, 1, 'Moscow', '2025-01-15 10:00:00', '2025-11-10 15:30:00'),
 (2,  'Jane Smith',    'jane@example.com',    2000.00, 1, 'SPb',    '2025-02-20 11:00:00', '2025-11-12 09:15:00'),
 (3,  'Bob Johnson',   'bob@example.com',      500.00, 0, 'Moscow', '2025-03-10 12:00:00', '2025-10-05 14:20:00'),
 (4,  'Alice Brown',   'alice@example.com',   2500.00, 1, 'Kazan',  '2025-01-05 09:00:00', '2025-11-13 11:45:00'),
 (5,  'Charlie Davis', 'charlie@example.com',  800.00, 1, 'SPb',    '2025-04-12 13:00:00', '2025-11-11 16:30:00'),
 (6,  'Emma Wilson',   'emma@example.com',    3000.00, 1, 'Moscow', '2024-12-20 10:00:00', '2025-11-14 08:00:00'),
 (7,  'Frank Miller',  'frank@example.com',   1200.00, 0, 'Moscow', '2025-05-18 14:00:00', '2025-09-20 10:10:00'),
 (8,  'Grace Lee',     'grace@example.com',   1800.00, 1, 'SPb',    '2025-02-28 15:00:00', '2025-11-13 12:20:00'),
 (9,  'Henry Taylor',  'henry@example.com',    400.00, 1, 'Kazan',  '2025-06-01 16:00:00', '2025-11-09 17:30:00'),
 (10, 'Ivy Anderson',  'ivy@example.com',     2200.00, 1, 'Moscow', '2025-01-30 17:00:00', '2025-11-14 07:45:00');

DROP TABLE IF EXISTS `orders`;
CREATE TABLE `orders` (
    `OrderID`     INT           NOT NULL AUTO_INCREMENT PRIMARY KEY,
    `UserID`      INT,
    `ProductName` VARCHAR(100),
    `Amount`      DECIMAL(18,2),
    `Status`      VARCHAR(20),
    `CreatedAt`   DATETIME
);
INSERT INTO `orders` VALUES
 (1, 1, 'Laptop',     1500.00, 'completed', '2025-11-01 10:00:00'),
 (2, 2, 'Phone',       800.00, 'pending',   '2025-11-05 11:30:00'),
 (3, 4, 'Tablet',      600.00, 'completed', '2025-11-03 14:15:00'),
 (4, 6, 'Monitor',     400.00, 'pending',   '2025-11-10 09:20:00'),
 (5, 8, 'Keyboard',    100.00, 'completed', '2025-11-08 16:45:00'),
 (6,10, 'Mouse',        50.00, 'pending',   '2025-11-12 12:30:00'),
 (7, 1, 'Headphones',  200.00, 'cancelled', '2025-11-02 13:00:00'),
 (8, 2, 'Webcam',      150.00, 'completed', '2025-11-09 10:10:00');

DROP TABLE IF EXISTS `products`;
CREATE TABLE `products` (
    `ProductID`   INT           NOT NULL AUTO_INCREMENT PRIMARY KEY,
    `Name`        VARCHAR(100)  NOT NULL,
    `Category`    VARCHAR(50),
    `Price`       DECIMAL(18,2),
    `Stock`       INT,
    `IsAvailable` TINYINT(1),
    `UpdatedAt`   DATETIME
);
INSERT INTO `products` VALUES
 (1,  'Laptop Pro 15',       'Electronics',  1500.00, 10,  1, '2025-11-01 10:00:00'),
 (2,  'Smartphone X',        'Electronics',   800.00, 25,  1, '2025-11-05 11:00:00'),
 (3,  'Tablet Ultra',        'Electronics',   600.00, 15,  1, '2025-11-03 12:00:00'),
 (4,  'Monitor 27inch',      'Electronics',   400.00,  8,  1, '2025-11-10 13:00:00'),
 (5,  'Mechanical Keyboard', 'Accessories',   100.00, 50,  1, '2025-11-08 14:00:00'),
 (6,  'Wireless Mouse',      'Accessories',    50.00,100,  1, '2025-11-12 15:00:00'),
 (7,  'USB-C Hub',           'Accessories',    80.00, 30,  1, '2025-11-07 16:00:00'),
 (8,  'Webcam HD',           'Electronics',   150.00, 12,  1, '2025-11-09 17:00:00'),
 (9,  'Headphones Pro',      'Audio',         200.00, 20,  1, '2025-11-02 18:00:00'),
 (10, 'Speakers',            'Audio',         300.00,  5,  0, '2025-10-15 19:00:00');

DROP TABLE IF EXISTS `complex_fields`;
CREATE TABLE `complex_fields` (
    `Order ID`      INT         NOT NULL PRIMARY KEY,
    `Customer Name` VARCHAR(50),
    `Total Cost $`  DOUBLE,
    `Discount %`    DOUBLE,
    `Is Active?`    TINYINT(1)
);
INSERT INTO `complex_fields` VALUES
 (1, 'Alice',  150.00, 0.10, 1),
 (2, 'Bob',    200.00, 0.00, 1),
 (3, 'Carol',   80.00, 0.20, 0),
 (4, 'Dave',   320.00, 0.05, 1),
 (5, 'Eve',     50.00, 0.00, 0);

DROP TABLE IF EXISTS `ERP$Entry`;
CREATE TABLE `ERP$Entry` (
    `No_`           INT        NOT NULL PRIMARY KEY,
    `Document Type` INT,
    `Posting Date`  DATE,
    `Description`   VARCHAR(100),
    `Amount`        DOUBLE
);
INSERT INTO `ERP$Entry` VALUES
 (1, 1, '2025-01-10', 'Invoice payment',   1200.00),
 (2, 2, '2025-01-15', 'Credit memo',        -300.00),
 (3, 1, '2025-02-03', 'Invoice payment',    800.00),
 (4, 3, '2025-02-20', 'Reminder fee',         15.00),
 (5, 1, '2025-03-01', 'Invoice payment',   2500.00),
 (6, 2, '2025-03-10', 'Credit memo',       -150.00);
"""
    mysql_exec_multi(sql)


# ─── General helpers ──────────────────────────────────────────────────────────

def run(*args, cfg=None, timeout=30) -> subprocess.CompletedProcess:
    cmd = [TDTPCLI, "--config", cfg or CFG] + list(args)
    return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)


def run_no_cfg(*args, timeout=30) -> subprocess.CompletedProcess:
    cmd = [TDTPCLI] + list(args)
    return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)


def count_rows_xml(path: str) -> int:
    if not os.path.exists(path):
        return -1
    try:
        root = ET.parse(path).getroot()
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
    conn = sqlite3.connect(db_path)
    try:
        return conn.execute(sql).fetchall()
    finally:
        conn.close()


def modify_xml_field(src: str, dst: str, id_val: str, field_idx: int, new_value: str):
    """Patch one field in TDTP XML pipe-rows where field[0] == id_val."""
    with open(src) as fh:
        content = fh.read()

    def _replacer(m: "re.Match[str]") -> str:
        vals = m.group(1).split("|")
        if vals[0] == id_val:
            vals[field_idx] = new_value
        return "<R>" + "|".join(vals) + "</R>"

    with open(dst, "w") as fh:
        fh.write(re.sub(r"<R>([^<]+)</R>", _replacer, content))


def out(name: str) -> str:
    return str(OUTDIR / name)


def write_mysql_cfg(path: str, compress: bool = False,
                    algo: str = "zstd", level: int = 3):
    with open(path, "w") as f:
        f.write("database:\n")
        f.write("  type: mysql\n")
        f.write(f"  host: {MYSQL_HOST}\n")
        f.write(f"  port: {MYSQL_PORT}\n")
        f.write(f"  user: {MYSQL_USER}\n")
        f.write(f"  password: {MYSQL_PASSWORD}\n")
        f.write(f"  database: {MYSQL_DB}\n")
        f.write("export:\n")
        f.write(f"  compress: {str(compress).lower()}\n")
        f.write(f"  compress_algo: {algo}\n")
        f.write(f"  compress_level: {level}\n")


# keep old name as alias so no call sites break
def write_cfg(path: str, compress: bool = False,
              algo: str = "zstd", level: int = 3):
    write_mysql_cfg(path, compress=compress, algo=algo, level=level)


def write_sqlite_cfg(path: str, db: str):
    with open(path, "w") as f:
        f.write(f"database:\n  type: sqlite\n  database: {db}\n")


def record(tid: str, passed: bool, elapsed: float, msg: str = ""):
    results.append((tid, passed, elapsed, msg))
    status = f"{GREEN}PASS{RESET}" if passed else f"{RED}FAIL{RESET}"
    detail = f"  ({msg})" if msg and not passed else ""
    print(f"  [{status}] {tid:<50} {elapsed:.2f}s{detail}")


def _import(file: str, table: str, strategy: str = "replace") -> subprocess.CompletedProcess:
    """Import file into MySQL (CFG_IMP)."""
    return subprocess.run(
        [TDTPCLI, "--config", CFG_IMP,
         "--import", file, "--table", table, "--strategy", strategy],
        capture_output=True, text=True, timeout=30,
    )


def _import_sqlite(file: str, table: str,
                   strategy: str = "replace") -> subprocess.CompletedProcess:
    """Import file into SQLite (CFG_IMP_SQLITE)."""
    return subprocess.run(
        [TDTPCLI, "--config", CFG_IMP_SQLITE,
         "--import", file, "--table", table, "--strategy", strategy],
        capture_output=True, text=True, timeout=30,
    )


# ─── T1 Basic Export ──────────────────────────────────────────────────────────

def test_T1_basic_export():
    print(f"\n{BOLD}=== T1 Basic Export ==={RESET}")

    # T1.1 — export all rows
    t = time.monotonic()
    p = run("--export", "users", "--output", out("t1_users.xml"))
    rows = count_rows_xml(out("t1_users.xml"))
    record("T1.1 export all rows (users)",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rc={p.returncode} rows={rows}")

    # T1.2 — column projection
    t = time.monotonic()
    p = run("--export", "users", "--fields", "Name,Email",
            "--output", out("t1_fields.xml"))
    fields = get_schema_fields(out("t1_fields.xml"))
    record("T1.2 --fields Name,Email → 2 columns",
           p.returncode == 0 and fields == ["Name", "Email"],
           time.monotonic() - t, f"fields={fields}")

    # T1.3 — --list shows all tables
    t = time.monotonic()
    p = run("--list")
    tables_ok = all(tbl in p.stdout for tbl in ("users", "orders", "products"))
    record("T1.3 --list shows users, orders, products",
           p.returncode == 0 and tables_ok,
           time.monotonic() - t, p.stdout.strip()[:60])

    # T1.4 — export products (10 rows)
    t = time.monotonic()
    p = run("--export", "products", "--output", out("t1_products.xml"))
    rows = count_rows_xml(out("t1_products.xml"))
    record("T1.4 export all products (10 rows)",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rows={rows}")


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

    # T2.4 — IN operator
    t = time.monotonic()
    p = run("--export", "orders",
            "--where", "Status IN ('completed','pending')",
            "--output", out("t2_4.xml"))
    rows = count_rows_xml(out("t2_4.xml"))
    record("T2.4 WHERE Status IN ('completed','pending') → 7 rows",
           p.returncode == 0 and rows == 7,
           time.monotonic() - t, f"rows={rows}")

    # T2.5 — ORDER BY + LIMIT
    t = time.monotonic()
    p = run("--export", "users",
            "--order-by", "Balance DESC", "--limit", "3",
            "--output", out("t2_5.xml"))
    rows = count_rows_xml(out("t2_5.xml"))
    record("T2.5 ORDER BY Balance DESC LIMIT 3 → 3 rows",
           p.returncode == 0 and rows == 3,
           time.monotonic() - t, f"rows={rows}")

    # T2.6 — LIMIT + OFFSET
    t = time.monotonic()
    p = run("--export", "users",
            "--limit", "5", "--offset", "3",
            "--output", out("t2_6.xml"))
    rows = count_rows_xml(out("t2_6.xml"))
    record("T2.6 LIMIT 5 OFFSET 3 → 5 rows",
           p.returncode == 0 and rows == 5,
           time.monotonic() - t, f"rows={rows}")

    # T2.7 — negative LIMIT (tail mode)
    t = time.monotonic()
    p = run("--export", "users",
            "--limit", "-3",
            "--output", out("t2_7.xml"))
    rows = count_rows_xml(out("t2_7.xml"))
    record("T2.7 LIMIT -3 (last 3 rows, tail mode)",
           p.returncode == 0 and rows == 3,
           time.monotonic() - t, f"rows={rows}")

    # T2.8 — bracket-quoted field with space: [Order ID] > 3 → rows 4,5
    t = time.monotonic()
    p = run("--export", "[complex_fields]",
            "--where", "[Order ID] > 3",
            "--output", out("t2_8.xml"))
    rows = count_rows_xml(out("t2_8.xml"))
    record("T2.8 bracket-quoted WHERE [Order ID] > 3 → 2 rows",
           p.returncode == 0 and rows == 2,
           time.monotonic() - t, f"rows={rows}")

    # T2.9 — bracket-quoted field with ?: [Is Active?] = 1 → rows 1,2,4
    t = time.monotonic()
    p = run("--export", "[complex_fields]",
            "--where", "[Is Active?] = 1",
            "--output", out("t2_9.xml"))
    rows = count_rows_xml(out("t2_9.xml"))
    record("T2.9 bracket-quoted WHERE [Is Active?] = 1 → 3 rows",
           p.returncode == 0 and rows == 3,
           time.monotonic() - t, f"rows={rows}")


# ─── T3 Compression ───────────────────────────────────────────────────────────

def test_T3_compression():
    print(f"\n{BOLD}=== T3 Compression ==={RESET}")

    run("--export", "users", "--output", out("t3_baseline.xml"))
    baseline = os.path.getsize(out("t3_baseline.xml")) if os.path.exists(out("t3_baseline.xml")) else 0

    # T3.1 — zstd level 3
    t = time.monotonic()
    p = run("--export", "users", "--compress",
            "--output", out("t3_zstd3.xml"))
    sz = os.path.getsize(out("t3_zstd3.xml")) if p.returncode == 0 else 0
    pt = run_no_cfg("--test", out("t3_zstd3.xml"))
    record("T3.1 zstd level 3 (smaller + --test OK)",
           p.returncode == 0 and sz < baseline and pt.returncode == 0,
           time.monotonic() - t, f"sz={sz} baseline={baseline}")

    # T3.2 — zstd level 19
    t = time.monotonic()
    p = run("--export", "users", "--compress", "--compress-level", "19",
            "--output", out("t3_zstd19.xml"))
    sz19 = os.path.getsize(out("t3_zstd19.xml")) if p.returncode == 0 else 0
    record("T3.2 zstd level 19 smaller than uncompressed",
           p.returncode == 0 and sz19 < baseline,
           time.monotonic() - t, f"sz19={sz19}")

    # T3.3 — kanzi level 6
    t = time.monotonic()
    p = run("--export", "users",
            "--compress", "--compress-algo", "kanzi", "--compress-level", "6",
            "--output", out("t3_kanzi6.xml"))
    pt = run_no_cfg("--test", out("t3_kanzi6.xml"))
    record("T3.3 kanzi level 6 + --test OK",
           p.returncode == 0 and pt.returncode == 0,
           time.monotonic() - t, f"rc={p.returncode} test_rc={pt.returncode}")

    # T3.4 — --hash checksum
    t = time.monotonic()
    p = run("--export", "users", "--compress", "--hash",
            "--output", out("t3_hash.xml"))
    pt = run_no_cfg("--test", out("t3_hash.xml"))
    record("T3.4 --compress --hash → checksum OK in --test",
           p.returncode == 0 and pt.returncode == 0 and "checksum OK" in pt.stdout,
           time.monotonic() - t, pt.stdout.strip()[:80])

    # T3.5 — corrupt file → --test must fail
    t = time.monotonic()
    run("--export", "users", "--compress", "--hash",
        "--output", out("t3_corrupt_src.xml"))
    src = out("t3_corrupt_src.xml")
    dst = out("t3_corrupt.xml")
    if os.path.exists(src):
        data = open(src, "rb").read()
        pos  = max(len(data) // 2, 100)
        with open(dst, "wb") as fh:
            fh.write(data[:pos] + bytes([data[pos] ^ 0xFF]) + data[pos+1:])
    pt = run_no_cfg("--test", dst)
    record("T3.5 corrupted file → --test fails",
           pt.returncode != 0,
           time.monotonic() - t, f"test_rc={pt.returncode}")

    # T3.6 — compress_algo from config (no CLI flag)
    t = time.monotonic()
    p = run("--export", "users", "--output", out("t3_cfg_algo.xml"), cfg=CFG_C)
    pt = run_no_cfg("--test", out("t3_cfg_algo.xml"))
    record("T3.6 compress_algo=zstd from config (no flag)",
           p.returncode == 0 and pt.returncode == 0,
           time.monotonic() - t, f"rc={p.returncode}")


# ─── T4 MySQL → MySQL Roundtrip ──────────────────────────────────────────────

def test_T4_roundtrip():
    print(f"\n{BOLD}=== T4 MySQL → MySQL Roundtrip ==={RESET}")

    write_mysql_cfg(CFG_IMP)

    # Clean leftover import tables
    for tbl in ("rt_users", "rt_users_comp", "rt_users_proj",
                "rt_erp_entry", "rt_complex_proj"):
        mysql_exec(f"DROP TABLE IF EXISTS `{tbl}`;")

    # T4.1 — plain roundtrip
    t = time.monotonic()
    run("--export", "users", "--output", out("t4_plain.xml"))
    p = _import(out("t4_plain.xml"), "rt_users")
    rows = mysql_count("rt_users") if p.returncode == 0 else -1
    record("T4.1 plain roundtrip MySQL→MySQL: 10 rows",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rc={p.returncode} rows={rows}")

    # T4.2 — compressed roundtrip
    t = time.monotonic()
    run("--export", "users", "--compress", "--output", out("t4_comp.xml"))
    p = _import(out("t4_comp.xml"), "rt_users_comp")
    rows = mysql_count("rt_users_comp") if p.returncode == 0 else -1
    record("T4.2 compressed roundtrip: 10 rows",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rows={rows}")

    # T4.3 — re-import --strategy replace → still 10 (no dup)
    t = time.monotonic()
    p = _import(out("t4_plain.xml"), "rt_users", strategy="replace")
    rows = mysql_count("rt_users") if p.returncode == 0 else -1
    record("T4.3 re-import --strategy replace → 10 (no dup)",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rows={rows}")

    # T4.4 — re-import --strategy ignore → still 10
    t = time.monotonic()
    p = _import(out("t4_plain.xml"), "rt_users", strategy="ignore")
    rows = mysql_count("rt_users") if p.returncode == 0 else -1
    record("T4.4 re-import --strategy ignore → 10 (no dup)",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rows={rows}")

    # T4.5 — --fields projection preserved in MySQL import
    t = time.monotonic()
    run("--export", "users", "--fields", "Name,Balance",
        "--output", out("t4_proj.xml"))
    p = _import(out("t4_proj.xml"), "rt_users_proj")
    cols = mysql_columns("rt_users_proj") if p.returncode == 0 else []
    record("T4.5 --fields Name,Balance preserved in import",
           p.returncode == 0 and cols == ["Name", "Balance"],
           time.monotonic() - t, f"cols={cols}")

    # T4.6 — bracket-quoted table [ERP$Entry] roundtrip: 6 rows
    t = time.monotonic()
    run("--export", "[ERP$Entry]", "--output", out("t4_erp.xml"))
    p = _import(out("t4_erp.xml"), "rt_erp_entry")
    rows = mysql_count("rt_erp_entry") if p.returncode == 0 else -1
    record("T4.6 bracket-quoted table [ERP$Entry] roundtrip: 6 rows",
           p.returncode == 0 and rows == 6,
           time.monotonic() - t, f"rc={p.returncode} rows={rows}")

    # T4.7 — bracket-quoted --fields with spaces and $
    t = time.monotonic()
    run("--export", "[complex_fields]",
        "--fields", "[Order ID],[Customer Name],[Total Cost $]",
        "--output", out("t4_complex_proj.xml"))
    p = _import(out("t4_complex_proj.xml"), "rt_complex_proj")
    cols = mysql_columns("rt_complex_proj") if p.returncode == 0 else []
    expected = ["Order ID", "Customer Name", "Total Cost $"]
    record("T4.7 bracket-quoted --fields [Order ID],[Customer Name],[Total Cost $]",
           p.returncode == 0 and cols == expected,
           time.monotonic() - t, f"cols={cols}")

    # T4.8 — bracket-quoted --where: [Total Cost $] > 100 → 3 rows
    t = time.monotonic()
    p = run("--export", "[complex_fields]",
            "--where", "[Total Cost $] > 100",
            "--output", out("t4_complex_where.xml"))
    rows = count_rows_xml(out("t4_complex_where.xml"))
    record("T4.8 --where [Total Cost $] > 100 → 3 rows",
           p.returncode == 0 and rows == 3,
           time.monotonic() - t, f"rc={p.returncode} rows={rows}")

    # Cleanup
    for tbl in ("rt_users", "rt_users_comp", "rt_users_proj",
                "rt_erp_entry", "rt_complex_proj"):
        mysql_exec(f"DROP TABLE IF EXISTS `{tbl}`;")


# ─── T5 File Integrity ────────────────────────────────────────────────────────

def test_T5_integrity():
    print(f"\n{BOLD}=== T5 File Integrity ==={RESET}")

    run("--export", "users", "--output", out("t5_plain.xml"))

    # T5.1 — --test on uncompressed → "Total rows: 10"
    t = time.monotonic()
    p = run_no_cfg("--test", out("t5_plain.xml"))
    record("T5.1 --test uncompressed → 'Total rows: 10'",
           p.returncode == 0 and "Total rows: 10" in p.stdout,
           time.monotonic() - t, p.stdout.strip()[-60:])

    # T5.2 — --test on compressed + checksum
    t = time.monotonic()
    run("--export", "users", "--compress", "--hash",
        "--output", out("t5_hash.xml"))
    p = run_no_cfg("--test", out("t5_hash.xml"))
    record("T5.2 --test compressed+checksum → checksum OK",
           p.returncode == 0 and "checksum OK" in p.stdout,
           time.monotonic() - t, p.stdout.strip()[-60:])

    # T5.3 — --inspect shows TableName
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

    # T6.1 — WHERE matches nothing → exit 0, 0 rows
    t = time.monotonic()
    p = run("--export", "users", "--where", "Balance > 99999",
            "--output", out("t6_empty.xml"))
    rows = count_rows_xml(out("t6_empty.xml")) if p.returncode == 0 else -1
    record("T6.1 WHERE matches nothing → exit 0, 0 rows",
           p.returncode == 0 and rows == 0,
           time.monotonic() - t, f"rc={p.returncode} rows={rows}")

    # T6.2 — nonexistent table → error
    t = time.monotonic()
    p = run("--export", "nonexistent_table_xyz",
            "--output", out("t6_no_table.xml"))
    record("T6.2 export nonexistent table → error",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode}")

    # T6.3 — import nonexistent file → error
    t = time.monotonic()
    p = run("--import", "/tmp/does_not_exist_xyz.xml")
    record("T6.3 import nonexistent file → error",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode}")


# ─── T7 Compact Format (v1.3.1) ───────────────────────────────────────────────

def test_T7_compact():
    print(f"\n{BOLD}=== T7 Compact Format (v1.3.1) ==={RESET}")

    # T7.1 — --compact --fixed-fields produces 1.3.1 protocol (version attr on root)
    t = time.monotonic()
    p = run("--export", "users", "--compact", "--fixed-fields", "City",
            "--output", out("t7_compact.xml"))
    proto_ok = False
    if os.path.exists(out("t7_compact.xml")):
        try:
            root = ET.parse(out("t7_compact.xml")).getroot()
            proto_ok = root.get("version", "") == "1.3.1"
        except Exception:
            pass
    record("T7.1 --compact → protocol TDTP 1.3.1",
           p.returncode == 0 and proto_ok,
           time.monotonic() - t, f"proto_ok={proto_ok}")

    # T7.2 — compact + compress + --hash → --test OK
    t = time.monotonic()
    p = run("--export", "users", "--compact", "--fixed-fields", "City",
            "--compress", "--hash",
            "--output", out("t7_compact_hash.xml"))
    pt = run_no_cfg("--test", out("t7_compact_hash.xml"))
    record("T7.2 compact + compress + --hash → checksum OK",
           p.returncode == 0 and pt.returncode == 0 and "checksum OK" in pt.stdout,
           time.monotonic() - t, pt.stdout.strip()[:80])

    # T7.3 — --to-compact converts plain → 1.3.1
    t = time.monotonic()
    run("--export", "users", "--output", out("t7_plain_for_conv.xml"))
    p = run_no_cfg("--to-compact", out("t7_plain_for_conv.xml"),
                   "--fixed-fields", "City",
                   "--output", out("t7_converted.xml"))
    proto_ok2 = False
    if p.returncode == 0 and os.path.exists(out("t7_converted.xml")):
        try:
            root  = ET.parse(out("t7_converted.xml")).getroot()
            proto_ok2 = root.get("version", "") == "1.3.1"
        except Exception:
            pass
    record("T7.3 --to-compact converts plain file → 1.3.1",
           p.returncode == 0 and proto_ok2,
           time.monotonic() - t, f"proto_ok={proto_ok2}")

    # T7.4 — compact roundtrip into MySQL: 10 rows
    t = time.monotonic()
    mysql_exec("DROP TABLE IF EXISTS `rt_compact`;")
    write_mysql_cfg(CFG_IMP)
    run("--export", "users", "--compact", "--fixed-fields", "City",
        "--output", out("t7_rt_compact.xml"))
    p = _import(out("t7_rt_compact.xml"), "rt_compact")
    rows = mysql_count("rt_compact") if p.returncode == 0 else -1
    record("T7.4 compact roundtrip MySQL→MySQL: 10 rows",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rows={rows}")
    mysql_exec("DROP TABLE IF EXISTS `rt_compact`;")


# ─── T8 MySQL → SQLite Roundtrip ─────────────────────────────────────────────

def test_T8_mysql_sqlite():
    print(f"\n{BOLD}=== T8 MySQL → SQLite Roundtrip ==={RESET}")

    if os.path.exists(SQLITE_IMPORT_DB):
        os.remove(SQLITE_IMPORT_DB)
    write_sqlite_cfg(CFG_IMP_SQLITE, SQLITE_IMPORT_DB)

    # T8.1 — plain export MySQL → import SQLite: 10 rows
    t = time.monotonic()
    run("--export", "users", "--output", out("t8_plain.xml"))
    p = _import_sqlite(out("t8_plain.xml"), "users_copy")
    rows = sqlite_query(SQLITE_IMPORT_DB, "SELECT COUNT(*) FROM users_copy")[0][0] \
           if p.returncode == 0 else -1
    record("T8.1 MySQL → SQLite plain: 10 rows",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rc={p.returncode} rows={rows}")

    # T8.2 — compressed MySQL → SQLite: 10 rows
    t = time.monotonic()
    run("--export", "users", "--compress", "--output", out("t8_comp.xml"))
    p = _import_sqlite(out("t8_comp.xml"), "users_comp")
    rows = sqlite_query(SQLITE_IMPORT_DB, "SELECT COUNT(*) FROM users_comp")[0][0] \
           if p.returncode == 0 else -1
    record("T8.2 MySQL --compress → SQLite: 10 rows",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rows={rows}")

    # T8.3 — re-import --strategy replace → idempotent (still 10)
    t = time.monotonic()
    p = _import_sqlite(out("t8_plain.xml"), "users_copy", strategy="replace")
    rows = sqlite_query(SQLITE_IMPORT_DB, "SELECT COUNT(*) FROM users_copy")[0][0] \
           if p.returncode == 0 else -1
    record("T8.3 re-import --strategy replace → 10 (no dup)",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rows={rows}")

    # T8.4 — --fields projection preserved in SQLite
    t = time.monotonic()
    run("--export", "users", "--fields", "Name,Balance",
        "--output", out("t8_proj.xml"))
    p = _import_sqlite(out("t8_proj.xml"), "users_proj")
    cols = [c[1] for c in sqlite_query(SQLITE_IMPORT_DB,
                                       "PRAGMA table_info(users_proj)")] \
           if p.returncode == 0 else []
    record("T8.4 --fields Name,Balance preserved in SQLite import",
           p.returncode == 0 and cols == ["Name", "Balance"],
           time.monotonic() - t, f"cols={cols}")

    # T8.5 — [ERP$Entry] roundtrip MySQL → SQLite: 6 rows
    t = time.monotonic()
    run("--export", "[ERP$Entry]", "--output", out("t8_erp.xml"))
    p = _import_sqlite(out("t8_erp.xml"), "erp_entry_copy")
    rows = sqlite_query(SQLITE_IMPORT_DB, "SELECT COUNT(*) FROM erp_entry_copy")[0][0] \
           if p.returncode == 0 else -1
    record("T8.5 [ERP$Entry] MySQL → SQLite: 6 rows",
           p.returncode == 0 and rows == 6,
           time.monotonic() - t, f"rows={rows}")

    if os.path.exists(SQLITE_IMPORT_DB):
        os.remove(SQLITE_IMPORT_DB)


# ─── T9 Diff ─────────────────────────────────────────────────────────────────

def test_T9_diff():
    print(f"\n{BOLD}=== T9 Diff ==={RESET}")

    run("--export", "users", "--output", out("t9_all.xml"))
    run("--export", "users", "--where", "ID <= 7", "--output", out("t9_7.xml"))
    # users schema: ID | Name | Email | Balance | IsActive | City | CreatedAt | LastLoginAt
    # index:         0     1      2        3          4        5        6             7
    modify_xml_field(out("t9_all.xml"), out("t9_modif.xml"), "1", 3, "9999")

    # T9.1 — identical files → "Files are identical"
    t = time.monotonic()
    p = run("--diff", out("t9_all.xml"), out("t9_all.xml"))
    record("T9.1 diff identical files → 'Files are identical'",
           p.returncode == 0
           and "Files are identical" in p.stdout
           and "Added:      0" in p.stdout
           and "Removed:    0" in p.stdout,
           time.monotonic() - t, p.stdout.strip()[-80:])

    # T9.2 — A(10) vs B(7) → Removed: 3
    t = time.monotonic()
    p = run("--diff", out("t9_all.xml"), out("t9_7.xml"))
    record("T9.2 diff A(10) vs B(7) → Removed: 3, Files differ",
           p.returncode == 0
           and "Removed:    3" in p.stdout
           and "Files differ" in p.stdout,
           time.monotonic() - t, p.stdout.strip()[-80:])

    # T9.3 — A(7) vs B(10) → Added: 3
    t = time.monotonic()
    p = run("--diff", out("t9_7.xml"), out("t9_all.xml"))
    record("T9.3 diff A(7) vs B(10) → Added: 3, Files differ",
           p.returncode == 0
           and "Added:      3" in p.stdout
           and "Files differ" in p.stdout,
           time.monotonic() - t, p.stdout.strip()[-80:])

    # T9.4 — one modified Balance → Modified: 1
    t = time.monotonic()
    p = run("--diff", out("t9_all.xml"), out("t9_modif.xml"))
    record("T9.4 diff with one modified Balance → Modified: 1",
           p.returncode == 0
           and "Modified:   1" in p.stdout
           and "Files differ" in p.stdout,
           time.monotonic() - t, p.stdout.strip()[-80:])

    # T9.5 — --ignore-fields skips Balance change → identical
    t = time.monotonic()
    p = run("--ignore-fields", "Balance",
            "--diff", out("t9_all.xml"), out("t9_modif.xml"))
    record("T9.5 --ignore-fields Balance → Files are identical",
           p.returncode == 0 and "Files are identical" in p.stdout,
           time.monotonic() - t, p.stdout.strip()[-60:])

    # T9.6 — --key-fields explicit, identical files
    t = time.monotonic()
    p = run("--key-fields", "ID",
            "--diff", out("t9_all.xml"), out("t9_all.xml"))
    record("T9.6 --key-fields ID explicit → 'Files are identical'",
           p.returncode == 0 and "Files are identical" in p.stdout,
           time.monotonic() - t, f"rc={p.returncode}")

    # T9.7 — nonexistent file → error
    t = time.monotonic()
    p = run("--diff", out("t9_nonexistent_xyz.xml"), out("t9_all.xml"))
    record("T9.7 diff nonexistent file → error exit code",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode}")


# ─── T10 Merge ────────────────────────────────────────────────────────────────

def test_T10_merge():
    print(f"\n{BOLD}=== T10 Merge ==={RESET}")

    run("--export", "users", "--where", "ID <= 5",  "--output", out("t10_1to5.xml"))
    run("--export", "users", "--where", "ID > 5",   "--output", out("t10_6to10.xml"))
    run("--export", "users", "--where", "ID <= 6",  "--output", out("t10_1to6.xml"))
    run("--export", "users", "--where", "ID >= 5",  "--output", out("t10_5to10.xml"))
    run("--export", "users", "--where", "ID <= 3",  "--output", out("t10_p1.xml"))
    run("--export", "users", "--where", "ID > 3", "--where", "ID <= 7",
        "--output", out("t10_p2.xml"))
    run("--export", "users", "--where", "ID > 7",   "--output", out("t10_p3.xml"))
    run("--export", "orders", "--output", out("t10_orders.xml"))

    # T10.1 — union non-overlapping (5+5) → 10 rows, Duplicates: 0
    t = time.monotonic()
    files = f"{out('t10_1to5.xml')},{out('t10_6to10.xml')}"
    p = run("--merge", files, "--output", out("t10_union_nooverlap.xml"))
    rows = count_rows_xml(out("t10_union_nooverlap.xml")) if p.returncode == 0 else -1
    record("T10.1 union non-overlapping (5+5) → 10 rows",
           p.returncode == 0 and rows == 10
           and "Merged file saved" in p.stdout
           and "Duplicates:     0" in p.stdout,
           time.monotonic() - t, f"rows={rows}")

    # T10.2 — union overlapping (6+6, IDs 5,6 shared) → 10 rows, Duplicates: 2
    t = time.monotonic()
    files = f"{out('t10_1to6.xml')},{out('t10_5to10.xml')}"
    p = run("--merge", files, "--output", out("t10_union_overlap.xml"))
    rows = count_rows_xml(out("t10_union_overlap.xml")) if p.returncode == 0 else -1
    record("T10.2 union overlapping → 10 rows, Duplicates: 2",
           p.returncode == 0 and rows == 10 and "Duplicates:     2" in p.stdout,
           time.monotonic() - t, f"rows={rows}")

    # T10.3 — intersection → only shared IDs 5,6 = 2 rows
    t = time.monotonic()
    files = f"{out('t10_1to6.xml')},{out('t10_5to10.xml')}"
    p = run("--merge", files, "--merge-strategy", "intersection",
            "--output", out("t10_intersection.xml"))
    rows = count_rows_xml(out("t10_intersection.xml")) if p.returncode == 0 else -1
    record("T10.3 intersection → 2 rows (IDs 5,6 only)",
           p.returncode == 0 and rows == 2,
           time.monotonic() - t, f"rows={rows}")

    # T10.4 — append (no dedup) → 12 rows
    t = time.monotonic()
    files = f"{out('t10_1to6.xml')},{out('t10_5to10.xml')}"
    p = run("--merge", files, "--merge-strategy", "append",
            "--output", out("t10_append.xml"))
    rows = count_rows_xml(out("t10_append.xml")) if p.returncode == 0 else -1
    record("T10.4 append (no dedup) → 12 rows",
           p.returncode == 0 and rows == 12,
           time.monotonic() - t, f"rows={rows}")

    # T10.5 — left priority + --show-conflicts → 10 rows, 'kept_existing'
    t = time.monotonic()
    files = f"{out('t10_1to6.xml')},{out('t10_5to10.xml')}"
    p = run("--merge", files, "--merge-strategy", "left", "--show-conflicts",
            "--output", out("t10_left.xml"))
    rows = count_rows_xml(out("t10_left.xml")) if p.returncode == 0 else -1
    kept_ok = "kept_existing" in p.stdout
    record("T10.5 left priority + --show-conflicts → 10 rows, 'kept_existing'",
           p.returncode == 0 and rows == 10 and kept_ok,
           time.monotonic() - t, f"rows={rows} kept={kept_ok}")

    # T10.6 — right priority + --show-conflicts → 10 rows, 'used_new'
    t = time.monotonic()
    files = f"{out('t10_1to6.xml')},{out('t10_5to10.xml')}"
    p = run("--merge", files, "--merge-strategy", "right", "--show-conflicts",
            "--output", out("t10_right.xml"))
    rows = count_rows_xml(out("t10_right.xml")) if p.returncode == 0 else -1
    used_ok = "used_new" in p.stdout
    record("T10.6 right priority + --show-conflicts → 10 rows, 'used_new'",
           p.returncode == 0 and rows == 10 and used_ok,
           time.monotonic() - t, f"rows={rows} used={used_ok}")

    # T10.7 — 3-file union (3+4+3) → 10 rows, 'Packets merged: 3'
    t = time.monotonic()
    files = f"{out('t10_p1.xml')},{out('t10_p2.xml')},{out('t10_p3.xml')}"
    p = run("--merge", files, "--output", out("t10_3way.xml"))
    rows = count_rows_xml(out("t10_3way.xml")) if p.returncode == 0 else -1
    packets_ok = "Packets merged: 3" in p.stdout
    record("T10.7 3-file union (3+4+3) → 10 rows, 'Packets merged: 3'",
           p.returncode == 0 and rows == 10 and packets_ok,
           time.monotonic() - t, f"rows={rows} pkt={packets_ok}")

    # T10.8 — 1 file → error
    t = time.monotonic()
    p = run("--merge", out("t10_1to5.xml"), "--output", out("t10_err1.xml"))
    record("T10.8 merge with 1 file → error exit code",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode}")

    # T10.9 — incompatible table names (users vs orders) → error
    t = time.monotonic()
    files = f"{out('t10_1to5.xml')},{out('t10_orders.xml')}"
    p = run("--merge", files, "--output", out("t10_err2.xml"))
    record("T10.9 merge incompatible table names → error exit code",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode}")


# ─── Runner ───────────────────────────────────────────────────────────────────

GROUPS = [
    ("T1",  test_T1_basic_export),
    ("T2",  test_T2_filters),
    ("T3",  test_T3_compression),
    ("T4",  test_T4_roundtrip),
    ("T5",  test_T5_integrity),
    ("T6",  test_T6_edge_cases),
    ("T7",  test_T7_compact),
    ("T8",  test_T8_mysql_sqlite),
    ("T9",  test_T9_diff),
    ("T10", test_T10_merge),
]


def preflight():
    if not os.path.exists(TDTPCLI):
        print(f"{RED}ERROR: tdtpcli not found at {TDTPCLI}{RESET}")
        print(f"Build: GOPROXY=https://goproxy.io GONOSUMDB='*' "
              f"go build -o {TDTPCLI} ./cmd/tdtpcli/")
        sys.exit(1)
    if not mysql_available():
        print(f"{RED}ERROR: MySQL not available "
              f"(container={MYSQL_CONTAINER} {MYSQL_HOST}:{MYSQL_PORT}){RESET}")
        print("Start: docker compose up -d mysql")
        sys.exit(1)
    ver = subprocess.run([TDTPCLI, "--version"],
                         capture_output=True, text=True).stdout.strip()
    my_ver = mysql_exec("SELECT VERSION();")
    print(f"tdtpcli: {ver}")
    print(f"MySQL:   {my_ver}  ({MYSQL_HOST}:{MYSQL_PORT}/{MYSQL_DB}  "
          f"container={MYSQL_CONTAINER})")


def main():
    filter_group = sys.argv[1].upper() if len(sys.argv) > 1 else None

    preflight()

    OUTDIR.mkdir(parents=True, exist_ok=True)
    print(f"Setting up test database in MySQL ({MYSQL_DB})…")
    setup_db()
    write_mysql_cfg(CFG)
    write_mysql_cfg(CFG_C, compress=True)
    print(f"Output dir: {OUTDIR}/")

    overall_start = time.monotonic()

    for group_id, fn in GROUPS:
        if filter_group and not group_id.startswith(filter_group):
            continue
        fn()

    passed  = sum(1 for _, ok, _, _ in results if ok)
    failed  = sum(1 for _, ok, _, _ in results if not ok)
    total   = len(results)
    elapsed = time.monotonic() - overall_start

    print(f"\n{BOLD}{'=' * 58}{RESET}")
    print(f"{BOLD}SUMMARY{RESET}")
    print(f"  {GREEN}PASSED: {passed} / {total}{RESET}")
    if failed:
        print(f"  {RED}FAILED: {failed}{RESET}")
        print(f"\n  Failed tests:")
        for tid, ok, _, msg in results:
            if not ok:
                print(f"    {RED}✗ {tid}{RESET}  {msg}")
    print(f"  DURATION: {elapsed:.1f}s")
    print(f"{'=' * 58}")
    sys.exit(0 if failed == 0 else 1)


if __name__ == "__main__":
    main()
