#!/usr/bin/env python3
"""
TDTP CLI Integration Tests — PostgreSQL source

Tests: DB availability check, basic export, TDTQL filters, compression,
       export/import roundtrip, PG-specific types (UUID, JSONB, TEXT[]).

Prerequisites:
    pg_ctlcluster 16 main start   # or any PG on localhost:5432
    python3 scripts/create_postgres_test_db.py

Usage:
    python3 tests/cli/test_postgres.py          # all groups
    python3 tests/cli/test_postgres.py T3       # single group
    TDTPCLI_BIN=/path/to/tdtpcli python3 tests/cli/test_postgres.py
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
    import psycopg2
    _HAVE_PSYCOPG2 = True
except ImportError:
    _HAVE_PSYCOPG2 = False

# ─── Configuration ────────────────────────────────────────────────────────────
TDTPCLI  = os.environ.get("TDTPCLI_BIN", "/tmp/tdtpcli")
OUTDIR   = Path("/tmp/tdtp_pg_test_out")
CFG      = "/tmp/tdtp_pg_test.yaml"       # plain (no compression)
CFG_C    = "/tmp/tdtp_pg_compress.yaml"   # compression from config
CFG_IMP  = "/tmp/tdtp_pg_import.yaml"     # import-target config

PG_HOST  = os.environ.get("PG_HOST",     "localhost")
PG_PORT  = int(os.environ.get("PG_PORT", "5432"))
PG_USER  = os.environ.get("PG_USER",     "tdtp_user")
PG_PASS  = os.environ.get("PG_PASS",     "tdtp_dev_pass_2025")
PG_DB    = os.environ.get("PG_DB",       "tdtp_test")

# Known row counts from create_postgres_test_db.py
USERS_COUNT    = 100
ORDERS_COUNT   = 200
PRODUCTS_COUNT = 50

# Computed from actual data (verified via psql before writing tests)
ACTIVE_USERS          = 71    # WHERE is_active = true
ORDERS_AMOUNT_GT_1000 = 132   # WHERE total_amount > 1000
USERS_BALANCE_GT_5000 = 49    # WHERE balance > 5000

# ─── ANSI colors ──────────────────────────────────────────────────────────────
GREEN  = "\033[32m"
RED    = "\033[31m"
YELLOW = "\033[33m"
BOLD   = "\033[1m"
RESET  = "\033[0m"

results: list = []   # (tid, passed, elapsed, msg)


# ─── Helpers ──────────────────────────────────────────────────────────────────

def run(*args, cfg=None, timeout=60) -> subprocess.CompletedProcess:
    cmd = [TDTPCLI, "--config", cfg or CFG] + list(args)
    return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)


def run_no_cfg(*args, timeout=30) -> subprocess.CompletedProcess:
    return subprocess.run([TDTPCLI] + list(args),
                         capture_output=True, text=True, timeout=timeout)


def count_rows_xml(path: str) -> int:
    """Count rows in TDTP XML (handles compressed and uncompressed)."""
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


def count_rows_multipart(prefix: str) -> int:
    """Sum rows across all _part_N_of_M files matching prefix."""
    total = 0
    for f in sorted(OUTDIR.glob(f"{prefix}_part_*.xml")):
        n = count_rows_xml(str(f))
        if n < 0:
            return -1
        total += n
    if total == 0:
        # maybe single file
        single = str(OUTDIR / f"{prefix}.xml")
        return count_rows_xml(single)
    return total


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


def out(name: str) -> str:
    return str(OUTDIR / name)


def pg_query(sql: str) -> list:
    """Run a SQL query against PostgreSQL and return stripped string values.

    Uses psycopg2 when available (works on Windows without psql in PATH),
    falls back to psql CLI otherwise.
    """
    if _HAVE_PSYCOPG2:
        try:
            conn = psycopg2.connect(
                host=PG_HOST, port=PG_PORT, user=PG_USER,
                password=PG_PASS, dbname=PG_DB,
            )
            conn.autocommit = True
            with conn.cursor() as cur:
                cur.execute(sql)
                if cur.description is None:
                    conn.close()
                    return []
                rows = cur.fetchall()
            conn.close()
            return [str(row[0]).strip() for row in rows if row[0] is not None]
        except Exception:
            return []
    # fallback: psql CLI
    env = os.environ.copy()
    env["PGPASSWORD"] = PG_PASS
    p = subprocess.run(
        ["psql", "-h", PG_HOST, "-p", str(PG_PORT),
         "-U", PG_USER, "-d", PG_DB,
         "-t", "-A", "-c", sql],
        capture_output=True, text=True, env=env, timeout=10,
    )
    return [line.strip() for line in p.stdout.splitlines() if line.strip()]


def write_cfg(path: str, db: str = PG_DB,
              compress: bool = False, algo: str = "zstd", level: int = 3):
    with open(path, "w") as f:
        f.write(f"database:\n"
                f"  type: postgres\n"
                f"  host: {PG_HOST}\n"
                f"  port: {PG_PORT}\n"
                f"  user: {PG_USER}\n"
                f"  password: {PG_PASS}\n"
                f"  database: {db}\n"
                f"  sslmode: disable\n"
                f"export:\n"
                f"  compress: {str(compress).lower()}\n"
                f"  compress_algo: {algo}\n"
                f"  compress_level: {level}\n")


def record(tid: str, passed: bool, elapsed: float, msg: str = ""):
    results.append((tid, passed, elapsed, msg))
    status = f"{GREEN}PASS{RESET}" if passed else f"{RED}FAIL{RESET}"
    detail = f"  ({msg})" if msg and not passed else ""
    print(f"  [{status}] {tid:<50} {elapsed:.2f}s{detail}")


# ─── Availability check ───────────────────────────────────────────────────────

def check_pg_available() -> bool:
    """Return True if PostgreSQL is reachable and tdtp_test DB is ready."""
    if _HAVE_PSYCOPG2:
        try:
            conn = psycopg2.connect(
                host=PG_HOST, port=PG_PORT, user=PG_USER,
                password=PG_PASS, dbname=PG_DB, connect_timeout=5,
            )
            conn.close()
        except Exception:
            return False
    else:
        p = subprocess.run(
            ["pg_isready", "-h", PG_HOST, "-p", str(PG_PORT)],
            capture_output=True, text=True, timeout=5,
        )
        if p.returncode != 0:
            return False
    # Verify the user and DB actually exist
    rows = pg_query("SELECT COUNT(*) FROM users")
    return len(rows) > 0 and rows[0].isdigit()


def check_tables_populated() -> bool:
    """Verify key tables have expected row counts."""
    try:
        u = pg_query("SELECT COUNT(*) FROM users")[0]
        o = pg_query("SELECT COUNT(*) FROM orders")[0]
        return int(u) == USERS_COUNT and int(o) == ORDERS_COUNT
    except Exception:
        return False


# ─── T1 Basic Export ──────────────────────────────────────────────────────────

def test_T1_basic_export():
    print(f"\n{BOLD}=== T1 Basic Export ==={RESET}")

    # T1.1 — export all users (100 rows, possibly multi-part)
    t = time.monotonic()
    p = run("--export", "users", "--output", out("t1_users.xml"))
    rows = count_rows_multipart("t1_users")
    if rows < 0:
        rows = count_rows_xml(out("t1_users.xml"))
    record("T1.1 export all users (100 rows)",
           p.returncode == 0 and rows == USERS_COUNT,
           time.monotonic() - t, f"rc={p.returncode} rows={rows}")

    # T1.2 — export products (50 rows)
    t = time.monotonic()
    p = run("--export", "products", "--output", out("t1_products.xml"))
    rows = count_rows_xml(out("t1_products.xml"))
    record("T1.2 export all products (50 rows)",
           p.returncode == 0 and rows == PRODUCTS_COUNT,
           time.monotonic() - t, f"rows={rows}")

    # T1.3 — --fields projection: username + email only
    t = time.monotonic()
    p = run("--export", "users", "--fields", "username,email",
            "--output", out("t1_fields.xml"))
    fields = get_schema_fields(out("t1_fields.xml"))
    record("T1.3 --fields username,email → 2 columns",
           p.returncode == 0 and fields == ["username", "email"],
           time.monotonic() - t, f"fields={fields}")

    # T1.4 — --list shows key tables
    t = time.monotonic()
    p = run("--list")
    tables_ok = all(tbl in p.stdout for tbl in ("users", "orders", "products"))
    record("T1.4 --list shows users, orders, products",
           p.returncode == 0 and tables_ok,
           time.monotonic() - t, p.stdout.strip()[:80])


# ─── T2 TDTQL Filters ─────────────────────────────────────────────────────────

def test_T2_filters():
    print(f"\n{BOLD}=== T2 TDTQL Filters ==={RESET}")

    # T2.1 — WHERE is_active = true → 76 rows
    t = time.monotonic()
    p = run("--export", "users", "--where", "is_active = true",
            "--output", out("t2_active.xml"))
    rows = count_rows_xml(out("t2_active.xml"))
    record("T2.1 WHERE is_active = true → 76 rows",
           p.returncode == 0 and rows == ACTIVE_USERS,
           time.monotonic() - t, f"rows={rows}")

    # T2.2 — WHERE balance > 5000 → 49 rows
    t = time.monotonic()
    p = run("--export", "users", "--where", "balance > 5000",
            "--output", out("t2_balance.xml"))
    rows = count_rows_xml(out("t2_balance.xml"))
    record("T2.2 WHERE balance > 5000 → 49 rows",
           p.returncode == 0 and rows == USERS_BALANCE_GT_5000,
           time.monotonic() - t, f"rows={rows}")

    # T2.3 — multiple --where (AND): active + balance > 5000
    t = time.monotonic()
    p = run("--export", "users",
            "--where", "is_active = true",
            "--where", "balance > 5000",
            "--output", out("t2_and.xml"))
    rows = count_rows_xml(out("t2_and.xml"))
    # cross-check via psql
    expected = int(pg_query("SELECT COUNT(*) FROM users WHERE is_active=true AND balance>5000")[0])
    record("T2.3 WHERE is_active=true AND balance>5000",
           p.returncode == 0 and rows == expected,
           time.monotonic() - t, f"rows={rows} expected={expected}")

    # T2.4 — IN operator on orders status
    t = time.monotonic()
    p = run("--export", "orders",
            "--where", "status IN ('pending','processing')",
            "--output", out("t2_in.xml"))
    rows = count_rows_xml(out("t2_in.xml"))
    expected = int(pg_query("SELECT COUNT(*) FROM orders WHERE status IN ('pending','processing')")[0])
    record("T2.4 WHERE status IN ('pending','processing')",
           p.returncode == 0 and rows == expected,
           time.monotonic() - t, f"rows={rows} expected={expected}")

    # T2.5 — ORDER BY + LIMIT
    t = time.monotonic()
    p = run("--export", "users",
            "--order-by", "balance DESC", "--limit", "10",
            "--output", out("t2_top10.xml"))
    rows = count_rows_xml(out("t2_top10.xml"))
    record("T2.5 ORDER BY balance DESC LIMIT 10 → 10 rows",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rows={rows}")

    # T2.6 — LIMIT + OFFSET
    t = time.monotonic()
    p = run("--export", "users", "--limit", "20", "--offset", "10",
            "--output", out("t2_page.xml"))
    rows = count_rows_xml(out("t2_page.xml"))
    record("T2.6 LIMIT 20 OFFSET 10 → 20 rows",
           p.returncode == 0 and rows == 20,
           time.monotonic() - t, f"rows={rows}")

    # T2.7 — tail mode: last 5 rows
    t = time.monotonic()
    p = run("--export", "orders", "--limit", "-5",
            "--output", out("t2_tail.xml"))
    rows = count_rows_xml(out("t2_tail.xml"))
    record("T2.7 LIMIT -5 (last 5 rows, tail mode)",
           p.returncode == 0 and rows == 5,
           time.monotonic() - t, f"rows={rows}")


# ─── T3 Compression ───────────────────────────────────────────────────────────

def test_T3_compression():
    print(f"\n{BOLD}=== T3 Compression ==={RESET}")

    # Baseline: uncompressed
    run("--export", "users", "--output", out("t3_base.xml"))
    # For multi-part, get total size of all parts
    base_files = list(OUTDIR.glob("t3_base_part_*.xml"))
    if not base_files:
        base_files = [OUTDIR / "t3_base.xml"]
    base_size = sum(f.stat().st_size for f in base_files if f.exists())

    # T3.1 — zstd level 3, verify smaller + --test OK
    t = time.monotonic()
    p = run("--export", "users", "--compress", "--compress-level", "3",
            "--output", out("t3_z3.xml"))
    z3_files = list(OUTDIR.glob("t3_z3_part_*.xml")) or [OUTDIR / "t3_z3.xml"]
    z3_size = sum(f.stat().st_size for f in z3_files if f.exists())
    # test the first part (or single file)
    test_file = str(z3_files[0]) if z3_files else out("t3_z3.xml")
    pt = run_no_cfg("--test", test_file)
    record("T3.1 zstd level 3 (smaller + --test OK)",
           p.returncode == 0 and z3_size < base_size and pt.returncode == 0,
           time.monotonic() - t, f"z3={z3_size} base={base_size} test_rc={pt.returncode}")

    # T3.2 — zstd level 19
    t = time.monotonic()
    p = run("--export", "users", "--compress", "--compress-level", "19",
            "--output", out("t3_z19.xml"))
    z19_files = list(OUTDIR.glob("t3_z19_part_*.xml")) or [OUTDIR / "t3_z19.xml"]
    z19_size = sum(f.stat().st_size for f in z19_files if f.exists())
    record("T3.2 zstd level 19 smaller than uncompressed",
           p.returncode == 0 and z19_size < base_size,
           time.monotonic() - t, f"z19={z19_size} base={base_size}")

    # T3.3 — kanzi level 6
    t = time.monotonic()
    p = run("--export", "users", "--compress",
            "--compress-algo", "kanzi", "--compress-level", "6",
            "--output", out("t3_k6.xml"))
    k6_files = list(OUTDIR.glob("t3_k6_part_*.xml")) or [OUTDIR / "t3_k6.xml"]
    k6_test_file = str(k6_files[0]) if k6_files else out("t3_k6.xml")
    pt = run_no_cfg("--test", k6_test_file)
    record("T3.3 kanzi level 6 + --test OK",
           p.returncode == 0 and pt.returncode == 0,
           time.monotonic() - t, f"test_rc={pt.returncode}")

    # T3.4 — zstd + --hash
    t = time.monotonic()
    p = run("--export", "users", "--compress", "--hash",
            "--output", out("t3_hash.xml"))
    hash_files = list(OUTDIR.glob("t3_hash_part_*.xml")) or [OUTDIR / "t3_hash.xml"]
    pt = run_no_cfg("--test", str(hash_files[0]) if hash_files else out("t3_hash.xml"))
    checksum_ok = "checksum OK" in pt.stdout
    record("T3.4 --compress --hash → checksum OK in --test",
           p.returncode == 0 and checksum_ok,
           time.monotonic() - t, pt.stdout.strip()[-60:])

    # T3.5 — corrupt 1 byte → --test must fail
    t = time.monotonic()
    src = hash_files[0] if hash_files else OUTDIR / "t3_hash.xml"
    corrupt = out("t3_corrupt.xml")
    shutil.copy(str(src), corrupt)
    fsize = os.path.getsize(corrupt)
    with open(corrupt, "r+b") as f:
        mid = fsize // 2
        f.seek(mid)
        b = f.read(1)
        f.seek(mid)
        f.write(bytes([b[0] ^ 0x55]))
    pt = run_no_cfg("--test", corrupt)
    record("T3.5 corrupted file → --test fails",
           pt.returncode != 0,
           time.monotonic() - t, f"rc={pt.returncode}")

    # T3.6 — compress_algo from config (no --compress flag on CLI)
    write_cfg(CFG_C, compress=True, algo="zstd", level=3)
    t = time.monotonic()
    p = run("--export", "users", "--output", out("t3_cfg.xml"), cfg=CFG_C)
    cfg_files = list(OUTDIR.glob("t3_cfg_part_*.xml")) or [OUTDIR / "t3_cfg.xml"]
    cfg_size = sum(f.stat().st_size for f in cfg_files if f.exists())
    record("T3.6 compress_algo=zstd from config (no flag)",
           p.returncode == 0 and cfg_size < base_size,
           time.monotonic() - t, f"size={cfg_size} base={base_size}")


# ─── T4 Export/Import Roundtrip ───────────────────────────────────────────────

IMPORT_DB = "tdtp_import_test"


def _import(file: str, table: str, strategy: str = "replace") -> subprocess.CompletedProcess:
    return subprocess.run(
        [TDTPCLI, "--config", CFG_IMP,
         "--import", file, "--table", table, "--strategy", strategy],
        capture_output=True, text=True, timeout=60,
    )


def test_T4_roundtrip():
    print(f"\n{BOLD}=== T4 Export/Import Roundtrip ==={RESET}")

    # Create import DB (re-use same PG server, different table names via --table)
    write_cfg(CFG_IMP)   # same server, same DB — use --table to avoid collision

    # Clean up any leftover import tables
    for tbl in ("rt_users", "rt_users_comp", "rt_users_proj"):
        pg_query(f"DROP TABLE IF EXISTS {tbl} CASCADE")

    # T4.1 — plain roundtrip: row count
    t = time.monotonic()
    run("--export", "users", "--output", out("t4_plain.xml"))
    # For multi-part, find first part
    parts = sorted(OUTDIR.glob("t4_plain_part_*.xml"))
    first = str(parts[0]) if parts else out("t4_plain.xml")
    p = _import(first, "rt_users")
    rows_sql = pg_query("SELECT COUNT(*) FROM rt_users")
    rows = int(rows_sql[0]) if p.returncode == 0 and rows_sql else -1
    record("T4.1 plain roundtrip: 100 rows imported",
           p.returncode == 0 and rows == USERS_COUNT,
           time.monotonic() - t, f"rc={p.returncode} rows={rows}")

    # T4.2 — compressed roundtrip
    t = time.monotonic()
    run("--export", "users", "--compress", "--output", out("t4_comp.xml"))
    parts_c = sorted(OUTDIR.glob("t4_comp_part_*.xml"))
    first_c = str(parts_c[0]) if parts_c else out("t4_comp.xml")
    p2 = _import(first_c, "rt_users_comp")
    rows2_sql = pg_query("SELECT COUNT(*) FROM rt_users_comp")
    rows2 = int(rows2_sql[0]) if p2.returncode == 0 and rows2_sql else -1
    record("T4.2 compressed roundtrip: 100 rows",
           p2.returncode == 0 and rows2 == USERS_COUNT,
           time.monotonic() - t, f"rows={rows2}")

    # T4.3 — re-import with --strategy replace (no duplicates)
    t = time.monotonic()
    p3 = _import(first, "rt_users", strategy="replace")
    rows3_sql = pg_query("SELECT COUNT(*) FROM rt_users")
    rows3 = int(rows3_sql[0]) if p3.returncode == 0 and rows3_sql else -1
    record("T4.3 re-import --strategy replace → 100 (no dup)",
           p3.returncode == 0 and rows3 == USERS_COUNT,
           time.monotonic() - t, f"rows={rows3}")

    # T4.4 — re-import with --strategy ignore (no duplicates)
    t = time.monotonic()
    p4 = _import(first, "rt_users", strategy="ignore")
    rows4_sql = pg_query("SELECT COUNT(*) FROM rt_users")
    rows4 = int(rows4_sql[0]) if p4.returncode == 0 and rows4_sql else -1
    record("T4.4 re-import --strategy ignore → 100 (no dup)",
           p4.returncode == 0 and rows4 == USERS_COUNT,
           time.monotonic() - t, f"rows={rows4}")

    # T4.5 — --fields projection preserved in import
    t = time.monotonic()
    run("--export", "users", "--fields", "username,balance",
        "--output", out("t4_proj.xml"))
    proj_file = out("t4_proj.xml")
    p5 = _import(proj_file, "rt_users_proj")
    col_rows = pg_query(
        "SELECT column_name FROM information_schema.columns "
        "WHERE table_name='rt_users_proj' ORDER BY ordinal_position"
    ) if p5.returncode == 0 else []
    record("T4.5 --fields username,balance preserved in import",
           p5.returncode == 0 and col_rows == ["username", "balance"],
           time.monotonic() - t, f"cols={col_rows}")

    # T4.6 — bracket-quoted table name with $ (ERP$Entry) export → import
    t = time.monotonic()
    pg_query('DROP TABLE IF EXISTS "rt_erp_entry" CASCADE')
    run("--export", "[ERP$Entry]", "--output", out("t4_erp.xml"))
    p6 = _import(out("t4_erp.xml"), "rt_erp_entry")
    rows6 = pg_query("SELECT COUNT(*) FROM rt_erp_entry")
    rows6 = int(rows6[0]) if p6.returncode == 0 and rows6 else -1
    record("T4.6 bracket-quoted table [ERP$Entry] roundtrip: 6 rows",
           p6.returncode == 0 and rows6 == 6,
           time.monotonic() - t, f"rc={p6.returncode} rows={rows6}")

    # T4.7 — bracket-quoted --fields with spaces and $ from complex_fields
    t = time.monotonic()
    pg_query('DROP TABLE IF EXISTS "rt_complex_proj" CASCADE')
    run("--export", "[complex_fields]",
        "--fields", "[Order ID],[Customer Name],[Total Cost $]",
        "--output", out("t4_complex_proj.xml"))
    p7 = _import(out("t4_complex_proj.xml"), "rt_complex_proj")
    cols7 = pg_query(
        "SELECT column_name FROM information_schema.columns "
        "WHERE table_name='rt_complex_proj' ORDER BY ordinal_position"
    ) if p7.returncode == 0 else []
    expected7 = ["Order ID", "Customer Name", "Total Cost $"]
    record("T4.7 bracket-quoted --fields [Order ID],[Customer Name],[Total Cost $]",
           p7.returncode == 0 and cols7 == expected7,
           time.monotonic() - t, f"cols={cols7}")

    # T4.8 — bracket-quoted --where filter on field with $ (3 rows where Total Cost $ > 100)
    t = time.monotonic()
    p8 = run("--export", "[complex_fields]",
             "--where", "[Total Cost $] > 100",
             "--output", out("t4_complex_where.xml"))
    rows8 = count_rows_xml(out("t4_complex_where.xml"))
    record("T4.8 --where [Total Cost $] > 100 → 3 rows",
           p8.returncode == 0 and rows8 == 3,
           time.monotonic() - t, f"rc={p8.returncode} rows={rows8}")

    # Cleanup import tables
    for tbl in ("rt_users", "rt_users_comp", "rt_users_proj",
                "rt_erp_entry", "rt_complex_proj"):
        pg_query(f'DROP TABLE IF EXISTS "{tbl}" CASCADE')


# ─── T5 File Integrity ────────────────────────────────────────────────────────

def test_T5_integrity():
    print(f"\n{BOLD}=== T5 File Integrity ==={RESET}")

    # T5.1 — --test on uncompressed (single or first part)
    run("--export", "users", "--output", out("t5_plain.xml"))
    parts = sorted(OUTDIR.glob("t5_plain_part_*.xml"))
    test_file = str(parts[0]) if parts else out("t5_plain.xml")
    t = time.monotonic()
    p = run_no_cfg("--test", test_file)
    record("T5.1 --test uncompressed → exit 0",
           p.returncode == 0,
           time.monotonic() - t, p.stdout.strip()[-60:])

    # T5.2 — --test on compressed + checksum (multi-part: pass first part)
    run("--export", "users", "--compress", "--hash",
        "--output", out("t5_hash.xml"))
    hash_parts = sorted(OUTDIR.glob("t5_hash_part_*.xml"))
    hash_file = str(hash_parts[0]) if hash_parts else out("t5_hash.xml")
    t = time.monotonic()
    p = run_no_cfg("--test", hash_file)
    record("T5.2 --test compressed+checksum → checksum OK",
           p.returncode == 0 and "checksum OK" in p.stdout,
           time.monotonic() - t, p.stdout.strip()[-60:])

    # T5.3 — --inspect shows metadata
    run("--export", "users", "--output", out("t5_inspect.xml"))
    insp_parts = sorted(OUTDIR.glob("t5_inspect_part_*.xml"))
    insp_file = str(insp_parts[0]) if insp_parts else out("t5_inspect.xml")
    t = time.monotonic()
    p = run_no_cfg("--inspect", insp_file)
    has_meta = "table:" in p.stdout or "TableName" in p.stdout
    record("T5.3 --inspect shows table metadata",
           p.returncode == 0 and has_meta,
           time.monotonic() - t, p.stdout.strip()[:80])


# ─── T6 Edge Cases ────────────────────────────────────────────────────────────

def test_T6_edge_cases():
    print(f"\n{BOLD}=== T6 Edge Cases ==={RESET}")

    # T6.1 — WHERE that matches nothing → exit 0, 0 rows
    t = time.monotonic()
    p = run("--export", "users", "--where", "balance > 999999",
            "--output", out("t6_empty.xml"))
    rows = count_rows_xml(out("t6_empty.xml")) if p.returncode == 0 else -1
    record("T6.1 WHERE matches nothing → exit 0, 0 rows",
           p.returncode == 0 and rows == 0,
           time.monotonic() - t, f"rc={p.returncode} rows={rows}")

    # T6.2 — export non-existent table → error
    t = time.monotonic()
    p = run("--export", "nonexistent_table_xyz",
            "--output", out("t6_no_table.xml"))
    record("T6.2 export nonexistent table → error",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode}")

    # T6.3 — import missing file → error
    t = time.monotonic()
    p = run("--import", "/tmp/does_not_exist_xyz.xml")
    record("T6.3 import nonexistent file → error",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode}")


# ─── T7 Compact Format (v1.3.1) ──────────────────────────────────────────────

def test_T7_compact():
    print(f"\n{BOLD}=== T7 Compact Format (v1.3.1) ==={RESET}")

    # T7.1 — --compact --fixed-fields: protocol version must be 1.3.1
    t = time.monotonic()
    p = run("--export", "users", "--limit", "20",
            "--compact", "--fixed-fields", "username,email",
            "--output", out("t7_compact.xml"))
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
    p = run("--export", "users", "--limit", "20",
            "--compact", "--fixed-fields", "username,email",
            "--compress", "--hash",
            "--output", out("t7_compact_comp.xml"))
    pt = run_no_cfg("--test", out("t7_compact_comp.xml"))
    checksum_ok = "checksum OK" in pt.stdout
    record("T7.2 compact + compress + --hash → checksum OK",
           p.returncode == 0 and pt.returncode == 0 and checksum_ok,
           time.monotonic() - t, f"test_rc={pt.returncode}")

    # T7.3 — --to-compact converts existing plain file
    run("--export", "users", "--limit", "10",
        "--output", out("t7_plain.xml"))
    t = time.monotonic()
    p = run_no_cfg("--to-compact", out("t7_plain.xml"),
                   "--fixed-fields", "username,email",
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

    # T7.4 — compact roundtrip: import preserves row count
    pg_query("DROP TABLE IF EXISTS rt_users_compact CASCADE")
    t = time.monotonic()
    p = _import(out("t7_compact.xml"), "rt_users_compact")
    rows_sql = pg_query("SELECT COUNT(*) FROM rt_users_compact")
    rows = int(rows_sql[0]) if p.returncode == 0 and rows_sql else -1
    record("T7.4 compact roundtrip: 20 rows imported",
           p.returncode == 0 and rows == 20,
           time.monotonic() - t, f"rows={rows}")
    pg_query("DROP TABLE IF EXISTS rt_users_compact CASCADE")


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
    """Check tdtpcli binary and PostgreSQL availability."""
    if not os.path.exists(TDTPCLI):
        print(f"{RED}ERROR: tdtpcli not found at {TDTPCLI}{RESET}")
        print(f"  Build: GOPROXY=https://goproxy.io GONOSUMDB='*' "
              f"go build -tags nokafka -o {TDTPCLI} ./cmd/tdtpcli/")
        sys.exit(1)

    ver = subprocess.run([TDTPCLI, "--version"], capture_output=True, text=True)
    print(f"tdtpcli: {ver.stdout.strip()}")

    print(f"Checking PostgreSQL at {PG_HOST}:{PG_PORT}...", end=" ", flush=True)
    if not check_pg_available():
        print(f"{RED}NOT AVAILABLE{RESET}")
        print(f"\nPostgreSQL is not running or {PG_DB} is missing.")
        print(f"Start it with:")
        print(f"  sudo pg_ctlcluster 16 main start")
        print(f"  python3 scripts/create_postgres_test_db.py")
        sys.exit(2)
    print(f"{GREEN}OK{RESET}")

    print(f"Checking test data...", end=" ", flush=True)
    if not check_tables_populated():
        print(f"{YELLOW}MISSING — running create_postgres_test_db.py{RESET}")
        root = Path(__file__).resolve().parent.parent.parent
        subprocess.run(
            [sys.executable, str(root / "scripts/create_postgres_test_db.py")],
            check=True,
        )
    else:
        print(f"{GREEN}OK (users={USERS_COUNT}, orders={ORDERS_COUNT}){RESET}")


def main():
    filter_group = sys.argv[1].upper() if len(sys.argv) > 1 else None

    preflight()

    OUTDIR.mkdir(parents=True, exist_ok=True)
    write_cfg(CFG)
    print(f"Config: {CFG}")
    print(f"Output: {OUTDIR}/")

    overall_start = time.monotonic()

    for group_id, fn in GROUPS:
        if filter_group and not group_id.startswith(filter_group):
            continue
        fn()

    passed  = sum(1 for _, ok, _, _ in results if ok)
    failed  = sum(1 for _, ok, _, _ in results if not ok)
    total   = len(results)
    elapsed = time.monotonic() - overall_start

    print(f"\n{BOLD}{'=' * 60}{RESET}")
    print(f"{BOLD}SUMMARY{RESET}")
    print(f"  {GREEN}PASSED: {passed} / {total}{RESET}")
    if failed:
        print(f"  {RED}FAILED: {failed}{RESET}")
        print(f"\n  Failed tests:")
        for tid, ok, _, msg in results:
            if not ok:
                print(f"    {RED}✗ {tid}{RESET}  {msg}")
    print(f"  DURATION: {elapsed:.1f}s")
    print(f"{'=' * 60}")

    sys.exit(0 if failed == 0 else 1)


if __name__ == "__main__":
    main()
