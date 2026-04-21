#!/usr/bin/env python3
"""
TDTP CLI Integration Tests — MS SQL Server (Windows Auth) + MSMQ

Primary scenario: Axapta 2009 / Dynamics NAV stack on ZTR-Live.
Tests cover the full pipeline:
    MSSQL (WinAuth) → tdtpcli → file / MSMQ → SQLite roundtrip

All tests operate on a temporary table `tdtp_test_emp` that is created
before the run and dropped after — no production data is touched.

Prerequisites (Windows, domain zt-2075):
    - SQL Server reachable at MSSQL_HOST with Windows Authentication
    - pyodbc + ODBC Driver 17 for SQL Server installed
    - MSMQ service running  (Get-Service MSMQ → Running)
    - Queue MSMQ_QUEUE exists (.\private$\tdtp_test)
    - tdtpcli binary built: go build -o /tmp/tdtpcli.exe ./cmd/tdtpcli/

Usage:
    python tests/cli/test_mssql_msmq.py          # all groups
    python tests/cli/test_mssql_msmq.py T2        # single group
    TDTPCLI_BIN=/path/to/tdtpcli.exe python tests/cli/test_mssql_msmq.py

Environment overrides:
    TDTPCLI_BIN       path to tdtpcli binary  (default: /tmp/tdtpcli)
    MSSQL_HOST        SQL Server host          (default: sql-srv1)
    MSSQL_PORT        SQL Server port          (default: 1433)
    MSSQL_DB          database name            (default: ZTR-Live)
    TDTP_MSMQ_QUEUE   MSMQ queue path          (default: .\\private$\\tdtp_test)
"""

import os
import sys
import time
import subprocess
import xml.etree.ElementTree as ET
from pathlib import Path

# Force UTF-8 output on Windows cp1251 terminals
if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")

# ─── Configuration ────────────────────────────────────────────────────────────
TDTPCLI    = os.environ.get("TDTPCLI_BIN", "/tmp/tdtpcli")
OUTDIR     = Path("/tmp/tdtp_mssql_test_out")
CFG        = "/tmp/tdtp_mssql_test.yaml"
CFG_C      = "/tmp/tdtp_mssql_compress.yaml"
CFG_IMP    = "/tmp/tdtp_mssql_import.yaml"

MSSQL_HOST = os.environ.get("MSSQL_HOST", "sql-srv1")
MSSQL_PORT = int(os.environ.get("MSSQL_PORT", "1433"))
MSSQL_DB   = os.environ.get("MSSQL_DB",   "ZTR-Live")

# Temporary test table — created in setup, dropped in teardown
TEST_TABLE  = "tdtp_test_emp"
TABLE_ROWS  = 10      # total fixture rows
ACTIVE_ROWS = 3       # rows with Status=0
TABLE_COLS  = 7       # number of columns in test table schema
KEY_FIELDS  = "No_,Last Name,First Name,Status,Employment Date"

MSMQ_QUEUE = os.environ.get("TDTP_MSMQ_QUEUE", r".\private$\tdtp_test")

# ─── ANSI colors ──────────────────────────────────────────────────────────────
GREEN  = "\033[32m"
RED    = "\033[31m"
YELLOW = "\033[33m"
BOLD   = "\033[1m"
RESET  = "\033[0m"

results: list = []

# ─── Fixture data ─────────────────────────────────────────────────────────────
# 10 rows: EMP001–EMP003 active (Status=0), EMP004–EMP010 inactive (Status=1)
FIXTURE_ROWS = [
    ("EMP001", "Ivanov",   "Ivan",    0, "2020-01-15", "IT",      "Developer"),
    ("EMP002", "Petrov",   "Petr",    0, "2019-06-01", "HR",      "Manager"),
    ("EMP003", "Sidorov",  "Alexey",  0, "2021-03-10", "Finance", "Analyst"),
    ("EMP004", "Kozlov",   "Dmitry",  1, "2015-08-20", "IT",      "Architect"),
    ("EMP005", "Smirnov",  "Pavel",   1, "2018-11-05", "Sales",   "Director"),
    ("EMP006", "Volkov",   "Nikolay", 1, "2017-02-28", "IT",      "DevOps"),
    ("EMP007", "Novikov",  "Sergey",  1, "2016-07-14", "HR",      "Specialist"),
    ("EMP008", "Sokolov",  "Anton",   1, "2014-12-01", "Finance", "Director"),
    ("EMP009", "Morozov",  "Viktor",  1, "2013-05-15", "Sales",   "Manager"),
    ("EMP010", "Lebedev",  "Andrey",  1, "2012-09-30", "IT",      "Tester"),
]

