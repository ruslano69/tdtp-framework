#!/usr/bin/env python3
"""
TDTP CLI Integration Tests — --to-xlsx

Mirror of tests/cli/test_csv.py for the XLSX converter. Same fixtures,
same group numbering (TC1..TC16), adapted to what --to-xlsx supports:

  TC1  Basic conversion (plain TDTP → XLSX, header + row count, auto name)
  TC2  Sheet name (default Sheet1, custom --sheet)
  TC3  Unicode (Cyrillic data survives — XLSX is always Unicode, no --cp/--bom)
  TC4  Special characters (comma/semicolon/tab/quotes/leading '=' stay intact)
  TC5  Column projection (--fields)
  TC6  WHERE filter (numeric, string, repeatable AND)
  TC7  ORDER BY
  TC8  LIMIT first-N and tail -N
  TC9  OFFSET / pagination
  TC10 Compressed input (zstd)
  TC11 Compact v1.3.1 input
  TC12 v1.4 integrity input
  TC13 Combined: fields + where + order-by + limit + sheet
  TC14 -l alias (same as --limit)
  TC15 --output flag (explicit output path)
  TC16 Error cases (unknown field, missing file)
  TC17 Wide table (40 columns → multi-letter refs AA..AO stay aligned)

Hard-limit refusal (>1_048_575 data rows, >16_384 columns, >32_767 chars per
cell — past them Excel reports the file as corrupt) is covered by Go unit
tests in pkg/xlsx/limits_test.go: triggering the real spec limits through
the CLI would need million-row / 16K-column fixtures (SQLite itself caps a
table at 2000 columns), so the suite only proves the near side — a wide
table converts intact.

Differences from CSV to keep in mind:
  * XLSX headers carry types: "ID (INTEGER) *", "Name (TEXT)" — compare by
    base name (text before " (") unless the type itself is asserted.
  * Numbers are stored as Excel serials: 1500.00 reads back as "1500",
    2000.50 as "2000.5" — compare numeric cells via float().
  * No --delimiter/--bom/--cp flags exist for XLSX (covered by TC2..TC4 instead).
  * The reader below uses only the stdlib (zipfile + ElementTree): the repo's
    writer emits inline strings, shared strings are handled too in case Excel
    itself resaved the file.

Usage:
    python3 tests/cli/test_xlsx.py            # all groups
    python3 tests/cli/test_xlsx.py TC5        # single group
    TDTPCLI_BIN=/path/to/tdtpcli python3 tests/cli/test_xlsx.py
"""

import os
import re
import sys
import time
import sqlite3
import subprocess
import zipfile
import xml.etree.ElementTree as ET
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from tdtp_binary import check_binary

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")

# ─── Configuration ────────────────────────────────────────────────────────────
ROOT    = Path(__file__).resolve().parent.parent.parent
TDTPCLI = os.environ.get("TDTPCLI_BIN", "/tmp/tdtpcli")
TEST_DB = "/tmp/tdtp_xlsx_test.db"
OUTDIR  = Path("/tmp/tdtp_xlsx_out")
CFG     = "/tmp/tdtp_xlsx_test.yaml"
CFG_C   = "/tmp/tdtp_xlsx_compress.yaml"     # zstd compression

# ─── ANSI colors ──────────────────────────────────────────────────────────────
GREEN  = "\033[32m"
RED    = "\033[31m"
YELLOW = "\033[33m"
BOLD   = "\033[1m"
RESET  = "\033[0m"

# ─── Global results ───────────────────────────────────────────────────────────
results: list = []


# ─── Helpers ──────────────────────────────────────────────────────────────────

def run(*args, timeout=60) -> subprocess.CompletedProcess:
    """Run tdtpcli (no --config; --to-xlsx needs no DB)."""
    cmd = [TDTPCLI] + list(args)
    return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)


def run_cfg(*args, cfg=CFG, timeout=60) -> subprocess.CompletedProcess:
    """Run tdtpcli with --config (for export setup phase)."""
    cmd = [TDTPCLI, "--config", cfg] + list(args)
    return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)


def out(name: str) -> str:
    return str(OUTDIR / name)


NS = "{http://schemas.openxmlformats.org/spreadsheetml/2006/main}"


def col_to_index(cell_ref: str) -> int:
    """A1-style ref → 0-based column index."""
    letters = re.match(r"([A-Z]+)", cell_ref).group(1)
    idx = 0
    for ch in letters:
        idx = idx * 26 + (ord(ch) - ord("A") + 1)
    return idx - 1


