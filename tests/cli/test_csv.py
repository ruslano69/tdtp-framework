#!/usr/bin/env python3
"""
TDTP CLI Integration Tests — --to-csv

Tests all --to-csv flags and combinations:
  TC1  Basic conversion (plain TDTP → CSV)
  TC2  Delimiters (semicolon, tab)
  TC3  BOM flag
  TC4  Encoding cp1251 (Cyrillic data)
  TC5  Column projection (--fields, bracket-quoted names)
  TC6  WHERE filter (numeric, string, repeatable AND)
  TC7  ORDER BY
  TC8  LIMIT first-N and tail -N
  TC9  OFFSET / pagination
  TC10 Compressed input (zstd)
  TC11 Compact v1.3.1 input
  TC12 v1.4 integrity input
  TC13 Combined: fields + where + order-by + limit + delimiter
  TC14 -l alias (same as --limit)
  TC15 --output flag (explicit output path)
  TC16 Error cases (unknown field, missing file)

Usage:
    python3 tests/cli/test_csv.py            # all groups
    python3 tests/cli/test_csv.py TC5        # single group
    TDTPCLI_BIN=/path/to/tdtpcli python3 tests/cli/test_csv.py
"""

import csv
import io
import os
import sys
import time
import shutil
import sqlite3
import subprocess
from pathlib import Path

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")

# ─── Configuration ────────────────────────────────────────────────────────────
ROOT    = Path(__file__).resolve().parent.parent.parent
TDTPCLI = os.environ.get("TDTPCLI_BIN", "/tmp/tdtpcli")
TEST_DB = "/tmp/tdtp_csv_test.db"
OUTDIR  = Path("/tmp/tdtp_csv_out")
CFG     = "/tmp/tdtp_csv_test.yaml"
CFG_C   = "/tmp/tdtp_csv_compress.yaml"     # zstd compression
CFG_K   = "/tmp/tdtp_csv_kanzi.yaml"        # kanzi compression

# ─── ANSI colors ──────────────────────────────────────────────────────────────
GREEN  = "\033[32m"
RED    = "\033[31m"
YELLOW = "\033[33m"
BOLD   = "\033[1m"
RESET  = "\033[0m"

# ─── Global results ───────────────────────────────────────────────────────────
results: list = []


# ─── Helpers ──────────────────────────────────────────────────────────────────

def run(*args, timeout=30) -> subprocess.CompletedProcess:
    """Run tdtpcli (no --config; --to-csv needs no DB)."""
    cmd = [TDTPCLI] + list(args)
    return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)


def run_cfg(*args, cfg=CFG, timeout=30) -> subprocess.CompletedProcess:
    """Run tdtpcli with --config (for export setup phase)."""
    cmd = [TDTPCLI, "--config", cfg] + list(args)
    return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)


def out(name: str) -> str:
    return str(OUTDIR / name)


def read_csv(path: str, encoding: str = "utf-8-sig") -> list[list[str]]:
    """Read CSV file, returns list of rows (header + data). utf-8-sig strips BOM."""
    with open(path, encoding=encoding, newline="") as f:
        return list(csv.reader(f))


def read_csv_delim(path: str, delimiter: str, encoding: str = "utf-8-sig") -> list[list[str]]:
    with open(path, encoding=encoding, newline="") as f:
        return list(csv.reader(f, delimiter=delimiter))


def record(tid: str, passed: bool, elapsed: float, msg: str = ""):
    results.append((tid, passed, elapsed, msg))
    status = f"{GREEN}PASS{RESET}" if passed else f"{RED}FAIL{RESET}"
    detail = f"  ({msg})" if msg and not passed else (f"  ({msg})" if msg else "")
    print(f"  [{status}] {tid:<55} {elapsed:.2f}s{detail}")


def write_cfg(path: str, compress: bool = False,
              algo: str = "zstd", level: int = 3):
    with open(path, "w") as f:
        f.write(f"database:\n  type: sqlite\n  database: {TEST_DB}\n")
        f.write(f"export:\n")
        f.write(f"  compress: {str(compress).lower()}\n")
        f.write(f"  compress_algo: {algo}\n")
        f.write(f"  compress_level: {level}\n")


# ─── Database & fixture setup ─────────────────────────────────────────────────