# ─── Helpers ──────────────────────────────────────────────────────────────────

def run(*args, cfg=None, timeout=60) -> subprocess.CompletedProcess:
    cmd = [TDTPCLI, "--config", cfg or CFG] + list(args)
    return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)


def run_no_cfg(*args, timeout=30) -> subprocess.CompletedProcess:
    return subprocess.run([TDTPCLI] + list(args),
                         capture_output=True, text=True, timeout=timeout)


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


def out(name: str) -> str:
    return str(OUTDIR / name)


def record(tid: str, passed: bool, elapsed: float, msg: str = ""):
    results.append((tid, passed, elapsed, msg))
    status = f"{GREEN}PASS{RESET}" if passed else f"{RED}FAIL{RESET}"
    detail = f"  ({msg})" if msg and not passed else ""
    print(f"  [{status}] {tid:<58} {elapsed:.2f}s{detail}")


# ─── pyodbc helpers ───────────────────────────────────────────────────────────

def _pyodbc_connect():
    """Return a pyodbc connection using Windows Auth (SSPI)."""
    import pyodbc  # imported lazily — only on Windows with pyodbc installed
    drivers = [d for d in pyodbc.drivers()
               if "SQL Server" in d and ("17" in d or "18" in d or "ODBC" in d)]
    driver = drivers[0] if drivers else "ODBC Driver 17 for SQL Server"
    conn_str = (
        f"DRIVER={{{driver}}};"
        f"SERVER={MSSQL_HOST},{MSSQL_PORT};"
        f"DATABASE={MSSQL_DB};"
        "Trusted_Connection=yes;"
        "TrustServerCertificate=yes;"
    )
    return pyodbc.connect(conn_str, timeout=10)


def setup_mssql() -> bool:
    """
    Create temporary test table and insert fixture data.
    Returns True on success, False on error.
    """
    try:
        conn = _pyodbc_connect()
        cur = conn.cursor()
        # Drop if leftover from a previous failed run
        cur.execute(f"""
            IF OBJECT_ID(N'{TEST_TABLE}', N'U') IS NOT NULL
                DROP TABLE [{TEST_TABLE}]
        """)
        cur.execute(f"""
            CREATE TABLE [{TEST_TABLE}] (
                [No_]             NVARCHAR(20)  NOT NULL PRIMARY KEY,
                [Last Name]       NVARCHAR(50),
                [First Name]      NVARCHAR(50),
                [Status]          INT           NOT NULL DEFAULT 0,
                [Employment Date] DATETIME,
                [Department]      NVARCHAR(50),
                [Position]        NVARCHAR(100)
            )
        """)
        for row in FIXTURE_ROWS:
            cur.execute(
                f"INSERT INTO [{TEST_TABLE}] VALUES (?,?,?,?,?,?,?)",
                row,
            )
        conn.commit()
        conn.close()
        return True
    except Exception as exc:
        print(f"{RED}setup_mssql failed: {exc}{RESET}")
        return False


def teardown_mssql():
    """Drop the temporary test table."""
    try:
        conn = _pyodbc_connect()
        cur = conn.cursor()
        cur.execute(f"""
            IF OBJECT_ID(N'{TEST_TABLE}', N'U') IS NOT NULL
                DROP TABLE [{TEST_TABLE}]
        """)
        conn.commit()
        conn.close()
    except Exception as exc:
        print(f"{YELLOW}teardown_mssql warning: {exc}{RESET}")