def read_xlsx(path: str) -> list[list[str]]:
    """Read first worksheet of an XLSX file, returns rows (header + data).

    Handles inline strings (what tdtpcli writes), plain numbers, and shared
    strings (in case the file was resaved by Excel). Blank cells → "".
    """
    with zipfile.ZipFile(path) as z:
        shared = []
        if "xl/sharedStrings.xml" in z.namelist():
            sst = ET.fromstring(z.read("xl/sharedStrings.xml"))
            for si in sst.findall(f"{NS}si"):
                shared.append("".join(t.text or "" for t in si.iter(f"{NS}t")))
        sheet_xml = z.read("xl/worksheets/sheet1.xml")
    root = ET.fromstring(sheet_xml)
    cell_map: dict[tuple[int, int], str] = {}
    max_row, max_col = 0, 0
    for row in root.iter(f"{NS}row"):
        r = int(row.get("r")) - 1
        max_row = max(max_row, r)
        for c in row.findall(f"{NS}c"):
            col = col_to_index(c.get("r"))
            max_col = max(max_col, col)
            t = c.get("t")
            if t == "inlineStr":
                is_el = c.find(f"{NS}is")
                val = "".join(x.text or "" for x in is_el.iter(f"{NS}t")) if is_el is not None else ""
            elif t == "s":
                v = c.find(f"{NS}v")
                val = shared[int(v.text)] if v is not None and v.text else ""
            else:
                v = c.find(f"{NS}v")
                val = v.text if v is not None and v.text is not None else ""
            cell_map[(r, col)] = val
    return [[cell_map.get((r, c), "") for c in range(max_col + 1)]
            for r in range(max_row + 1)]


def sheet_name(path: str) -> str:
    """Sheet name from xl/workbook.xml."""
    with zipfile.ZipFile(path) as z:
        root = ET.fromstring(z.read("xl/workbook.xml"))
    sheet = root.find(f".//{NS}sheet")
    return sheet.get("name") if sheet is not None else ""


def base_names(header: list[str]) -> list[str]:
    """Strip XLSX type suffixes: 'ID (INTEGER) *' → 'ID'."""
    return [h.split(" (")[0] for h in header]


def fval(s: str) -> float:
    return float(s) if s not in ("", None) else 0.0


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
    """Create SQLite test DB with users, orders, cyrillic and special tables."""
    if os.path.exists(TEST_DB):
        os.remove(TEST_DB)
    conn = sqlite3.connect(TEST_DB)
    c = conn.cursor()

    # users: 10 rows, 8 columns (same as test_csv.py)
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

    # orders: 8 rows (same as test_csv.py)
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

    # cyrillic: Cyrillic names for unicode tests
    c.execute("""CREATE TABLE cyrillic (
        ID INTEGER PRIMARY KEY, Name TEXT, City TEXT, Amount REAL)""")
    c.executemany("INSERT INTO cyrillic VALUES (?,?,?,?)", [
        (1, "Иванов Иван",    "Москва",       1000.0),
        (2, "Петрова Мария",  "Санкт-Петербург", 2000.0),
        (3, "Сидоров Алексей", "Казань",       1500.0),
    ])

    # special: values that break naive CSV encoders — in XLSX each must stay
    # a single intact cell (mirrors csv_delimiter_test.go at CLI level)
    c.execute("""CREATE TABLE special (
        ID INTEGER PRIMARY KEY, Name TEXT, Note TEXT)""")
    c.executemany("INSERT INTO special VALUES (?,?,?)", [
        (1, "simple",         "plain value"),
        (2, "Semi; Colon",    "contains semicolon"),
        (3, "Comma, Inc",     "contains comma"),
        (4, "Tab\tName",     "contains tab"),
        (5, 'Quote "Q" Name', "contains double-quote"),
        (6, "=SUM(A1:A2)",    "leading equals stays text"),
    ])

    # wide: 1 key + 40 text columns → refs past Z (AA, AB, …) must stay aligned
    wide_cols = [f"W{i:02d}" for i in range(1, 41)]
    c.execute(f"CREATE TABLE wide (ID INTEGER PRIMARY KEY, {', '.join(f'{w} TEXT' for w in wide_cols)})")
    c.executemany(f"INSERT INTO wide VALUES ({', '.join(['?'] * 41)})", [
        tuple([i] + [f"r{i}{w}" for w in wide_cols]) for i in (1, 2, 3)
    ])

    conn.commit()
    conn.close()


def setup_fixtures():
    """Export TDTP files used as input by --to-xlsx tests."""
    OUTDIR.mkdir(parents=True, exist_ok=True)

    write_cfg(CFG)
    write_cfg(CFG_C, compress=True, algo="zstd", level=3)

    # plain TDTP
    run_cfg("--export", "users",    "--output", out("users.tdtp.xml"))
    run_cfg("--export", "orders",   "--output", out("orders.tdtp.xml"))
    run_cfg("--export", "cyrillic", "--output", out("cyrillic.tdtp.xml"))
    run_cfg("--export", "special",  "--output", out("special.tdtp.xml"))
    run_cfg("--export", "wide",     "--output", out("wide.tdtp.xml"))

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

    # TC1.1 — plain TDTP → XLSX, row count and header
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"), "--output", out("tc1_users.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc1_users.xlsx")):
        rows = read_xlsx(out("tc1_users.xlsx"))
    header_ok = (rows and base_names(rows[0]) ==
                 ["ID", "Name", "Email", "Balance", "IsActive", "City", "CreatedAt", "LastLoginAt"])
    row_count_ok = len(rows) == 11  # 1 header + 10 data
    record("TC1.1 plain → XLSX: header + 10 data rows",
           p.returncode == 0 and header_ok and row_count_ok,
           time.monotonic() - t,
           f"rc={p.returncode} rows={len(rows)} header_ok={header_ok}")

    # TC1.2 — PK header carries '*' marker, types in parentheses
    t = time.monotonic()
    ok = bool(rows) and rows[0][0].endswith("*") and "(TEXT)" in rows[0][1]
    record("TC1.2 header shows types + '*' PK marker",
           ok, time.monotonic() - t,
           f"header={rows[0][:3] if rows else '?'}")

    # TC1.3 — auto output name: no --output → <input>.xlsx next to source
    t = time.monotonic()
    src = out("users.tdtp.xml")
    auto = src + ".xlsx"
    if os.path.exists(auto):
        os.remove(auto)
    p = run("--to-xlsx", src)
    found = auto if os.path.exists(auto) else None
    record("TC1.3 auto output file created (<input>.tdtp.xml.xlsx)",
           p.returncode == 0 and found is not None,
           time.monotonic() - t,
           f"rc={p.returncode} found={found}")