def setup_db():
    """Create SQLite test DB with users, orders, and a Cyrillic table."""
    if os.path.exists(TEST_DB):
        os.remove(TEST_DB)
    conn = sqlite3.connect(TEST_DB)
    c = conn.cursor()

    # users: 10 rows, 8 columns
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

    # orders: 8 rows
    c.execute("""CREATE TABLE orders (
        OrderID INTEGER PRIMARY KEY, UserID INTEGER, ProductName TEXT,
        Amount NUMERIC(18,2), Status TEXT, CreatedAt DATETIME)""")
    c.executemany("INSERT INTO orders VALUES (?,?,?,?,?,?)", [
        (1, 1, "Laptop",      1500.00, "completed", "2025-11-01 10:00:00"),
        (2, 2, "Phone",        800.00, "pending",   "2025-11-05 11:30:00"),
        (3, 4, "Tablet",       600.00, "completed", "2025-11-03 14:15:00"),
        (4, 6, "Monitor",      400.00, "pending",   "2025-11-10 09:20:00"),
        (5, 8, "Keyboard",     100.00, "completed", "2025-11-08 16:45:00"),
        (6, 10, "Mouse",        50.00, "pending",   "2025-11-12 12:30:00"),
        (7, 1, "Headphones",   200.00, "cancelled", "2025-11-02 13:00:00"),
        (8, 2, "Webcam",       150.00, "completed", "2025-11-09 10:10:00"),
    ])

    # cyrillic: Cyrillic names for encoding tests
    c.execute("""CREATE TABLE cyrillic (
        ID INTEGER PRIMARY KEY, Name TEXT, City TEXT, Amount REAL)""")
    c.executemany("INSERT INTO cyrillic VALUES (?,?,?,?)", [
        (1, "Иванов Иван",    "Москва",       1000.0),
        (2, "Петрова Мария",  "Санкт-Петербург", 2000.0),
        (3, "Сидоров Алексей", "Казань",       1500.0),
    ])

    conn.commit()
    conn.close()


def setup_fixtures():
    """Export TDTP files used as input by --to-csv tests."""
    OUTDIR.mkdir(parents=True, exist_ok=True)

    write_cfg(CFG)
    write_cfg(CFG_C, compress=True, algo="zstd", level=3)
    write_cfg(CFG_K, compress=True, algo="kanzi", level=6)

    # plain TDTP
    run_cfg("--export", "users",   "--output", out("users.tdtp.xml"))
    run_cfg("--export", "orders",  "--output", out("orders.tdtp.xml"))
    run_cfg("--export", "cyrillic","--output", out("cyrillic.tdtp.xml"))

    # zstd-compressed
    run_cfg("--export", "users", "--compress", "--output", out("users_zstd.tdtp.xml"), cfg=CFG_C)

    # v1.4 integrity (plain)
    run_cfg("--export", "users", "--integrity", "--output", out("users_v14.tdtp.xml"))

    # compact v1.3.1
    run_cfg("--export", "users", "--compact", "--output", out("users_compact.tdtp.xml"))

    print(f"  Fixtures: {OUTDIR}/")


# ─── TC1  Basic ───────────────────────────────────────────────────────────────

def test_TC1_basic():
    print(f"\n{BOLD}TC1  Basic conversion{RESET}")

    # TC1.1 — plain TDTP → CSV, row count and header
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"), "--output", out("tc1_users.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc1_users.csv")):
        rows = read_csv(out("tc1_users.csv"))
    header_ok  = rows and rows[0] == ["ID","Name","Email","Balance","IsActive","City","CreatedAt","LastLoginAt"]
    row_count_ok = len(rows) == 11  # 1 header + 10 data
    record("TC1.1 plain → CSV: header + 10 data rows",
           p.returncode == 0 and header_ok and row_count_ok,
           time.monotonic() - t,
           f"rc={p.returncode} rows={len(rows)} header={header_ok}")

    # TC1.2 — auto output name: no --output → users.tdtp.xml.csv or users.csv
    t = time.monotonic()
    src = out("users.tdtp.xml")
    p = run("--to-csv", src)
    # auto output is next to source or in cwd — check either users.csv or users.tdtp.xml.csv
    auto1 = src.replace(".tdtp.xml", ".csv")
    auto2 = src + ".csv"
    auto3 = "users.csv"
    found = next((x for x in [auto1, auto2, auto3] if os.path.exists(x)), None)
    record("TC1.2 auto output file created",
           p.returncode == 0 and found is not None,
           time.monotonic() - t,
           f"rc={p.returncode} found={found}")