# ─── Availability checks ──────────────────────────────────────────────────────

def mssql_available() -> bool:
    """Return True if SQL Server is reachable via Windows Auth."""
    if sys.platform != "win32":
        return False
    try:
        conn = _pyodbc_connect()
        conn.close()
        return True
    except Exception:
        return False


def pyodbc_available() -> bool:
    """Return True if pyodbc module is importable."""
    try:
        import pyodbc  # noqa: F401
        return True
    except ImportError:
        return False


def msmq_available() -> bool:
    """Return True if MSMQ service is running (Windows-only)."""
    if sys.platform != "win32":
        return False
    try:
        p = subprocess.run(
            ["powershell", "-NoProfile", "-Command",
             "(Get-Service -Name MSMQ -ErrorAction SilentlyContinue).Status"],
            capture_output=True, text=True, timeout=5,
        )
        return p.stdout.strip() == "Running"
    except Exception:
        return False


# ─── Config writers ───────────────────────────────────────────────────────────

def write_mssql_cfg(path: str, compress: bool = False,
                    algo: str = "zstd", level: int = 3):
    with open(path, "w", encoding="utf-8") as f:
        f.write("database:\n")
        f.write("  type: mssql\n")
        f.write(f"  host: {MSSQL_HOST}\n")
        f.write(f"  port: {MSSQL_PORT}\n")
        f.write(f"  database: \"{MSSQL_DB}\"\n")
        f.write("  schema: dbo\n")
        f.write("  windows_auth: true\n")
        f.write("  sslmode: disable\n")
        f.write("export:\n")
        f.write(f"  compress: {str(compress).lower()}\n")
        f.write(f"  compress_algo: {algo}\n")
        f.write(f"  compress_level: {level}\n")


def write_sqlite_cfg(path: str, db: str):
    with open(path, "w", encoding="utf-8") as f:
        f.write(f"database:\n  type: sqlite\n  database: {db}\n")


def write_msmq_cfg(path: str, db_path: str = "/tmp/no.db",
                   queue: str = MSMQ_QUEUE, mssql: bool = False,
                   compress: bool = False):
    """Config with MSSQL or SQLite source + MSMQ broker."""
    queue_yaml = queue.replace("\\", "\\\\")
    with open(path, "w", encoding="utf-8") as f:
        if mssql:
            f.write("database:\n")
            f.write("  type: mssql\n")
            f.write(f"  host: {MSSQL_HOST}\n")
            f.write(f"  port: {MSSQL_PORT}\n")
            f.write(f"  database: \"{MSSQL_DB}\"\n")
            f.write("  schema: dbo\n")
            f.write("  windows_auth: true\n")
            f.write("  sslmode: disable\n")
        else:
            f.write(f"database:\n  type: sqlite\n  database: {db_path}\n")
        f.write(f"export:\n  compress: {str(compress).lower()}\n")
        f.write("broker:\n  type: msmq\n")
        f.write(f"  queue_path: \"{queue_yaml}\"\n")


# ─── T1 MSSQL Basic Export ────────────────────────────────────────────────────