# ─── TC2  Sheet name ──────────────────────────────────────────────────────────

def test_TC2_sheet():
    print(f"\n{BOLD}TC2  Sheet name{RESET}")

    # TC2.1 — default sheet is Sheet1
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"), "--output", out("tc2_default.xlsx"))
    name = ""
    if p.returncode == 0 and os.path.exists(out("tc2_default.xlsx")):
        name = sheet_name(out("tc2_default.xlsx"))
    record("TC2.1 default sheet is Sheet1",
           p.returncode == 0 and name == "Sheet1",
           time.monotonic() - t, f"rc={p.returncode} sheet={name!r}")

    # TC2.2 — custom --sheet
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"), "--sheet", "Users",
            "--output", out("tc2_custom.xlsx"))
    name, rows = "", []
    if p.returncode == 0 and os.path.exists(out("tc2_custom.xlsx")):
        name = sheet_name(out("tc2_custom.xlsx"))
        rows = read_xlsx(out("tc2_custom.xlsx"))
    record("TC2.2 --sheet Users → sheet renamed, data intact",
           p.returncode == 0 and name == "Users" and len(rows) == 11,
           time.monotonic() - t,
           f"rc={p.returncode} sheet={name!r} rows={len(rows)}")

    # TC2.3 — custom sheet + filter combined
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"), "--sheet", "Filtered",
            "--where", "Balance > 1500",
            "--output", out("tc2_sheet_filter.xlsx"))
    name, rows = "", []
    if p.returncode == 0 and os.path.exists(out("tc2_sheet_filter.xlsx")):
        name = sheet_name(out("tc2_sheet_filter.xlsx"))
        rows = read_xlsx(out("tc2_sheet_filter.xlsx"))
    record("TC2.3 --sheet + --where → renamed sheet with 5 rows",
           p.returncode == 0 and name == "Filtered" and len(rows) == 6,
           time.monotonic() - t,
           f"rc={p.returncode} sheet={name!r} rows={len(rows)-1 if rows else '?'}")


# ─── TC3  Unicode ─────────────────────────────────────────────────────────────

def test_TC3_unicode():
    print(f"\n{BOLD}TC3  Unicode (Cyrillic){RESET}")

    # TC3.1 — Cyrillic names survive (XLSX is always Unicode, no --cp needed)
    t = time.monotonic()
    p = run("--to-xlsx", out("cyrillic.tdtp.xml"), "--output", out("tc3_cyr.xlsx"))
    ok = False
    if p.returncode == 0 and os.path.exists(out("tc3_cyr.xlsx")):
        rows = read_xlsx(out("tc3_cyr.xlsx"))
        names = [r[1] for r in rows[1:] if len(r) > 1]
        ok = len(rows) == 4 and any("Иванов" in n for n in names)
    record("TC3.1 Cyrillic names intact, 3 rows",
           p.returncode == 0 and ok,
           time.monotonic() - t, f"rc={p.returncode} ok={ok}")

    # TC3.2 — header base names of the cyrillic table
    t = time.monotonic()
    rows = read_xlsx(out("tc3_cyr.xlsx")) if os.path.exists(out("tc3_cyr.xlsx")) else []
    ok = bool(rows) and base_names(rows[0]) == ["ID", "Name", "City", "Amount"]
    record("TC3.2 header base names ID,Name,City,Amount",
           ok, time.monotonic() - t,
           f"header={rows[0] if rows else '?'}")


# ─── TC4  Special characters ──────────────────────────────────────────────────

def test_TC4_special():
    print(f"\n{BOLD}TC4  Special characters{RESET}")

    t = time.monotonic()
    p = run("--to-xlsx", out("special.tdtp.xml"), "--output", out("tc4_special.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc4_special.xlsx")):
        rows = read_xlsx(out("tc4_special.xlsx"))
    by_id = {r[0]: r for r in rows[1:]} if len(rows) == 7 else {}

    # TC4.1 — every tricky value stays a single intact cell
    checks = [
        ("2", 1, "Semi; Colon"),
        ("3", 1, "Comma, Inc"),
        ("4", 1, "Tab\tName"),
        ("5", 1, 'Quote "Q" Name'),
    ]
    ok = p.returncode == 0 and len(rows) == 7
    bad = []
    for rid, col, want in checks:
        got = by_id.get(rid, ["?"] * 3)[col] if by_id else "?"
        if got != want:
            ok = False
            bad.append(f"id={rid} got={got!r} want={want!r}")
    record("TC4.1 semicolon/comma/tab/quote survive as single cells",
           ok, time.monotonic() - t,
           f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}" + (f" {'; '.join(bad)}" if bad else ""))

    # TC4.2 — leading '=' stays text (formula-injection guard)
    t = time.monotonic()
    got = by_id.get("6", ["?"] * 3)[1] if by_id else "?"
    record("TC4.2 leading '=' kept as text, not a formula",
           got == "=SUM(A1:A2)",
           time.monotonic() - t, f"got={got!r}")