# ─── TC2  Delimiters ──────────────────────────────────────────────────────────

def test_TC2_delimiters():
    print(f"\n{BOLD}TC2  Delimiters{RESET}")

    # TC2.1 — semicolon
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"), "--delimiter", ";",
            "--output", out("tc2_semi.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc2_semi.csv")):
        rows = read_csv_delim(out("tc2_semi.csv"), ";")
    ok = p.returncode == 0 and len(rows) == 11 and rows[0][0] == "ID" and rows[0][1] == "Name"
    record("TC2.1 semicolon delimiter → 11 rows, correct split",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)}")

    # TC2.2 — shorthand -d
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"), "-d", ";",
            "--output", out("tc2_d_short.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc2_d_short.csv")):
        rows = read_csv_delim(out("tc2_d_short.csv"), ";")
    record("TC2.2 -d shorthand (semicolon) → same as --delimiter",
           p.returncode == 0 and len(rows) == 11,
           time.monotonic() - t, f"rc={p.returncode} rows={len(rows)}")

    # TC2.3 — tab
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"), "--delimiter", "\t",
            "--output", out("tc2_tab.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc2_tab.csv")):
        rows = read_csv_delim(out("tc2_tab.csv"), "\t")
    ok = p.returncode == 0 and len(rows) == 11 and len(rows[0]) == 8
    record("TC2.3 tab delimiter → 11 rows, 8 columns",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)}")


# ─── TC3  BOM ─────────────────────────────────────────────────────────────────

def test_TC3_bom():
    print(f"\n{BOLD}TC3  BOM{RESET}")

    # TC3.1 — BOM present when --bom given
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"), "--bom",
            "--output", out("tc3_bom.csv"))
    has_bom = False
    if p.returncode == 0 and os.path.exists(out("tc3_bom.csv")):
        with open(out("tc3_bom.csv"), "rb") as f:
            has_bom = f.read(3) == b"\xef\xbb\xbf"
    record("TC3.1 --bom → UTF-8 BOM prefix (EF BB BF)",
           p.returncode == 0 and has_bom,
           time.monotonic() - t, f"rc={p.returncode} bom={has_bom}")

    # TC3.2 — no BOM by default
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"), "--output", out("tc3_nobom.csv"))
    no_bom = False
    if p.returncode == 0 and os.path.exists(out("tc3_nobom.csv")):
        with open(out("tc3_nobom.csv"), "rb") as f:
            no_bom = f.read(3) != b"\xef\xbb\xbf"
    record("TC3.2 no --bom → no BOM by default",
           p.returncode == 0 and no_bom,
           time.monotonic() - t, f"rc={p.returncode} no_bom={no_bom}")


# ─── TC4  Encoding ────────────────────────────────────────────────────────────

def test_TC4_encoding():
    print(f"\n{BOLD}TC4  Encoding{RESET}")

    # TC4.1 — cp1251: Cyrillic data encoded in Windows-1251
    t = time.monotonic()
    p = run("--to-csv", out("cyrillic.tdtp.xml"), "--cp", "1251",
            "--output", out("tc4_cp1251.csv"))
    ok = False
    if p.returncode == 0 and os.path.exists(out("tc4_cp1251.csv")):
        try:
            rows = read_csv(out("tc4_cp1251.csv"), encoding="cp1251")
            # row[1] should be the Name column (header = Name)
            names = [r[1] for r in rows[1:] if len(r) > 1]
            ok = any("Иванов" in n for n in names)
        except Exception:
            ok = False
    record("TC4.1 --cp 1251: Cyrillic names readable in cp1251",
           p.returncode == 0 and ok,
           time.monotonic() - t, f"rc={p.returncode} ok={ok}")

    # TC4.2 — default utf8: same Cyrillic data readable in UTF-8
    t = time.monotonic()
    p = run("--to-csv", out("cyrillic.tdtp.xml"), "--output", out("tc4_utf8.csv"))
    ok = False
    if p.returncode == 0 and os.path.exists(out("tc4_utf8.csv")):
        try:
            rows = read_csv(out("tc4_utf8.csv"), encoding="utf-8")
            names = [r[1] for r in rows[1:] if len(r) > 1]
            ok = any("Иванов" in n for n in names)
        except Exception:
            ok = False
    record("TC4.2 default utf8: Cyrillic names readable in UTF-8",
           p.returncode == 0 and ok,
           time.monotonic() - t, f"rc={p.returncode} ok={ok}")