def test_T1_basic():
    print(f"\n{BOLD}=== T1 MSSQL Basic Export ==={RESET}")

    write_mssql_cfg(CFG)
    OUTDIR.mkdir(parents=True, exist_ok=True)

    # T1.1 — export test table → correct row count
    t = time.monotonic()
    p = run("--export", TEST_TABLE, "--output", out("test_emp.tdtp.xml"))
    rows = count_rows_xml(out("test_emp.tdtp.xml"))
    record("T1.1 export test table → correct row count",
           p.returncode == 0 and rows == TABLE_ROWS,
           time.monotonic() - t, f"rows={rows} expected={TABLE_ROWS}")

    # T1.2 — schema has expected number of fields
    t = time.monotonic()
    fields = get_schema_fields(out("test_emp.tdtp.xml"))
    record(f"T1.2 schema has {TABLE_COLS} fields",
           len(fields) == TABLE_COLS,
           time.monotonic() - t, f"got={len(fields)}")

    # T1.3 — key field No_ present in schema
    t = time.monotonic()
    record("T1.3 key field No_ present in schema",
           "No_" in fields,
           time.monotonic() - t, f"fields={fields[:5]}")

    # T1.4 — --inspect shows correct table name
    t = time.monotonic()
    pi = run_no_cfg("--inspect", out("test_emp.tdtp.xml"))
    record(f"T1.4 --inspect shows table name '{TEST_TABLE}'",
           TEST_TABLE in pi.stdout,
           time.monotonic() - t, pi.stderr[:80] if TEST_TABLE not in pi.stdout else "")

    # T1.5 — --test without --hash → exit 0
    t = time.monotonic()
    pt = run_no_cfg("--test", out("test_emp.tdtp.xml"))
    record("T1.5 --test (no hash) → exit 0",
           pt.returncode == 0,
           time.monotonic() - t, pt.stderr[:80] if pt.returncode != 0 else "")


# ─── T2 MSSQL Filters ────────────────────────────────────────────────────────

def test_T2_filters():
    print(f"\n{BOLD}=== T2 MSSQL Filters ==={RESET}")

    write_mssql_cfg(CFG)

    # T2.1 — --limit 5
    t = time.monotonic()
    p = run("--export", TEST_TABLE, "--limit", "5",
            "--output", out("test_limit5.tdtp.xml"))
    rows = count_rows_xml(out("test_limit5.tdtp.xml"))
    record("T2.1 --limit 5 → 5 rows",
           p.returncode == 0 and rows == 5,
           time.monotonic() - t, f"rows={rows}")

    # T2.2 — --where Status=0 → active employees only (3 rows)
    t = time.monotonic()
    p = run("--export", TEST_TABLE, "--where", "Status=0",
            "--output", out("test_active.tdtp.xml"))
    rows = count_rows_xml(out("test_active.tdtp.xml"))
    record(f"T2.2 --where Status=0 → {ACTIVE_ROWS} active rows",
           p.returncode == 0 and rows == ACTIVE_ROWS,
           time.monotonic() - t, f"rows={rows} expected={ACTIVE_ROWS}")

    # T2.3 — --fields projection → schema has exactly 5 fields
    t = time.monotonic()
    p = run("--export", TEST_TABLE, "--fields", KEY_FIELDS,
            "--limit", "5", "--output", out("test_fields.tdtp.xml"))
    fields = get_schema_fields(out("test_fields.tdtp.xml"))
    record("T2.3 --fields 5 columns → schema has exactly 5 fields",
           p.returncode == 0 and len(fields) == 5,
           time.monotonic() - t, f"fields={len(fields)}")

    # T2.4 — --offset 5 --limit 3 → 3 rows
    t = time.monotonic()
    p = run("--export", TEST_TABLE, "--offset", "5", "--limit", "3",
            "--output", out("test_offset.tdtp.xml"))
    rows = count_rows_xml(out("test_offset.tdtp.xml"))
    record("T2.4 --offset 5 --limit 3 → 3 rows",
           p.returncode == 0 and rows == 3,
           time.monotonic() - t, f"rows={rows}")

    # T2.5 — --where Status=1 → inactive employees (TABLE_ROWS - ACTIVE_ROWS)
    t = time.monotonic()
    inactive = TABLE_ROWS - ACTIVE_ROWS
    p = run("--export", TEST_TABLE, "--where", "Status=1",
            "--output", out("test_inactive.tdtp.xml"))
    rows = count_rows_xml(out("test_inactive.tdtp.xml"))
    record(f"T2.5 --where Status=1 → {inactive} inactive rows",
           p.returncode == 0 and rows == inactive,
           time.monotonic() - t, f"rows={rows} expected={inactive}")


# ─── T3 Compression ──────────────────────────────────────────────────────────

