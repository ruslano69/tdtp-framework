#!/usr/bin/env python3
"""
TDTP CLI Integration Tests — Audit Logger Database Sink

Covers audit.database, the config surface added to let AuditConfig write to
its own SQL sink (cmd/tdtpcli/production.go newAuditDatabaseAppender) instead
of only file/console. Zero e2e coverage existed for this before — AuditConfig
was config.yaml-driven but AuditDatabaseConfig didn't exist at all, and
pkg/audit.DatabaseAppender was reachable only via pkg/audit's own Go unit
tests (never through the CLI).

This suite's job is proving the CLI wiring end-to-end (config.yaml ->
initAuditLogger -> DatabaseAppender -> real SQLite file), NOT re-proving the
DatabaseAppender's own SQL/batching logic — that's pkg/audit/logger_test.go's
job, and the two bugs found there (generateID() colliding under time.Now()
resolution in a tight loop; flushBatch() never clearing its queue on
failure, so one bad batch permanently wedged every later flush) are Go-level
races within a single process. Each tdtpcli invocation here is its own OS
process logging exactly one audit entry (cmd/tdtpcli/main.go's single
LogWithMetadata call site), which doesn't reliably reproduce that specific
race — A1/A2 instead check the invariants that fix must uphold no matter how
it's exercised: every run's entry actually lands, and every ID is distinct.

A3 covers a third, CLI-specific bug this feature's own multi-process nature
introduces and that neither the Go unit tests nor A1/A2 can reach: SQLite
only allows one writer at a time, and multiple tdtpcli processes started
together race each other's CREATE TABLE / INSERT — see
newAuditDatabaseAppender's busy_timeout/WAL comment for the fix and why
pragma order mattered.

Usage:
    python3 tests/cli/test_audit_database.py          # all groups
    python3 tests/cli/test_audit_database.py A1        # single group
    TDTPCLI_BIN=/path/to/tdtpcli python3 tests/cli/test_audit_database.py
"""

import os
import sys
import time
import sqlite3
import subprocess
import tempfile
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")

# ─── Configuration ────────────────────────────────────────────────────────────
# tempfile.gettempdir() resolves correctly under both native Windows Python
# and POSIX — hardcoded "/tmp/..." (the convention in sibling scripts like
# test_sqlite.py) only resolves correctly under a POSIX-path-aware Python
# (WSL/git-bash's own), same issue documented in test_encryption.py.
_TMP     = Path(tempfile.gettempdir())
TDTPCLI  = os.environ.get("TDTPCLI_BIN", str(_TMP / "tdtpcli"))
TEST_DB  = str(_TMP / "tdtp_audit_test.db")
AUDIT_DB = str(_TMP / "tdtp_audit_log_test.db")
OUTDIR   = _TMP / "tdtp_audit_test_out"
CFG      = str(_TMP / "tdtp_audit_test.yaml")

# ─── ANSI colors ──────────────────────────────────────────────────────────────
GREEN  = "\033[32m"
RED    = "\033[31m"
BOLD   = "\033[1m"
RESET  = "\033[0m"

results: list = []


# ─── Helpers ──────────────────────────────────────────────────────────────────

def run(*args, timeout=30) -> subprocess.CompletedProcess:
    cmd = [TDTPCLI, "--config", CFG] + list(args)
    return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)


def out(name: str) -> str:
    return str(OUTDIR / name)


def audit_query(sql: str, params: tuple = ()) -> list:
    conn = sqlite3.connect(AUDIT_DB)
    try:
        return conn.execute(sql, params).fetchall()
    finally:
        conn.close()


def write_cfg(path: str, batch_size: int):
    # Forward slashes: YAML double-quoted strings process backslash escapes,
    # and a raw Windows path like "C:\Users\..." breaks the parser ("did not
    # find expected hex escape") — same fix as test_encryption.py.
    db_path = TEST_DB.replace("\\", "/")
    audit_db_path = AUDIT_DB.replace("\\", "/")
    with open(path, "w") as f:
        f.write(f"""database:
  type: sqlite
  database: {db_path}
audit:
  enabled: true
  level: standard
  database:
    type: sqlite
    dsn: {audit_db_path}
    table: audit_log
    batch_size: {batch_size}
    auto_create_table: true
""")


def record(tid: str, passed: bool, elapsed: float, msg: str = ""):
    results.append((tid, passed, elapsed, msg))
    status = f"{GREEN}PASS{RESET}" if passed else f"{RED}FAIL{RESET}"
    detail = f"  ({msg})" if msg and not passed else ""
    print(f"  [{status}] {tid:<62} {elapsed:.2f}s{detail}")


def setup_db():
    if os.path.exists(TEST_DB):
        os.remove(TEST_DB)
    conn = sqlite3.connect(TEST_DB)
    c = conn.cursor()
    c.execute("CREATE TABLE users (ID INTEGER PRIMARY KEY, Name TEXT NOT NULL)")
    c.executemany("INSERT INTO users VALUES (?,?)",
                  [(1, "John Doe"), (2, "Jane Smith")])
    conn.commit()
    conn.close()


# ─── A1 Basic wiring: config.yaml -> CLI -> real audit DB ──────────────────────