# ─── TC5  Column projection ───────────────────────────────────────────────────

def test_TC5_fields():
    print(f"\n{BOLD}TC5  Column projection (--fields){RESET}")

    # TC5.1 — 3 of 8 columns
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"),
            "--fields", "ID,Name,Balance",
            "--output", out("tc5_3cols.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc5_3cols.csv")):
        rows = read_csv(out("tc5_3cols.csv"))
    ok = (p.returncode == 0
          and len(rows) == 11
          and rows[0] == ["ID", "Name", "Balance"]
          and len(rows[1]) == 3)
    record("TC5.1 --fields ID,Name,Balance → 3 columns only",
           ok, time.monotonic() - t,
           f"rc={p.returncode} cols={len(rows[0]) if rows else '?'}")

    # TC5.2 — single column
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"),
            "--fields", "Email",
            "--output", out("tc5_email.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc5_email.csv")):
        rows = read_csv(out("tc5_email.csv"))
    ok = (p.returncode == 0
          and len(rows) == 11
          and rows[0] == ["Email"]
          and "john@example.com" in [r[0] for r in rows[1:]])
    record("TC5.2 --fields Email → 1 column, 10 data rows",
           ok, time.monotonic() - t,
           f"rc={p.returncode} rows={len(rows)}")

    # TC5.3 — unknown field → non-zero exit
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"),
            "--fields", "ID,NonExistentColumn",
            "--output", out("tc5_badfield.csv"))
    record("TC5.3 unknown field in --fields → error exit",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode}")

    # TC5.4 — column order follows --fields, not schema order
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"),
            "--fields", "Balance,Name,ID",
            "--output", out("tc5_reorder.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc5_reorder.csv")):
        rows = read_csv(out("tc5_reorder.csv"))
    ok = rows and rows[0] == ["Balance", "Name", "ID"]
    record("TC5.4 --fields Balance,Name,ID → columns in requested order",
           p.returncode == 0 and ok,
           time.monotonic() - t,
           f"rc={p.returncode} header={rows[0] if rows else '?'}")


# ─── TC6  WHERE filter ────────────────────────────────────────────────────────

def test_TC6_where():
    print(f"\n{BOLD}TC6  WHERE filter{RESET}")

    # TC6.1 — numeric condition: Balance > 1500 → IDs 2,4,6,8,10 = 5 rows
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"),
            "--where", "Balance > 1500",
            "--output", out("tc6_balance.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc6_balance.csv")):
        rows = read_csv(out("tc6_balance.csv"))
    ok = p.returncode == 0 and len(rows) == 6  # 1 header + 5 data
    record("TC6.1 --where Balance > 1500 → 5 rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC6.2 — string condition: City = Moscow → IDs 1,3,6,7,10 = 5 rows
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"),
            "--where", "City = Moscow",
            "--output", out("tc6_city.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc6_city.csv")):
        rows = read_csv(out("tc6_city.csv"))
    ok = p.returncode == 0 and len(rows) == 6
    record("TC6.2 --where City = Moscow → 5 rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC6.3 — IsActive = 0 → 2 rows (IDs 3,7)
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"),
            "--where", "IsActive = 0",
            "--output", out("tc6_inactive.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc6_inactive.csv")):
        rows = read_csv(out("tc6_inactive.csv"))
    ok = p.returncode == 0 and len(rows) == 3  # 1 header + 2 data
    record("TC6.3 --where IsActive = 0 → 2 rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC6.4 — repeatable --where (AND): City=Moscow AND IsActive=1 → IDs 1,6,10 = 3 rows
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"),
            "--where", "City = Moscow",
            "--where", "IsActive = 1",
            "--output", out("tc6_and.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc6_and.csv")):
        rows = read_csv(out("tc6_and.csv"))
    ok = p.returncode == 0 and len(rows) == 4  # 1 header + 3 data
    record("TC6.4 two --where (AND): City=Moscow AND IsActive=1 → 3 rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC6.5 — -w shorthand
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"),
            "-w", "Balance > 1500",
            "--output", out("tc6_w_short.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc6_w_short.csv")):
        rows = read_csv(out("tc6_w_short.csv"))
    ok = p.returncode == 0 and len(rows) == 6
    record("TC6.5 -w shorthand (alias for --where)",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC6.6 — IN operator: City IN (Moscow,SPb) → Moscow(1,3,6,7,10) + SPb(2,5,8) = 8 rows
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"),
            "--where", "City IN (Moscow,SPb)",
            "--output", out("tc6_in.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc6_in.csv")):
        rows = read_csv(out("tc6_in.csv"))
    ok = p.returncode == 0 and len(rows) == 9  # 1 header + 8 data
    record("TC6.6 --where City IN (Moscow,SPb) → 8 rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")