def test_T3_compression():
    print(f"\n{BOLD}=== T3 Compression ==={RESET}")

    write_mssql_cfg(CFG)
    write_mssql_cfg(CFG_C, compress=True, algo="zstd", level=3)

    # T3.1 — compressed file smaller than plain
    t = time.monotonic()
    p_plain = run("--export", TEST_TABLE, "--output", out("test_plain.tdtp.xml"))
    p_comp  = run("--export", TEST_TABLE, "--compress",
                  "--output", out("test_comp.tdtp.xml"))
    size_plain = os.path.getsize(out("test_plain.tdtp.xml")) \
                 if os.path.exists(out("test_plain.tdtp.xml")) else 0
    size_comp  = os.path.getsize(out("test_comp.tdtp.xml")) \
                 if os.path.exists(out("test_comp.tdtp.xml")) else 0
    record("T3.1 compressed file smaller than plain",
           p_comp.returncode == 0 and 0 < size_comp < size_plain,
           time.monotonic() - t,
           f"plain={size_plain}B comp={size_comp}B")

    # T3.2 — --hash → --test checksum OK
    t = time.monotonic()
    p  = run("--export", TEST_TABLE, "--compress", "--hash",
             "--output", out("test_hash.tdtp.xml"))
    pt = run_no_cfg("--test", out("test_hash.tdtp.xml"))
    record("T3.2 --compress --hash → --test checksum OK",
           p.returncode == 0 and pt.returncode == 0 and "checksum OK" in pt.stdout,
           time.monotonic() - t, f"test_rc={pt.returncode}")

    # T3.3 — kanzi compression → --inspect shows kanzi
    t = time.monotonic()
    write_mssql_cfg(CFG_C, compress=True, algo="kanzi", level=6)
    p  = run("--export", TEST_TABLE, "--compress",
             "--output", out("test_kanzi.tdtp.xml"), cfg=CFG_C)
    pi = run_no_cfg("--inspect", out("test_kanzi.tdtp.xml"))
    record("T3.3 kanzi compress → --inspect shows kanzi",
           p.returncode == 0 and "kanzi" in pi.stdout,
           time.monotonic() - t, pi.stderr[:80] if p.returncode != 0 else "")

    # T3.4 — compressed export row count matches (via --test header)
    t = time.monotonic()
    rows = count_rows_xml(out("test_hash.tdtp.xml"))
    record("T3.4 compressed export → row count in header matches",
           rows == TABLE_ROWS,
           time.monotonic() - t, f"rows={rows} expected={TABLE_ROWS}")


# ─── T4 MSSQL → SQLite Roundtrip ─────────────────────────────────────────────