# ─── TC5  Column projection ───────────────────────────────────────────────────

def test_TC5_fields():
    print(f"\n{BOLD}TC5  Column projection (--fields){RESET}")

    # TC5.1 — 3 of 8 columns
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "--fields", "ID,Name,Balance",
            "--output", out("tc5_3cols.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc5_3cols.xlsx")):
        rows = read_xlsx(out("tc5_3cols.xlsx"))
    ok = (p.returncode == 0
          and len(rows) == 11
          and base_names(rows[0]) == ["ID", "Name", "Balance"]
          and len(rows[1]) == 3)
    record("TC5.1 --fields ID,Name,Balance → 3 columns only",
           ok, time.monotonic() - t,
           f"rc={p.returncode} header={rows[0] if rows else '?'}")

    # TC5.2 — single column
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "--fields", "Email",
            "--output", out("tc5_email.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc5_email.xlsx")):
        rows = read_xlsx(out("tc5_email.xlsx"))
    ok = (p.returncode == 0
          and len(rows) == 11
          and base_names(rows[0]) == ["Email"]
          and "john@example.com" in [r[0] for r in rows[1:]])
    record("TC5.2 --fields Email → 1 column, 10 data rows",
           ok, time.monotonic() - t,
           f"rc={p.returncode} rows={len(rows)}")

    # TC5.3 — unknown field → non-zero exit
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "--fields", "ID,NonExistentColumn",
            "--output", out("tc5_badfield.xlsx"))
    record("TC5.3 unknown field in --fields → error exit",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode}")

    # TC5.4 — column order follows --fields, not schema order
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "--fields", "Balance,Name,ID",
            "--output", out("tc5_reorder.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc5_reorder.xlsx")):
        rows = read_xlsx(out("tc5_reorder.xlsx"))
    ok = bool(rows) and base_names(rows[0]) == ["Balance", "Name", "ID"]
    record("TC5.4 --fields Balance,Name,ID → columns in requested order",
           p.returncode == 0 and ok,
           time.monotonic() - t,
           f"rc={p.returncode} header={rows[0] if rows else '?'}")


# ─── TC6  WHERE filter ────────────────────────────────────────────────────────

def test_TC6_where():
    print(f"\n{BOLD}TC6  WHERE filter{RESET}")

    # TC6.1 — numeric condition: Balance > 1500 → IDs 2,4,6,8,10 = 5 rows
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "--where", "Balance > 1500",
            "--output", out("tc6_balance.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc6_balance.xlsx")):
        rows = read_xlsx(out("tc6_balance.xlsx"))
    ok = p.returncode == 0 and len(rows) == 6  # 1 header + 5 data
    record("TC6.1 --where Balance > 1500 → 5 rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC6.2 — string condition: City = Moscow → IDs 1,3,6,7,10 = 5 rows
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "--where", "City = Moscow",
            "--output", out("tc6_city.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc6_city.xlsx")):
        rows = read_xlsx(out("tc6_city.xlsx"))
    ok = p.returncode == 0 and len(rows) == 6
    record("TC6.2 --where City = Moscow → 5 rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC6.3 — IsActive = 0 → 2 rows (IDs 3,7)
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "--where", "IsActive = 0",
            "--output", out("tc6_inactive.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc6_inactive.xlsx")):
        rows = read_xlsx(out("tc6_inactive.xlsx"))
    ok = p.returncode == 0 and len(rows) == 3  # 1 header + 2 data
    record("TC6.3 --where IsActive = 0 → 2 rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC6.4 — repeatable --where (AND): City=Moscow AND IsActive=1 → IDs 1,6,10 = 3 rows
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "--where", "City = Moscow",
            "--where", "IsActive = 1",
            "--output", out("tc6_and.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc6_and.xlsx")):
        rows = read_xlsx(out("tc6_and.xlsx"))
    ok = p.returncode == 0 and len(rows) == 4  # 1 header + 3 data
    record("TC6.4 two --where (AND): City=Moscow AND IsActive=1 → 3 rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC6.5 — -w shorthand
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "-w", "Balance > 1500",
            "--output", out("tc6_w_short.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc6_w_short.xlsx")):
        rows = read_xlsx(out("tc6_w_short.xlsx"))
    ok = p.returncode == 0 and len(rows) == 6
    record("TC6.5 -w shorthand (alias for --where)",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC6.6 — IN operator: City IN (Moscow,SPb) → 8 rows
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "--where", "City IN (Moscow,SPb)",
            "--output", out("tc6_in.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc6_in.xlsx")):
        rows = read_xlsx(out("tc6_in.xlsx"))
    ok = p.returncode == 0 and len(rows) == 9  # 1 header + 8 data
    record("TC6.6 --where City IN (Moscow,SPb) → 8 rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC6.7 — --where may reference a column outside --fields projection
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "--fields", "ID,Name",
            "--where", "Balance > 1500",
            "--output", out("tc6_proj_where.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc6_proj_where.xlsx")):
        rows = read_xlsx(out("tc6_proj_where.xlsx"))
    ok = (p.returncode == 0 and len(rows) == 6
          and base_names(rows[0]) == ["ID", "Name"])
    record("TC6.7 --where on non-projected column → filter then project",
           ok, time.monotonic() - t,
           f"rc={p.returncode} rows={len(rows)-1 if rows else '?'} header={rows[0] if rows else '?'}")