# ─── TC7  ORDER BY ────────────────────────────────────────────────────────────

def test_TC7_orderby():
    print(f"\n{BOLD}TC7  ORDER BY{RESET}")

    # TC7.1 — sort Balance DESC → first data row has highest balance (Emma Wilson 3000)
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"),
            "--order-by", "Balance DESC",
            "--output", out("tc7_balance_desc.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc7_balance_desc.csv")):
        rows = read_csv(out("tc7_balance_desc.csv"))
    # Balance column is index 3; first data row = highest
    first_balance = float(rows[1][3]) if len(rows) > 1 and rows[1][3] else 0
    ok = p.returncode == 0 and len(rows) == 11 and first_balance == 3000.0
    record("TC7.1 --order-by Balance DESC → first row = 3000 (Emma Wilson)",
           ok, time.monotonic() - t,
           f"rc={p.returncode} first_balance={first_balance}")

    # TC7.2 — sort Balance ASC → first data row has lowest balance (Henry Taylor 400)
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"),
            "--order-by", "Balance ASC",
            "--output", out("tc7_balance_asc.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc7_balance_asc.csv")):
        rows = read_csv(out("tc7_balance_asc.csv"))
    first_balance = float(rows[1][3]) if len(rows) > 1 and rows[1][3] else 0
    ok = p.returncode == 0 and len(rows) == 11 and first_balance == 400.0
    record("TC7.2 --order-by Balance ASC → first row = 400 (Henry Taylor)",
           ok, time.monotonic() - t,
           f"rc={p.returncode} first_balance={first_balance}")


# ─── TC8  LIMIT & tail ────────────────────────────────────────────────────────

def test_TC8_limit():
    print(f"\n{BOLD}TC8  LIMIT (first-N and tail -N){RESET}")

    # TC8.1 — first 3
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"),
            "--limit", "3",
            "--output", out("tc8_first3.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc8_first3.csv")):
        rows = read_csv(out("tc8_first3.csv"))
    ok = p.returncode == 0 and len(rows) == 4  # 1 header + 3 data
    record("TC8.1 --limit 3 → 3 data rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC8.2 — tail -3 (last 3 by natural order)
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"),
            "--limit", "-3",
            "--output", out("tc8_tail3.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc8_tail3.csv")):
        rows = read_csv(out("tc8_tail3.csv"))
    ok = p.returncode == 0 and len(rows) == 4
    record("TC8.2 --limit -3 (tail mode) → 3 data rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC8.3 — --limit > total rows → all 10 rows (no error)
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"),
            "--limit", "999",
            "--output", out("tc8_overlimit.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc8_overlimit.csv")):
        rows = read_csv(out("tc8_overlimit.csv"))
    ok = p.returncode == 0 and len(rows) == 11
    record("TC8.3 --limit 999 (> total) → all 10 rows, no error",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC8.4 — filter + limit combined: Balance > 1000, limit 3 → 3 rows
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"),
            "--where", "Balance > 1000",
            "--limit", "3",
            "--output", out("tc8_filter_limit.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc8_filter_limit.csv")):
        rows = read_csv(out("tc8_filter_limit.csv"))
    ok = p.returncode == 0 and len(rows) == 4
    record("TC8.4 --where Balance > 1000 --limit 3 → 3 rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")


# ─── TC9  OFFSET / pagination ─────────────────────────────────────────────────