def test_T4_roundtrip():
    print(f"\n{BOLD}=== T4 MSSQL → SQLite Roundtrip ==={RESET}")

    write_mssql_cfg(CFG)

    # T4.1 — export plain → import → row count matches
    t = time.monotonic()
    imp_db = "/tmp/tdtp_mssql_imp1.db"
    _clean(imp_db)
    p_exp = run("--export", TEST_TABLE, "--output", out("test_rt.tdtp.xml"))
    write_sqlite_cfg(CFG_IMP, imp_db)
    p_imp = subprocess.run(
        [TDTPCLI, "--config", CFG_IMP,
         "--import", out("test_rt.tdtp.xml"), "--table", "TestEmp"],
        capture_output=True, text=True, timeout=30,
    )
    rows = _sqlite_count(imp_db, "TestEmp")
    record("T4.1 export plain → import → row count matches",
           p_exp.returncode == 0 and p_imp.returncode == 0 and rows == TABLE_ROWS,
           time.monotonic() - t, f"rows={rows} expected={TABLE_ROWS}")
    _clean(imp_db)

    # T4.2 — export --compress → import → row count matches
    t = time.monotonic()
    imp_db2 = "/tmp/tdtp_mssql_imp2.db"
    _clean(imp_db2)
    p_exp = run("--export", TEST_TABLE, "--compress",
                "--output", out("test_rt_c.tdtp.xml"))
    write_sqlite_cfg(CFG_IMP, imp_db2)
    p_imp = subprocess.run(
        [TDTPCLI, "--config", CFG_IMP,
         "--import", out("test_rt_c.tdtp.xml"), "--table", "TestEmp"],
        capture_output=True, text=True, timeout=30,
    )
    rows2 = _sqlite_count(imp_db2, "TestEmp")
    record("T4.2 export --compress → import → row count matches",
           p_exp.returncode == 0 and p_imp.returncode == 0 and rows2 == TABLE_ROWS,
           time.monotonic() - t, f"rows={rows2}")
    _clean(imp_db2)

    # T4.3 — import strategy=upsert → idempotent (import twice, same count)
    t = time.monotonic()
    imp_db3 = "/tmp/tdtp_mssql_imp3.db"
    _clean(imp_db3)
    write_sqlite_cfg(CFG_IMP, imp_db3)
    for _ in range(2):
        subprocess.run(
            [TDTPCLI, "--config", CFG_IMP,
             "--import", out("test_rt.tdtp.xml"),
             "--table", "TestEmp", "--strategy", "upsert"],
            capture_output=True, text=True, timeout=30,
        )
    rows3 = _sqlite_count(imp_db3, "TestEmp")
    record("T4.3 upsert twice → idempotent (same row count)",
           rows3 == TABLE_ROWS,
           time.monotonic() - t, f"rows={rows3} expected={TABLE_ROWS}")
    _clean(imp_db3)

    # T4.4 — --where Status=0 export → import → only active rows
    t = time.monotonic()
    imp_db4 = "/tmp/tdtp_mssql_imp4.db"
    _clean(imp_db4)
    p_exp = run("--export", TEST_TABLE, "--where", "Status=0",
                "--output", out("test_rt_active.tdtp.xml"))
    write_sqlite_cfg(CFG_IMP, imp_db4)
    p_imp = subprocess.run(
        [TDTPCLI, "--config", CFG_IMP,
         "--import", out("test_rt_active.tdtp.xml"), "--table", "TestEmpActive"],
        capture_output=True, text=True, timeout=30,
    )
    rows4 = _sqlite_count(imp_db4, "TestEmpActive")
    record(f"T4.4 export --where Status=0 → import → {ACTIVE_ROWS} rows",
           p_exp.returncode == 0 and p_imp.returncode == 0 and rows4 == ACTIVE_ROWS,
           time.monotonic() - t, f"rows={rows4} expected={ACTIVE_ROWS}")
    _clean(imp_db4)


# ─── T5 MSSQL → MSMQ → SQLite ────────────────────────────────────────────────

