#!/usr/bin/env python3
"""
TDTP CLI Integration Tests — MySQL source

Tests: basic export, TDTQL filters, compression, export/import roundtrip,
       file integrity, edge cases, compact format.
       Special focus: bracket-quoted table/field names with $ and spaces.

Prerequisites:
    docker run (or docker-compose up tdtp-mysql-test)
    python3 scripts/create_mysql_test_db.py

Usage:
    python3 tests/cli/test_mysql.py          # all groups
    python3 tests/cli/test_mysql.py T3       # single group
    TDTPCLI_BIN=/path/to/tdtpcli python3 tests/cli/test_mysql.py
"""

import os
import sys
import time
import shutil
import subprocess
import xml.etree.ElementTree as ET
from pathlib import Path

# Force UTF-8 output so → and other Unicode chars work on Windows cp1251 terminals
if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")

try:
    import pymysql
    _HAVE_PYMYSQL = True
except ImportError:
    _HAVE_PYMYSQL = False

# ─── Configuration ────────────────────────────────────────────────────────────
TDTPCLI   = os.environ.get("TDTPCLI_BIN", "/tmp/tdtpcli")
OUTDIR    = Path("/tmp/tdtp_mysql_test_out")
CFG       = "/tmp/tdtp_mysql_test.yaml"
CFG_C     = "/tmp/tdtp_mysql_compress.yaml"
CFG_IMP   = "/tmp/tdtp_mysql_import.yaml"   # same server, import via --table rename

MYSQL_HOST = os.environ.get("MYSQL_HOST", "localhost")
MYSQL_PORT = int(os.environ.get("MYSQL_PORT", "3306"))
MYSQL_USER = os.environ.get("MYSQL_USER", "tdtp_test")
MYSQL_PASS = os.environ.get("MYSQL_PASS", "tdtp_test_password")
MYSQL_DB   = os.environ.get("MYSQL_DB",   "tdtp_test_db")

# ─── ANSI colors ──────────────────────────────────────────────────────────────
GREEN  = "\033[32m"
RED    = "\033[31m"
YELLOW = "\033[33m"
BOLD   = "\033[1m"
RESET  = "\033[0m"

# ─── Global results ───────────────────────────────────────────────────────────
results: list = []


# ─── Helpers ──────────────────────────────────────────────────────────────────

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


def mysql_query(sql: str) -> list:
    """Execute SQL on MySQL, return list of stringified first-column values."""
    if not _HAVE_PYMYSQL:
        return []
    try:
        conn = pymysql.connect(
            host=MYSQL_HOST, port=MYSQL_PORT,
            user=MYSQL_USER, password=MYSQL_PASS,
            database=MYSQL_DB, charset="utf8mb4",
            autocommit=True,
        )
        with conn.cursor() as cur:
            cur.execute(sql)
            if cur.description is None:
                conn.close()
                return []
            rows = cur.fetchall()
        conn.close()
        return [str(r[0]).strip() for r in rows if r[0] is not None]
    except Exception:
        return []


def out(name: str) -> str:
    return str(OUTDIR / name)


def write_cfg(path: str, compress: bool = False, algo: str = "zstd", level: int = 3):
    with open(path, "w") as f:
        f.write(
            f"database:\n"
            f"  type: mysql\n"
            f"  host: {MYSQL_HOST}\n"
            f"  port: {MYSQL_PORT}\n"
            f"  user: {MYSQL_USER}\n"
            f"  password: {MYSQL_PASS}\n"
            f"  database: {MYSQL_DB}\n"
            f"export:\n"
            f"  compress: {str(compress).lower()}\n"
            f"  compress_algo: {algo}\n"
            f"  compress_level: {level}\n"
        )


def record(tid: str, passed: bool, elapsed: float, msg: str = ""):
    results.append((tid, passed, elapsed, msg))
    status = f"{GREEN}PASS{RESET}" if passed else f"{RED}FAIL{RESET}"
    detail = f"  ({msg})" if msg and not passed else ""
    print(f"  [{status}] {tid:<50} {elapsed:.2f}s{detail}")