def test_TC9_offset():
    print(f"\n{BOLD}TC9  OFFSET / pagination{RESET}")

    # TC9.1 — skip first 7 → 3 remaining rows (IDs 8,9,10)
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"),
            "--offset", "7",
            "--output", out("tc9_skip7.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc9_skip7.csv")):
        rows = read_csv(out("tc9_skip7.csv"))
    ok = p.returncode == 0 and len(rows) == 4  # 1 header + 3 data
    record("TC9.1 --offset 7 → 3 remaining rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC9.2 — pagination: limit 4, offset 4 → rows 5–8
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"),
            "--limit", "4", "--offset", "4",
            "--output", out("tc9_page2.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc9_page2.csv")):
        rows = read_csv(out("tc9_page2.csv"))
    ok = p.returncode == 0 and len(rows) == 5  # 1 header + 4 data
    record("TC9.2 --limit 4 --offset 4 → rows 5–8 (4 rows)",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC9.3 — offset >= total rows → 0 data rows (just header)
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"),
            "--offset", "100",
            "--output", out("tc9_skip_all.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc9_skip_all.csv")):
        rows = read_csv(out("tc9_skip_all.csv"))
    ok = p.returncode == 0 and len(rows) <= 1  # header only (or empty)
    record("TC9.3 --offset 100 (> total) → 0 data rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")


# ─── TC10  Compressed input ───────────────────────────────────────────────────