def test_T5_msmq_pipeline():
    print(f"\n{BOLD}=== T5 MSSQL → MSMQ → SQLite (Axapta pipeline) ==={RESET}")

    if not msmq_available():
        print(f"  {YELLOW}SKIP{RESET} MSMQ service not running")
        return

    # T5.1 — export-broker → MSMQ → exit 0
    t = time.monotonic()
    write_msmq_cfg(CFG, mssql=True)
    p = run("--export-broker", TEST_TABLE, cfg=CFG, timeout=60)
    record("T5.1 MSSQL export-broker test table → exit 0",
           p.returncode == 0,
           time.monotonic() - t,
           p.stderr.strip()[:120] if p.returncode != 0 else "")

    # T5.2 — import-broker MSMQ → SQLite → row count matches
    t = time.monotonic()
    imp_db = "/tmp/tdtp_msmq_mssql_imp.db"
    _clean(imp_db)
    write_msmq_cfg(CFG_IMP, db_path=imp_db)
    p_imp = subprocess.run(
        [TDTPCLI, "--config", CFG_IMP,
         "--import-broker", "--table", "TestEmp"],
        capture_output=True, text=True, timeout=30,
    )
    rows = _sqlite_count(imp_db, "TestEmp")
    record("T5.2 import-broker MSMQ → SQLite → row count matches",
           p_imp.returncode == 0 and rows == TABLE_ROWS,
           time.monotonic() - t, f"rows={rows} expected={TABLE_ROWS}")
    _clean(imp_db)

    # T5.3 — export-broker --compress → import-broker → row count matches
    t = time.monotonic()
    imp_db2 = "/tmp/tdtp_msmq_mssql_imp2.db"
    _clean(imp_db2)
    write_msmq_cfg(CFG, mssql=True, compress=True)
    p_exp = run("--export-broker", TEST_TABLE, cfg=CFG, timeout=60)
    write_msmq_cfg(CFG_IMP, db_path=imp_db2)
    p_imp = subprocess.run(
        [TDTPCLI, "--config", CFG_IMP,
         "--import-broker", "--table", "TestEmp"],
        capture_output=True, text=True, timeout=30,
    )
    rows2 = _sqlite_count(imp_db2, "TestEmp")
    record("T5.3 export-broker --compress → import-broker → rows match",
           p_exp.returncode == 0 and p_imp.returncode == 0 and rows2 == TABLE_ROWS,
           time.monotonic() - t, f"rows={rows2}")
    _clean(imp_db2)

    # T5.4 — export-broker --fields → import-broker → only projected fields
    t = time.monotonic()
    imp_db3 = "/tmp/tdtp_msmq_mssql_imp3.db"
    _clean(imp_db3)
    write_msmq_cfg(CFG, mssql=True)
    p_exp = run("--export-broker", TEST_TABLE, "--fields", KEY_FIELDS,
                cfg=CFG, timeout=60)
    write_msmq_cfg(CFG_IMP, db_path=imp_db3)
    p_imp = subprocess.run(
        [TDTPCLI, "--config", CFG_IMP,
         "--import-broker", "--table", "TestEmpFields"],
        capture_output=True, text=True, timeout=30,
    )
    rows3 = _sqlite_count(imp_db3, "TestEmpFields")
    cols  = _sqlite_cols(imp_db3, "TestEmpFields")
    record("T5.4 export-broker --fields 5 → import-broker → 5 columns",
           p_exp.returncode == 0 and p_imp.returncode == 0
           and rows3 == TABLE_ROWS and len(cols) == 5,
           time.monotonic() - t, f"rows={rows3} cols={len(cols)}")
    _clean(imp_db3)

    # T5.5 — export-broker --where Status=0 → import-broker → active rows only
    t = time.monotonic()
    imp_db4 = "/tmp/tdtp_msmq_mssql_imp4.db"
    _clean(imp_db4)
    write_msmq_cfg(CFG, mssql=True)
    p_exp = run("--export-broker", TEST_TABLE, "--where", "Status=0",
                cfg=CFG, timeout=60)
    write_msmq_cfg(CFG_IMP, db_path=imp_db4)
    p_imp = subprocess.run(
        [TDTPCLI, "--config", CFG_IMP,
         "--import-broker", "--table", "TestEmpActive"],
        capture_output=True, text=True, timeout=30,
    )
    rows4 = _sqlite_count(imp_db4, "TestEmpActive")
    record(f"T5.5 export-broker --where Status=0 → {ACTIVE_ROWS} active rows",
           p_exp.returncode == 0 and p_imp.returncode == 0 and rows4 == ACTIVE_ROWS,
           time.monotonic() - t, f"rows={rows4} expected={ACTIVE_ROWS}")
    _clean(imp_db4)


# ─── Utilities ────────────────────────────────────────────────────────────────

def _clean(path: str):
    if os.path.exists(path):
        os.remove(path)


def _sqlite_count(db: str, table: str) -> int:
    """Return row count from SQLite table, -1 on error."""
    if not os.path.exists(db):
        return -1
    try:
        import sqlite3
        with sqlite3.connect(db) as conn:
            return conn.execute(f'SELECT COUNT(*) FROM "{table}"').fetchone()[0]
    except Exception:
        return -1