# ─── TC7  ORDER BY ────────────────────────────────────────────────────────────

def test_TC7_orderby():
    print(f"\n{BOLD}TC7  ORDER BY{RESET}")

    # TC7.1 — sort Balance DESC → first data row has highest balance (3000)
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "--order-by", "Balance DESC",
            "--output", out("tc7_balance_desc.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc7_balance_desc.xlsx")):
        rows = read_xlsx(out("tc7_balance_desc.xlsx"))
    # Balance column is index 3; first data row = highest
    first_balance = fval(rows[1][3]) if len(rows) > 1 and rows[1][3] else 0
    ok = p.returncode == 0 and len(rows) == 11 and first_balance == 3000.0
    record("TC7.1 --order-by Balance DESC → first row = 3000 (Emma Wilson)",
           ok, time.monotonic() - t,
           f"rc={p.returncode} first_balance={first_balance}")

    # TC7.2 — sort Balance ASC → first data row has lowest balance (400)
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "--order-by", "Balance ASC",
            "--output", out("tc7_balance_asc.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc7_balance_asc.xlsx")):
        rows = read_xlsx(out("tc7_balance_asc.xlsx"))
    first_balance = fval(rows[1][3]) if len(rows) > 1 and rows[1][3] else 0
    ok = p.returncode == 0 and len(rows) == 11 and first_balance == 400.0
    record("TC7.2 --order-by Balance ASC → first row = 400 (Henry Taylor)",
           ok, time.monotonic() - t,
           f"rc={p.returncode} first_balance={first_balance}")


# ─── TC8  LIMIT & tail ────────────────────────────────────────────────────────

def test_TC8_limit():
    print(f"\n{BOLD}TC8  LIMIT (first-N and tail -N){RESET}")

    # TC8.1 — first 3
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "--limit", "3",
            "--output", out("tc8_first3.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc8_first3.xlsx")):
        rows = read_xlsx(out("tc8_first3.xlsx"))
    ok = p.returncode == 0 and len(rows) == 4  # 1 header + 3 data
    record("TC8.1 --limit 3 → 3 data rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC8.2 — tail -3 (last 3 by natural order)
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "--limit", "-3",
            "--output", out("tc8_tail3.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc8_tail3.xlsx")):
        rows = read_xlsx(out("tc8_tail3.xlsx"))
    ok = p.returncode == 0 and len(rows) == 4
    record("TC8.2 --limit -3 (tail mode) → 3 data rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC8.3 — --limit > total rows → all 10 rows (no error)
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "--limit", "999",
            "--output", out("tc8_overlimit.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc8_overlimit.xlsx")):
        rows = read_xlsx(out("tc8_overlimit.xlsx"))
    ok = p.returncode == 0 and len(rows) == 11
    record("TC8.3 --limit 999 (> total) → all 10 rows, no error",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC8.4 — filter + limit combined: Balance > 1000, limit 3 → 3 rows
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "--where", "Balance > 1000",
            "--limit", "3",
            "--output", out("tc8_filter_limit.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc8_filter_limit.xlsx")):
        rows = read_xlsx(out("tc8_filter_limit.xlsx"))
    ok = p.returncode == 0 and len(rows) == 4
    record("TC8.4 --where Balance > 1000 --limit 3 → 3 rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC8.5 — tail -1 with explicit order: last by Balance DESC = lowest (400)
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "--order-by", "Balance DESC",
            "--limit", "-1",
            "--output", out("tc8_tail_ordered.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc8_tail_ordered.xlsx")):
        rows = read_xlsx(out("tc8_tail_ordered.xlsx"))
    last_balance = fval(rows[1][3]) if len(rows) == 2 and rows[1][3] else -1
    ok = p.returncode == 0 and len(rows) == 2 and last_balance == 400.0
    record("TC8.5 --order-by DESC --limit -1 → single row, Balance = 400",
           ok, time.monotonic() - t,
           f"rc={p.returncode} balance={last_balance}")


# ─── TC9  OFFSET / pagination ─────────────────────────────────────────────────