def test_TC10_compressed():
    print(f"\n{BOLD}TC10  Compressed input (zstd){RESET}")

    # TC10.1 — zstd compressed → same result as plain
    t = time.monotonic()
    p = run("--to-csv", out("users_zstd.tdtp.xml"),
            "--output", out("tc10_zstd.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc10_zstd.csv")):
        rows = read_csv(out("tc10_zstd.csv"))
    ok = p.returncode == 0 and len(rows) == 11
    record("TC10.1 zstd-compressed input → 10 rows (auto-decompress)",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC10.2 — zstd + filter
    t = time.monotonic()
    p = run("--to-csv", out("users_zstd.tdtp.xml"),
            "--where", "Balance > 1500",
            "--output", out("tc10_zstd_filter.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc10_zstd_filter.csv")):
        rows = read_csv(out("tc10_zstd_filter.csv"))
    ok = p.returncode == 0 and len(rows) == 6
    record("TC10.2 zstd + --where Balance > 1500 → 5 rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC10.3 — zstd + fields projection
    t = time.monotonic()
    p = run("--to-csv", out("users_zstd.tdtp.xml"),
            "--fields", "ID,Name,Balance",
            "--output", out("tc10_zstd_fields.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc10_zstd_fields.csv")):
        rows = read_csv(out("tc10_zstd_fields.csv"))
    ok = (p.returncode == 0
          and len(rows) == 11
          and rows[0] == ["ID", "Name", "Balance"])
    record("TC10.3 zstd + --fields ID,Name,Balance → 3 columns",
           ok, time.monotonic() - t,
           f"rc={p.returncode} cols={len(rows[0]) if rows else '?'}")


# ─── TC11  Compact v1.3.1 ─────────────────────────────────────────────────────

def test_TC11_compact():
    print(f"\n{BOLD}TC11  Compact v1.3.1 input{RESET}")

    # TC11.1 — compact TDTP → full CSV (all rows expanded)
    t = time.monotonic()
    p = run("--to-csv", out("users_compact.tdtp.xml"),
            "--output", out("tc11_compact.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc11_compact.csv")):
        rows = read_csv(out("tc11_compact.csv"))
    ok = p.returncode == 0 and len(rows) == 11
    record("TC11.1 compact v1.3.1 → all 10 rows expanded in CSV",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC11.2 — compact + filter
    t = time.monotonic()
    p = run("--to-csv", out("users_compact.tdtp.xml"),
            "--where", "City = Moscow",
            "--output", out("tc11_compact_filter.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc11_compact_filter.csv")):
        rows = read_csv(out("tc11_compact_filter.csv"))
    ok = p.returncode == 0 and len(rows) == 6  # 1 header + 5 Moscow rows
    record("TC11.2 compact + --where City = Moscow → 5 rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")


# ─── TC12  v1.4 integrity ─────────────────────────────────────────────────────

def test_TC12_v14():
    print(f"\n{BOLD}TC12  v1.4 integrity input{RESET}")

    # TC12.1 — v1.4 plain → CSV (hashes verified, all rows)
    t = time.monotonic()
    p = run("--to-csv", out("users_v14.tdtp.xml"),
            "--output", out("tc12_v14.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc12_v14.csv")):
        rows = read_csv(out("tc12_v14.csv"))
    ok = p.returncode == 0 and len(rows) == 11
    record("TC12.1 v1.4 integrity → hashes verified, 10 rows in CSV",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC12.2 — v1.4 + fields + where
    t = time.monotonic()
    p = run("--to-csv", out("users_v14.tdtp.xml"),
            "--fields", "ID,Name,Balance",
            "--where", "Balance > 1500",
            "--output", out("tc12_v14_combo.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc12_v14_combo.csv")):
        rows = read_csv(out("tc12_v14_combo.csv"))
    ok = (p.returncode == 0
          and len(rows) == 6
          and rows[0] == ["ID", "Name", "Balance"])
    record("TC12.2 v1.4 + --fields + --where → 5 rows, 3 cols",
           ok, time.monotonic() - t,
           f"rc={p.returncode} rows={len(rows)-1 if rows else '?'} cols={len(rows[0]) if rows else '?'}")


# ─── TC13  Combined query ─────────────────────────────────────────────────────

def test_TC13_combined():
    print(f"\n{BOLD}TC13  Combined query{RESET}")

    # TC13.1 — fields + where + order-by + limit + semicolon
    # Balance > 1000 → IDs 1,2,4,6,7,8,10 = 7 rows; limit 3, sort DESC → top 3
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"),
            "--fields", "ID,Name,Balance",
            "--where", "Balance > 1000",
            "--order-by", "Balance DESC",
            "--limit", "3",
            "--delimiter", ";",
            "--output", out("tc13_combo.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc13_combo.csv")):
        rows = read_csv_delim(out("tc13_combo.csv"), ";")
    ok = (p.returncode == 0
          and len(rows) == 4           # 1 header + 3 data
          and rows[0] == ["ID", "Name", "Balance"]
          and float(rows[1][2]) == 3000.0)  # first = highest balance
    record("TC13.1 fields+where+order-by+limit+semicolon → top-3 by Balance DESC",
           ok, time.monotonic() - t,
           f"rc={p.returncode} rows={len(rows)-1 if rows else '?'} top={rows[1][2] if len(rows)>1 else '?'}")

    # TC13.2 — orders: status=completed + fields + sort Amount DESC + limit 3
    t = time.monotonic()
    p = run("--to-csv", out("orders.tdtp.xml"),
            "--fields", "OrderID,ProductName,Amount",
            "--where", "Status = completed",
            "--order-by", "Amount DESC",
            "--limit", "3",
            "--output", out("tc13_orders.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc13_orders.csv")):
        rows = read_csv(out("tc13_orders.csv"))
    # completed orders: 1(1500), 3(600), 5(100), 8(150) → top-3: 1500,600,150
    ok = (p.returncode == 0
          and len(rows) == 4
          and rows[0] == ["OrderID", "ProductName", "Amount"]
          and float(rows[1][2]) == 1500.0)
    record("TC13.2 orders: status=completed + top-3 by Amount DESC",
           ok, time.monotonic() - t,
           f"rc={p.returncode} rows={len(rows)-1 if rows else '?'} top={rows[1][2] if len(rows)>1 else '?'}")


# ─── TC14  -l alias ───────────────────────────────────────────────────────────

def test_TC14_l_alias():
    print(f"\n{BOLD}TC14  -l alias (shorthand for --limit){RESET}")

    # TC14.1 — -l 5 same as --limit 5
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"),
            "-l", "5",
            "--output", out("tc14_l5.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc14_l5.csv")):
        rows = read_csv(out("tc14_l5.csv"))
    ok = p.returncode == 0 and len(rows) == 6  # 1 header + 5 data
    record("TC14.1 -l 5 → 5 data rows (alias for --limit)",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC14.2 — -l -3 (tail mode via -l)
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"),
            "-l", "-3",
            "--output", out("tc14_tail3.csv"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc14_tail3.csv")):
        rows = read_csv(out("tc14_tail3.csv"))
    ok = p.returncode == 0 and len(rows) == 4
    record("TC14.2 -l -3 (tail mode) → 3 data rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")


# ─── TC15  --output flag ──────────────────────────────────────────────────────

def test_TC15_output():
    print(f"\n{BOLD}TC15  --output flag{RESET}")

    # TC15.1 — explicit output path
    target = out("tc15_explicit.csv")
    t = time.monotonic()
    p = run("--to-csv", out("users.tdtp.xml"), "--output", target)
    ok = p.returncode == 0 and os.path.exists(target)
    record("TC15.1 --output <path> → file created at exact path",
           ok, time.monotonic() - t, f"rc={p.returncode} exists={os.path.exists(target)}")

    # TC15.2 — auto-generated output name (no --output → <input>.csv next to source)
    t = time.monotonic()
    src = out("orders.tdtp.xml")
    # remove any previous auto-output
    for candidate in [src.replace(".tdtp.xml", ".csv"), src + ".csv"]:
        if os.path.exists(candidate):
            os.remove(candidate)
    p2 = run("--to-csv", src)
    auto = next((x for x in [src.replace(".tdtp.xml", ".csv"), src + ".csv"]
                 if os.path.exists(x)), None)
    rows2 = read_csv(auto) if auto else []
    ok = p2.returncode == 0 and auto is not None and len(rows2) == 9  # 1 header + 8 orders
    record("TC15.2 no --output → auto file created with 8 data rows",
           ok, time.monotonic() - t,
           f"rc={p2.returncode} file={Path(auto).name if auto else 'none'} rows={len(rows2)-1 if rows2 else '?'}")


# ─── TC16  Error cases ────────────────────────────────────────────────────────

def test_TC16_errors():
    print(f"\n{BOLD}TC16  Error cases{RESET}")

    # TC16.1 — missing input file → non-zero exit
    t = time.monotonic()
    p = run("--to-csv", "/tmp/does_not_exist_12345.tdtp.xml")
    record("TC16.1 missing input file → error exit",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode}")

    # TC16.2 — unknown field in --fields → non-zero exit + no output file
    t = time.monotonic()
    bad_out = out("tc16_bad.csv")
    p = run("--to-csv", out("users.tdtp.xml"),
            "--fields", "ID,GhostColumn",
            "--output", bad_out)
    ok = p.returncode != 0 and not os.path.exists(bad_out)
    record("TC16.2 unknown --fields column → error exit, no output written",
           ok, time.monotonic() - t, f"rc={p.returncode}")


# ─── Runner ───────────────────────────────────────────────────────────────────

GROUPS = [
    ("TC1",  test_TC1_basic),
    ("TC2",  test_TC2_delimiters),
    ("TC3",  test_TC3_bom),
    ("TC4",  test_TC4_encoding),
    ("TC5",  test_TC5_fields),
    ("TC6",  test_TC6_where),
    ("TC7",  test_TC7_orderby),
    ("TC8",  test_TC8_limit),
    ("TC9",  test_TC9_offset),
    ("TC10", test_TC10_compressed),
    ("TC11", test_TC11_compact),
    ("TC12", test_TC12_v14),
    ("TC13", test_TC13_combined),
    ("TC14", test_TC14_l_alias),
    ("TC15", test_TC15_output),
    ("TC16", test_TC16_errors),
]


def preflight():
    if not os.path.exists(TDTPCLI):
        print(f"{RED}ERROR: tdtpcli not found at {TDTPCLI}{RESET}")
        print(f"Build: GOPROXY=https://proxy.golang.org GONOSUMDB='*' "
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
    print("Exporting TDTP fixtures...")
    setup_fixtures()

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
    print(f"{BOLD}SUMMARY  --to-csv{RESET}")
    print(f"  {GREEN}PASSED: {passed} / {total}{RESET}")
    if failed:
        print(f"  {RED}FAILED: {failed}{RESET}")
        print(f"\n  Failed tests:")
        for tid, ok, _, msg in results:
            if not ok:
                print(f"    {RED}✗ {tid}{RESET}  {msg}")
    print(f"  Total time: {elapsed:.1f}s")
    print(f"{BOLD}{'=' * 60}{RESET}")

    sys.exit(0 if failed == 0 else 1)


if __name__ == "__main__":
    main()