def _sqlite_cols(db: str, table: str) -> list:
    """Return column names from SQLite table."""
    if not os.path.exists(db):
        return []
    try:
        import sqlite3
        with sqlite3.connect(db) as conn:
            cur = conn.execute(f'SELECT * FROM "{table}" LIMIT 0')
            return [d[0] for d in cur.description]
    except Exception:
        return []


# ─── Runner ───────────────────────────────────────────────────────────────────

GROUPS = [
    ("T1", test_T1_basic),
    ("T2", test_T2_filters),
    ("T3", test_T3_compression),
    ("T4", test_T4_roundtrip),
    ("T5", test_T5_msmq_pipeline),
]


def preflight():
    if not os.path.exists(TDTPCLI):
        print(f"{RED}ERROR: tdtpcli binary not found at {TDTPCLI}{RESET}")
        print("Build: GOPROXY=https://goproxy.io GONOSUMDB='*' "
              "go build -o /tmp/tdtpcli.exe ./cmd/tdtpcli/")
        sys.exit(1)
    if sys.platform != "win32":
        print(f"{RED}ERROR: MSSQL (Windows Auth) and MSMQ require Windows{RESET}")
        sys.exit(1)
    if not pyodbc_available():
        print(f"{RED}ERROR: pyodbc not installed{RESET}")
        print("Install: pip install pyodbc")
        sys.exit(1)
    if not mssql_available():
        print(f"{RED}ERROR: MSSQL not reachable at {MSSQL_HOST}:{MSSQL_PORT} "
              f"db={MSSQL_DB} (Windows Auth){RESET}")
        print("Check: sql-srv1 is up, you are on the domain, "
              "Windows Authentication is enabled")
        sys.exit(1)
    OUTDIR.mkdir(parents=True, exist_ok=True)
    ver = subprocess.run([TDTPCLI, "--version"],
                         capture_output=True, text=True).stdout.strip()
    print(f"tdtpcli: {ver}")
    print(f"MSSQL:   {MSSQL_HOST}:{MSSQL_PORT}/{MSSQL_DB} (Windows Auth)")
    print(f"MSMQ:    {MSMQ_QUEUE}  "
          f"{'(running)' if msmq_available() else '(not running — T5 will skip)'}")
    print(f"Table:   {TEST_TABLE}  "
          f"({TABLE_ROWS} rows fixture, {ACTIVE_ROWS} active)  [TEMPORARY — auto-drop]")


def main():
    requested = set(sys.argv[1:]) if len(sys.argv) > 1 else None
    preflight()

    print(f"\n{BOLD}Setting up test data in MSSQL...{RESET}")
    if not setup_mssql():
        print(f"{RED}ERROR: could not create test table {TEST_TABLE}{RESET}")
        sys.exit(1)
    print(f"  {GREEN}Created [{TEST_TABLE}] with {TABLE_ROWS} fixture rows{RESET}")

    try:
        for name, fn in GROUPS:
            if requested is None or name in requested:
                fn()
    finally:
        print(f"\n{BOLD}Tearing down test data...{RESET}")
        teardown_mssql()
        print(f"  {GREEN}Dropped [{TEST_TABLE}]{RESET}")

    print(f"\n{BOLD}{'=' * 58}{RESET}")
    print(f"{BOLD}SUMMARY{RESET}")
    passed = sum(1 for _, ok, _, _ in results if ok)
    total  = len(results)
    color  = GREEN if passed == total else RED
    print(f"  {color}PASSED: {passed} / {total}{RESET}")
    duration = sum(e for _, _, e, _ in results)
    failed = [(tid, msg) for tid, ok, _, msg in results if not ok]
    if failed:
        print(f"  {RED}DURATION: {duration:.1f}s{RESET}")
        print(f"\n  Failed tests:")
        for tid, msg in failed:
            print(f"    {RED}✗ {tid}{RESET}  {msg}")
    else:
        print(f"  DURATION: {duration:.1f}s")
    print(f"{'=' * 58}")
    sys.exit(0 if passed == total else 1)


if __name__ == "__main__":
    main()