def test_TC9_offset():
    print(f"\n{BOLD}TC9  OFFSET / pagination{RESET}")

    # TC9.1 — skip first 7 → 3 remaining rows (IDs 8,9,10)
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "--offset", "7",
            "--output", out("tc9_skip7.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc9_skip7.xlsx")):
        rows = read_xlsx(out("tc9_skip7.xlsx"))
    ok = p.returncode == 0 and len(rows) == 4  # 1 header + 3 data
    record("TC9.1 --offset 7 → 3 remaining rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC9.2 — pagination: limit 4, offset 4 → rows 5–8
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "--limit", "4", "--offset", "4",
            "--output", out("tc9_page2.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc9_page2.xlsx")):
        rows = read_xlsx(out("tc9_page2.xlsx"))
    ok = p.returncode == 0 and len(rows) == 5  # 1 header + 4 data
    record("TC9.2 --limit 4 --offset 4 → rows 5–8 (4 rows)",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC9.3 — offset >= total rows → 0 data rows (just header)
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "--offset", "100",
            "--output", out("tc9_skip_all.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc9_skip_all.xlsx")):
        rows = read_xlsx(out("tc9_skip_all.xlsx"))
    ok = p.returncode == 0 and len(rows) <= 1  # header only (or empty)
    record("TC9.3 --offset 100 (> total) → 0 data rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")


# ─── TC10  Compressed input ───────────────────────────────────────────────────