def _import(file: str, table: str, strategy: str = "replace") -> subprocess.CompletedProcess:
    return subprocess.run(
        [TDTPCLI, "--config", CFG_IMP,
         "--import", file, "--table", table, "--strategy", strategy],
        capture_output=True, text=True, timeout=30,
    )


# ─── Availability check ───────────────────────────────────────────────────────

def check_mysql_available() -> bool:
    if not _HAVE_PYMYSQL:
        print(f"{RED}ERROR: pip install pymysql{RESET}")
        return False
    try:
        conn = pymysql.connect(
            host=MYSQL_HOST, port=MYSQL_PORT,
            user=MYSQL_USER, password=MYSQL_PASS,
            database=MYSQL_DB, charset="utf8mb4",
            connect_timeout=5,
        )
        conn.close()
    except Exception as e:
        print(f"{RED}Cannot connect to MySQL: {e}{RESET}")
        return False
    rows = mysql_query("SELECT COUNT(*) FROM users")
    return len(rows) > 0 and rows[0].isdigit()


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

    # T2.3 — compound AND
    t = time.monotonic()
    p = run("--export", "users",
            "--where", "Balance > 100 AND IsActive = 1",
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


# ─── T4 Export/Import Roundtrip ───────────────────────────────────────────────

def test_T4_roundtrip():
    print(f"\n{BOLD}=== T4 Export/Import Roundtrip ==={RESET}")

    write_cfg(CFG_IMP)

    # Clean leftover import tables
    for tbl in ("rt_users", "rt_users_comp", "rt_users_proj",
                "rt_erp_entry", "rt_complex_proj"):
        mysql_query(f"DROP TABLE IF EXISTS `{tbl}`")

    # T4.1 — plain roundtrip
    t = time.monotonic()
    run("--export", "users", "--output", out("t4_plain.xml"))
    p = _import(out("t4_plain.xml"), "rt_users")
    rows = mysql_query("SELECT COUNT(*) FROM `rt_users`")
    rows = int(rows[0]) if p.returncode == 0 and rows else -1
    record("T4.1 plain roundtrip: 10 rows imported",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rc={p.returncode} rows={rows}")

    # T4.2 — compressed roundtrip
    t = time.monotonic()
    run("--export", "users", "--compress", "--output", out("t4_comp.xml"))
    p = _import(out("t4_comp.xml"), "rt_users_comp")
    rows = mysql_query("SELECT COUNT(*) FROM `rt_users_comp`")
    rows = int(rows[0]) if p.returncode == 0 and rows else -1
    record("T4.2 compressed roundtrip: 10 rows",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rows={rows}")

    # T4.3 — re-import --strategy replace (no duplicates)
    t = time.monotonic()
    p = _import(out("t4_plain.xml"), "rt_users", strategy="replace")
    rows = mysql_query("SELECT COUNT(*) FROM `rt_users`")
    rows = int(rows[0]) if p.returncode == 0 and rows else -1
    record("T4.3 re-import --strategy replace → 10 (no dup)",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rows={rows}")

    # T4.4 — re-import --strategy ignore (no duplicates)
    t = time.monotonic()
    p = _import(out("t4_plain.xml"), "rt_users", strategy="ignore")
    rows = mysql_query("SELECT COUNT(*) FROM `rt_users`")
    rows = int(rows[0]) if p.returncode == 0 and rows else -1
    record("T4.4 re-import --strategy ignore → 10 (no dup)",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rows={rows}")

    # T4.5 — --fields projection preserved in import
    t = time.monotonic()
    run("--export", "users", "--fields", "Name,Balance",
        "--output", out("t4_proj.xml"))
    p = _import(out("t4_proj.xml"), "rt_users_proj")
    cols = mysql_query(
        "SELECT COLUMN_NAME FROM INFORMATION_SCHEMA.COLUMNS "
        f"WHERE TABLE_SCHEMA='{MYSQL_DB}' AND TABLE_NAME='rt_users_proj' "
        "ORDER BY ORDINAL_POSITION"
    ) if p.returncode == 0 else []
    record("T4.5 --fields Name,Balance preserved in import",
           p.returncode == 0 and cols == ["Name", "Balance"],
           time.monotonic() - t, f"cols={cols}")

    # T4.6 — bracket-quoted table name with $ (ERP$Entry) roundtrip
    t = time.monotonic()
    run("--export", "[ERP$Entry]", "--output", out("t4_erp.xml"))
    p = _import(out("t4_erp.xml"), "rt_erp_entry")
    rows = mysql_query("SELECT COUNT(*) FROM `rt_erp_entry`")
    rows = int(rows[0]) if p.returncode == 0 and rows else -1
    record("T4.6 bracket-quoted table [ERP$Entry] roundtrip: 6 rows",
           p.returncode == 0 and rows == 6,
           time.monotonic() - t, f"rc={p.returncode} rows={rows}")

    # T4.7 — bracket-quoted --fields with spaces and $
    t = time.monotonic()
    run("--export", "[complex_fields]",
        "--fields", "[Order ID],[Customer Name],[Total Cost $]",
        "--output", out("t4_complex_proj.xml"))
    p = _import(out("t4_complex_proj.xml"), "rt_complex_proj")
    cols = mysql_query(
        "SELECT COLUMN_NAME FROM INFORMATION_SCHEMA.COLUMNS "
        f"WHERE TABLE_SCHEMA='{MYSQL_DB}' AND TABLE_NAME='rt_complex_proj' "
        "ORDER BY ORDINAL_POSITION"
    ) if p.returncode == 0 else []
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
        mysql_query(f"DROP TABLE IF EXISTS `{tbl}`")


# ─── T5 File Integrity ────────────────────────────────────────────────────────

def test_T5_integrity():
    print(f"\n{BOLD}=== T5 File Integrity ==={RESET}")

    run("--export", "users", "--output", out("t5_plain.xml"))

    # T5.1 — --test on uncompressed file
    t = time.monotonic()
    p = run_no_cfg("--test", out("t5_plain.xml"))
    record("T5.1 --test uncompressed → exit 0",
           p.returncode == 0,
           time.monotonic() - t, p.stdout.strip()[:80])

    # T5.2 — --test on compressed+hash file
    t = time.monotonic()
    run("--export", "users", "--compress", "--hash",
        "--output", out("t5_hash.xml"))
    p = run_no_cfg("--test", out("t5_hash.xml"))
    record("T5.2 --test compressed+checksum → checksum OK",
           p.returncode == 0 and "checksum OK" in p.stdout,
           time.monotonic() - t, p.stdout.strip()[:80])

    # T5.3 — --inspect-table
    t = time.monotonic()
    p = run("--inspect-table", "users")
    record("T5.3 --inspect-table users → YAML with columns",
           p.returncode == 0 and "columns:" in p.stdout and "total_rows:" in p.stdout,
           time.monotonic() - t, p.stdout.strip()[:80])


# ─── T6 Edge Cases ────────────────────────────────────────────────────────────

def test_T6_edge_cases():
    print(f"\n{BOLD}=== T6 Edge Cases ==={RESET}")

    # T6.1 — WHERE matches nothing → exit 0, 0 rows
    t = time.monotonic()
    p = run("--export", "users", "--where", "Balance < 0",
            "--output", out("t6_empty.xml"))
    rows = count_rows_xml(out("t6_empty.xml"))
    record("T6.1 WHERE matches nothing → exit 0, 0 rows",
           p.returncode == 0 and rows == 0,
           time.monotonic() - t, f"rc={p.returncode} rows={rows}")

    # T6.2 — nonexistent table → error
    t = time.monotonic()
    p = run("--export", "no_such_table_xyz",
            "--output", out("t6_no_table.xml"))
    record("T6.2 export nonexistent table → error",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode}")

    # T6.3 — import nonexistent file → error
    t = time.monotonic()
    p = _import("/tmp/nonexistent_xyz.xml", "dummy")
    record("T6.3 import nonexistent file → error",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode}")

    # T6.4 — --inspect-table with bracket-quoted $ name
    t = time.monotonic()
    p = run("--inspect-table", "[ERP$Entry]")
    record("T6.4 --inspect-table [ERP$Entry] → 6 rows in YAML",
           p.returncode == 0 and "6" in p.stdout and "No_" in p.stdout,
           time.monotonic() - t, p.stdout.strip()[:80])


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

    # T7.4 — compact roundtrip: export → import; row count preserved
    t = time.monotonic()
    mysql_query("DROP TABLE IF EXISTS `rt_compact`")
    write_cfg(CFG_IMP)
    run("--export", "users", "--compact", "--fixed-fields", "City",
        "--output", out("t7_rt_compact.xml"))
    p = _import(out("t7_rt_compact.xml"), "rt_compact")
    rows = mysql_query("SELECT COUNT(*) FROM `rt_compact`")
    rows = int(rows[0]) if p.returncode == 0 and rows else -1
    record("T7.4 compact roundtrip: 10 rows imported",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rows={rows}")
    mysql_query("DROP TABLE IF EXISTS `rt_compact`")


# ─── Main ─────────────────────────────────────────────────────────────────────

GROUPS = {
    "T1": test_T1_basic_export,
    "T2": test_T2_filters,
    "T3": test_T3_compression,
    "T4": test_T4_roundtrip,
    "T5": test_T5_integrity,
    "T6": test_T6_edge_cases,
    "T7": test_T7_compact,
}


def preflight():
    if not os.path.exists(TDTPCLI):
        print(f"{RED}ERROR: tdtpcli not found at {TDTPCLI}{RESET}")
        print(f"  Build: GOPROXY=https://goproxy.io GONOSUMDB='*' "
              f"go build -tags nokafka -o {TDTPCLI} ./cmd/tdtpcli/")
        sys.exit(1)

    print(f"tdtpcli: ", end="")
    vp = subprocess.run([TDTPCLI, "--version"], capture_output=True, text=True)
    print(vp.stdout.strip() or vp.stderr.strip())

    print(f"Checking MySQL at {MYSQL_HOST}:{MYSQL_PORT}... ", end="", flush=True)
    if not check_mysql_available():
        print(f"{RED}UNAVAILABLE{RESET}")
        print("  Run: python3 scripts/create_mysql_test_db.py")
        sys.exit(1)
    print(f"{GREEN}OK{RESET}")

    rows = mysql_query("SELECT COUNT(*) FROM users")
    orders = mysql_query("SELECT COUNT(*) FROM orders")
    print(f"Checking test data... {GREEN}OK (users={rows[0] if rows else '?'}, "
          f"orders={orders[0] if orders else '?'}){RESET}")

    OUTDIR.mkdir(parents=True, exist_ok=True)
    print(f"Config: {CFG}")
    print(f"Output: {OUTDIR}/")
    write_cfg(CFG)
    write_cfg(CFG_C, compress=True)


def main():
    filter_groups = [a.upper() for a in sys.argv[1:] if re.match(r"T\d+", a, re.I)] \
        if len(sys.argv) > 1 else []

    preflight()

    to_run = [(k, v) for k, v in GROUPS.items()
              if not filter_groups or k in filter_groups]

    for _, fn in to_run:
        fn()

    total  = len(results)
    passed = sum(1 for _, ok, _, _ in results if ok)
    failed = total - passed

    print(f"\n{BOLD}{'=' * 60}{RESET}")
    print(f"{BOLD}SUMMARY{RESET}")
    if failed == 0:
        print(f"  {GREEN}PASSED: {passed} / {total}{RESET}")
    else:
        print(f"  {GREEN}PASSED: {passed} / {total}{RESET}")
        print(f"  {RED}FAILED: {failed}{RESET}")
        print(f"\n  Failed tests:")
        for tid, ok, _, msg in results:
            if not ok:
                print(f"    {RED}✗ {tid}{RESET}  {msg}")
    duration = sum(e for _, _, e, _ in results)
    print(f"  DURATION: {duration:.1f}s")
    print("=" * 60)
    sys.exit(0 if failed == 0 else 1)


import re  # noqa: E402 (used in main)

if __name__ == "__main__":
    main()