def test_A1_wiring():
    print(f"\n{BOLD}=== A1 audit.database config wiring (batch_size=5) ==={RESET}")

    if os.path.exists(AUDIT_DB):
        os.remove(AUDIT_DB)
    write_cfg(CFG, batch_size=5)

    n_runs = 12  # > 2x batch_size: forces two auto-flushes plus a final flush-on-Close
    t = time.monotonic()
    rcs = []
    for i in range(n_runs):
        p = run("--export", "users", "--output", out(f"a1_run_{i}.tdtp.xml"))
        rcs.append(p.returncode)
    all_ok = all(rc == 0 for rc in rcs)
    record("A1.1 all export runs exit 0",
           all_ok, time.monotonic() - t, f"returncodes={rcs}")

    t = time.monotonic()
    count = audit_query("SELECT COUNT(*) FROM audit_log")[0][0] if os.path.exists(AUDIT_DB) else -1
    record(f"A1.2 audit_log has exactly {n_runs} rows (one per CLI invocation)",
           count == n_runs, time.monotonic() - t, f"count={count}")

    t = time.monotonic()
    distinct = audit_query("SELECT COUNT(DISTINCT id) FROM audit_log")[0][0] if os.path.exists(AUDIT_DB) else -1
    record("A1.3 every audit_log.id is distinct (regression: generateID() collision)",
           distinct == count, time.monotonic() - t, f"distinct={distinct} count={count}")

    t = time.monotonic()
    rows = audit_query("SELECT operation, status FROM audit_log")
    all_success_exports = all(op == "export" and status == "success" for op, status in rows)
    record("A1.4 every row is operation=export status=success",
           all_success_exports, time.monotonic() - t, f"sample={rows[:3]}")


# ─── A2 Failure path also lands in the audit DB ────────────────────────────────

def test_A2_failure_path():
    print(f"\n{BOLD}=== A2 failed command still logs to audit.database ==={RESET}")

    if os.path.exists(AUDIT_DB):
        os.remove(AUDIT_DB)
    write_cfg(CFG, batch_size=0)  # no batching: direct insert, isolates this from A1's batch timing

    t = time.monotonic()
    p = run("--export", "does_not_exist_table", "--output", out("a2_fail.tdtp.xml"))
    record("A2.1 export of a nonexistent table fails (rc != 0)",
           p.returncode != 0, time.monotonic() - t, f"rc={p.returncode}")

    t = time.monotonic()
    rows = audit_query("SELECT status FROM audit_log") if os.path.exists(AUDIT_DB) else []
    has_failure_row = any(status == "failure" for (status,) in rows)
    record("A2.2 audit_log recorded a failure row",
           has_failure_row, time.monotonic() - t, f"statuses={rows}")


# ─── A3 Concurrent processes writing to the same audit DB ─────────────────────

def test_A3_concurrent_writers():
    print(f"\n{BOLD}=== A3 concurrent tdtpcli processes, same audit DB (SQLITE_BUSY regression) ==={RESET}")
    # Found by hand while reviewing this feature: SQLite allows only one
    # writer at a time. Without a busy_timeout, N tdtpcli processes started
    # together race both AutoCreateTable's CREATE TABLE IF NOT EXISTS and
    # each other's insert transaction, and several fail immediately with
    # "database is locked (5) (SQLITE_BUSY)" instead of waiting. First fix
    # attempt (WAL + busy_timeout, WAL applied first) still failed ~1-in-8
    # bursts — switching journal_mode itself takes the write lock, and
    # busy_timeout wasn't active yet to make THAT statement wait. Fixed by
    # setting busy_timeout before journal_mode. Verified by hand across 5
    # bursts of 8 before writing this as a permanent regression test.

    if os.path.exists(AUDIT_DB):
        os.remove(AUDIT_DB)
    write_cfg(CFG, batch_size=1)

    n_procs = 8

    def one_run(i: int) -> subprocess.CompletedProcess:
        return run("--export", "users", "--output", out(f"a3_run_{i}.tdtp.xml"))

    t = time.monotonic()
    with ThreadPoolExecutor(max_workers=n_procs) as pool:
        procs = list(pool.map(one_run, range(n_procs)))
    rcs = [p.returncode for p in procs]
    stderrs = [p.stderr.strip() for p in procs if p.returncode != 0]
    record(f"A3.1 {n_procs} concurrent tdtpcli processes all exit 0 (no SQLITE_BUSY)",
           all(rc == 0 for rc in rcs), time.monotonic() - t,
           f"returncodes={rcs} first_error={stderrs[0][:150] if stderrs else ''}")

    t = time.monotonic()
    count = audit_query("SELECT COUNT(*) FROM audit_log")[0][0] if os.path.exists(AUDIT_DB) else -1
    distinct = audit_query("SELECT COUNT(DISTINCT id) FROM audit_log")[0][0] if os.path.exists(AUDIT_DB) else -1
    record(f"A3.2 audit_log has exactly {n_procs} distinct rows despite concurrent writers",
           count == n_procs and distinct == n_procs, time.monotonic() - t,
           f"count={count} distinct={distinct}")


# ─── Runner ───────────────────────────────────────────────────────────────────

GROUPS = [
    ("A1", test_A1_wiring),
    ("A2", test_A2_failure_path),
    ("A3", test_A3_concurrent_writers),
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
    print(f"Audit database: {AUDIT_DB}")
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