def test_TC10_compressed():
    print(f"\n{BOLD}TC10  Compressed input (zstd){RESET}")

    # TC10.1 — zstd compressed → same result as plain
    t = time.monotonic()
    p = run("--to-xlsx", out("users_zstd.tdtp.xml"),
            "--output", out("tc10_zstd.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc10_zstd.xlsx")):
        rows = read_xlsx(out("tc10_zstd.xlsx"))
    ok = p.returncode == 0 and len(rows) == 11
    record("TC10.1 zstd-compressed input → 10 rows (auto-decompress)",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC10.2 — zstd + filter
    t = time.monotonic()
    p = run("--to-xlsx", out("users_zstd.tdtp.xml"),
            "--where", "Balance > 1500",
            "--output", out("tc10_zstd_filter.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc10_zstd_filter.xlsx")):
        rows = read_xlsx(out("tc10_zstd_filter.xlsx"))
    ok = p.returncode == 0 and len(rows) == 6
    record("TC10.2 zstd + --where Balance > 1500 → 5 rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC10.3 — zstd + fields projection
    t = time.monotonic()
    p = run("--to-xlsx", out("users_zstd.tdtp.xml"),
            "--fields", "ID,Name,Balance",
            "--output", out("tc10_zstd_fields.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc10_zstd_fields.xlsx")):
        rows = read_xlsx(out("tc10_zstd_fields.xlsx"))
    ok = (p.returncode == 0
          and len(rows) == 11
          and base_names(rows[0]) == ["ID", "Name", "Balance"])
    record("TC10.3 zstd + --fields ID,Name,Balance → 3 columns",
           ok, time.monotonic() - t,
           f"rc={p.returncode} cols={len(rows[0]) if rows else '?'}")


# ─── TC11  Compact v1.3.1 ─────────────────────────────────────────────────────

def test_TC11_compact():
    print(f"\n{BOLD}TC11  Compact v1.3.1 input{RESET}")

    # TC11.1 — compact TDTP → full XLSX (all rows expanded)
    t = time.monotonic()
    p = run("--to-xlsx", out("users_compact.tdtp.xml"),
            "--output", out("tc11_compact.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc11_compact.xlsx")):
        rows = read_xlsx(out("tc11_compact.xlsx"))
    ok = p.returncode == 0 and len(rows) == 11
    record("TC11.1 compact v1.3.1 → all 10 rows expanded in XLSX",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC11.2 — compact + filter
    t = time.monotonic()
    p = run("--to-xlsx", out("users_compact.tdtp.xml"),
            "--where", "City = Moscow",
            "--output", out("tc11_compact_filter.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc11_compact_filter.xlsx")):
        rows = read_xlsx(out("tc11_compact_filter.xlsx"))
    ok = p.returncode == 0 and len(rows) == 6  # 1 header + 5 Moscow rows
    record("TC11.2 compact + --where City = Moscow → 5 rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC11.3 — compact equals plain: same first data row
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "--output", out("tc11_plain_cmp.xlsx"))
    rows_plain, rows_compact = [], []
    if p.returncode == 0 and os.path.exists(out("tc11_plain_cmp.xlsx")):
        rows_plain = read_xlsx(out("tc11_plain_cmp.xlsx"))
    if os.path.exists(out("tc11_compact.xlsx")):
        rows_compact = read_xlsx(out("tc11_compact.xlsx"))
    ok = (bool(rows_plain) and bool(rows_compact)
          and rows_plain == rows_compact)
    record("TC11.3 compact output identical to plain output",
           ok, time.monotonic() - t,
           f"plain={len(rows_plain)} compact={len(rows_compact)}")


# ─── TC12  v1.4 integrity ─────────────────────────────────────────────────────

def test_TC12_v14():
    print(f"\n{BOLD}TC12  v1.4 integrity input{RESET}")

    # TC12.1 — v1.4 plain → XLSX (hashes verified, all rows)
    t = time.monotonic()
    p = run("--to-xlsx", out("users_v14.tdtp.xml"),
            "--output", out("tc12_v14.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc12_v14.xlsx")):
        rows = read_xlsx(out("tc12_v14.xlsx"))
    ok = p.returncode == 0 and len(rows) == 11
    record("TC12.1 v1.4 integrity → hashes verified, 10 rows in XLSX",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC12.2 — v1.4 + fields + where
    t = time.monotonic()
    p = run("--to-xlsx", out("users_v14.tdtp.xml"),
            "--fields", "ID,Name,Balance",
            "--where", "Balance > 1500",
            "--output", out("tc12_v14_combo.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc12_v14_combo.xlsx")):
        rows = read_xlsx(out("tc12_v14_combo.xlsx"))
    ok = (p.returncode == 0
          and len(rows) == 6
          and base_names(rows[0]) == ["ID", "Name", "Balance"])
    record("TC12.2 v1.4 + --fields + --where → 5 rows, 3 cols",
           ok, time.monotonic() - t,
           f"rc={p.returncode} rows={len(rows)-1 if rows else '?'} cols={len(rows[0]) if rows else '?'}")


# ─── TC13  Combined query ─────────────────────────────────────────────────────

def test_TC13_combined():
    print(f"\n{BOLD}TC13  Combined query{RESET}")

    # TC13.1 — fields + where + order-by + limit + custom sheet
    # Balance > 1000 → IDs 1,2,4,6,7,8,10 = 7 rows; limit 3, sort DESC → top 3
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "--fields", "ID,Name,Balance",
            "--where", "Balance > 1000",
            "--order-by", "Balance DESC",
            "--limit", "3",
            "--sheet", "Top3",
            "--output", out("tc13_combo.xlsx"))
    rows, name = [], ""
    if p.returncode == 0 and os.path.exists(out("tc13_combo.xlsx")):
        rows = read_xlsx(out("tc13_combo.xlsx"))
        name = sheet_name(out("tc13_combo.xlsx"))
    ok = (p.returncode == 0
          and len(rows) == 4           # 1 header + 3 data
          and base_names(rows[0]) == ["ID", "Name", "Balance"]
          and fval(rows[1][2]) == 3000.0  # first = highest balance
          and name == "Top3")
    record("TC13.1 fields+where+order-by+limit+sheet → top-3 by Balance DESC",
           ok, time.monotonic() - t,
           f"rc={p.returncode} rows={len(rows)-1 if rows else '?'} "
           f"top={rows[1][2] if len(rows) > 1 else '?'} sheet={name!r}")

    # TC13.2 — orders: status=completed + fields + sort Amount DESC + limit 3
    t = time.monotonic()
    p = run("--to-xlsx", out("orders.tdtp.xml"),
            "--fields", "OrderID,ProductName,Amount",
            "--where", "Status = completed",
            "--order-by", "Amount DESC",
            "--limit", "3",
            "--output", out("tc13_orders.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc13_orders.xlsx")):
        rows = read_xlsx(out("tc13_orders.xlsx"))
    # completed orders: 1(1500), 3(600), 5(100), 8(150) → top-3: 1500,600,150
    ok = (p.returncode == 0
          and len(rows) == 4
          and base_names(rows[0]) == ["OrderID", "ProductName", "Amount"]
          and fval(rows[1][2]) == 1500.0)
    record("TC13.2 orders: status=completed + top-3 by Amount DESC",
           ok, time.monotonic() - t,
           f"rc={p.returncode} rows={len(rows)-1 if rows else '?'} top={rows[1][2] if len(rows) > 1 else '?'}")

    # TC13.3 — full pipeline on compressed input: zstd + fields + where + order + limit
    t = time.monotonic()
    p = run("--to-xlsx", out("users_zstd.tdtp.xml"),
            "--fields", "ID,Name,Balance",
            "--where", "Balance > 1000",
            "--order-by", "Balance ASC",
            "--limit", "2",
            "--output", out("tc13_zstd_combo.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc13_zstd_combo.xlsx")):
        rows = read_xlsx(out("tc13_zstd_combo.xlsx"))
    # Balance > 1000 ASC → lowest two: 1200 (Frank), 1500 (John)
    ok = (p.returncode == 0
          and len(rows) == 3
          and base_names(rows[0]) == ["ID", "Name", "Balance"]
          and fval(rows[1][2]) == 1200.0)
    record("TC13.3 zstd + fields+where+order-by+limit → 2 lowest above 1000",
           ok, time.monotonic() - t,
           f"rc={p.returncode} rows={len(rows)-1 if rows else '?'} first={rows[1][2] if len(rows) > 1 else '?'}")


# ─── TC14  -l alias ───────────────────────────────────────────────────────────

def test_TC14_l_alias():
    print(f"\n{BOLD}TC14  -l alias (shorthand for --limit){RESET}")

    # TC14.1 — -l 5 same as --limit 5
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "-l", "5",
            "--output", out("tc14_l5.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc14_l5.xlsx")):
        rows = read_xlsx(out("tc14_l5.xlsx"))
    ok = p.returncode == 0 and len(rows) == 6  # 1 header + 5 data
    record("TC14.1 -l 5 → 5 data rows (alias for --limit)",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")

    # TC14.2 — -l -3 (tail mode via -l)
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "-l", "-3",
            "--output", out("tc14_tail3.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc14_tail3.xlsx")):
        rows = read_xlsx(out("tc14_tail3.xlsx"))
    ok = p.returncode == 0 and len(rows) == 4
    record("TC14.2 -l -3 (tail mode) → 3 data rows",
           ok, time.monotonic() - t, f"rc={p.returncode} rows={len(rows)-1 if rows else '?'}")


# ─── TC15  --output flag ──────────────────────────────────────────────────────

def test_TC15_output():
    print(f"\n{BOLD}TC15  --output flag{RESET}")

    # TC15.1 — explicit output path
    target = out("tc15_explicit.xlsx")
    t = time.monotonic()
    p = run("--to-xlsx", out("users.tdtp.xml"), "--output", target)
    ok = p.returncode == 0 and os.path.exists(target)
    record("TC15.1 --output <path> → file created at exact path",
           ok, time.monotonic() - t, f"rc={p.returncode} exists={os.path.exists(target)}")

    # TC15.2 — auto-generated output name (no --output → <input>.xlsx next to source)
    t = time.monotonic()
    src = out("orders.tdtp.xml")
    auto = src + ".xlsx"
    if os.path.exists(auto):
        os.remove(auto)
    p2 = run("--to-xlsx", src)
    rows2 = read_xlsx(auto) if os.path.exists(auto) else []
    ok = p2.returncode == 0 and os.path.exists(auto) and len(rows2) == 9  # 1 header + 8 orders
    record("TC15.2 no --output → auto file created with 8 data rows",
           ok, time.monotonic() - t,
           f"rc={p2.returncode} file={Path(auto).name} rows={len(rows2)-1 if rows2 else '?'}")


# ─── TC16  Error cases ────────────────────────────────────────────────────────

def test_TC16_errors():
    print(f"\n{BOLD}TC16  Error cases{RESET}")

    # TC16.1 — missing input file → non-zero exit
    t = time.monotonic()
    p = run("--to-xlsx", "/tmp/does_not_exist_12345.tdtp.xml")
    record("TC16.1 missing input file → error exit",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode}")

    # TC16.2 — unknown field in --fields → non-zero exit + no output file
    t = time.monotonic()
    bad_out = out("tc16_bad.xlsx")
    if os.path.exists(bad_out):
        os.remove(bad_out)
    p = run("--to-xlsx", out("users.tdtp.xml"),
            "--fields", "ID,GhostColumn",
            "--output", bad_out)
    ok = p.returncode != 0 and not os.path.exists(bad_out)
    record("TC16.2 unknown --fields column → error exit, no output written",
           ok, time.monotonic() - t, f"rc={p.returncode}")


# ─── TC17  Wide table ───────────────────────────────────────────────────────

def test_TC17_wide():
    print(f"\n{BOLD}TC17  Wide table (multi-letter columns){RESET}")

    # TC17.1 — 41 columns convert intact, refs past Z stay aligned
    t = time.monotonic()
    p = run("--to-xlsx", out("wide.tdtp.xml"), "--output", out("tc17_wide.xlsx"))
    rows = []
    if p.returncode == 0 and os.path.exists(out("tc17_wide.xlsx")):
        rows = read_xlsx(out("tc17_wide.xlsx"))
    want_header = ["ID"] + [f"W{i:02d}" for i in range(1, 41)]
    ok = (p.returncode == 0 and len(rows) == 4
          and base_names(rows[0]) == want_header)
    record("TC17.1 41 columns → header + 3 rows, names in order",
           ok, time.monotonic() - t,
           f"rc={p.returncode} cols={len(rows[0]) if rows else '?'}")

    # TC17.2 — far-right cells (past Z) hold the right values, not shifted
    t = time.monotonic()
    ok = (len(rows) == 4 and rows[1][0] == "1"
          and rows[1][27] == "r1W27"   # column AB
          and rows[1][40] == "r1W40"   # last column AO
          and rows[3][40] == "r3W40")
    record("TC17.2 cells AB/AO aligned (no column shift past Z)",
           ok, time.monotonic() - t,
           f"AB={rows[1][27] if len(rows) > 1 else '?'} AO={rows[1][40] if len(rows) > 1 else '?'}")

    # TC17.3 — projection to far-right columns only
    t = time.monotonic()
    p = run("--to-xlsx", out("wide.tdtp.xml"),
            "--fields", "W39,W40",
            "--output", out("tc17_farcols.xlsx"))
    far = []
    if p.returncode == 0 and os.path.exists(out("tc17_farcols.xlsx")):
        far = read_xlsx(out("tc17_farcols.xlsx"))
    ok = (p.returncode == 0 and len(far) == 4
          and base_names(far[0]) == ["W39", "W40"]
          and far[1] == ["r1W39", "r1W40"])
    record("TC17.3 --fields W39,W40 → 2 far-right columns, values intact",
           ok, time.monotonic() - t,
           f"rc={p.returncode} header={far[0] if far else '?'}")


# ─── Runner ───────────────────────────────────────────────────────────────────

GROUPS = [
    ("TC1",  test_TC1_basic),
    ("TC2",  test_TC2_sheet),
    ("TC3",  test_TC3_unicode),
    ("TC4",  test_TC4_special),
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
    ("TC17", test_TC17_wide),
]


def preflight():
    check_binary(TDTPCLI)
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
    print(f"{BOLD}SUMMARY  --to-xlsx{RESET}")
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
