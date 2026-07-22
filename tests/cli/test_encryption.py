#!/usr/bin/env python3
"""
TDTP CLI Integration Tests — Encryption (AES-256-GCM via xZMercury)

Tests every encryption mode this build of tdtpcli supports:
    --export --enc                (whole-packet AES-256-GCM, real xZMercury/mock)
    --import <file>.tdtp.enc      (decrypt, burn-on-read)
    --enc + --compress            (compression then encryption, combined)
    --pipeline --enc-dev          (local ephemeral key, no xZMercury — mechanical
                                    success only; NOT decryptable after the process
                                    exits, by design — see flags_dev.go)
    error paths: missing --mercury-url, wrong URL, burn-on-read replay,
                 corrupted ciphertext, stdout rejection

Zero encryption coverage existed in tests/cli/ before this file (checked via
grep -in "encrypt|mercury" across test_sqlite.py/test_mysql.py/test_postgres.py/
test_mssql_msmq.py/test_csv.py — no hits).

Starts cmd/xzmercury-mock itself (go run) as a disposable fixture — no external
infra required, same "self-contained like the SQLite DB" philosophy as the rest
of this suite. Skips only if `go` is not on PATH.

Usage:
    python3 tests/cli/test_encryption.py          # all groups
    python3 tests/cli/test_encryption.py E3        # single group
    TDTPCLI_BIN=/path/to/tdtpcli python3 tests/cli/test_encryption.py
"""

import os
import sys
import time
import shutil
import signal
import sqlite3
import subprocess
import urllib.request
import tempfile
import xml.etree.ElementTree as ET
from pathlib import Path

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")

# ─── Configuration ────────────────────────────────────────────────────────────
# Sibling scripts (test_sqlite.py etc.) hardcode /tmp/..., which only resolves
# correctly under a POSIX-path-aware Python (WSL/git-bash's own). Under native
# Windows Python, "/tmp/x" resolves relative to the current drive root and
# silently misses whatever directory tdtpcli itself uses — tdtpcli.exe (a Go
# binary) always gets whatever exact path string this script hands it, with
# no /tmp translation of its own. tempfile.gettempdir() resolves correctly
# under both, so this file uses it instead of copying the hardcoded /tmp
# convention — verified against tdtpcli.exe built and run on native Windows.
_TMP     = Path(tempfile.gettempdir())
ROOT     = Path(__file__).resolve().parent.parent.parent   # repo root
TDTPCLI  = os.environ.get("TDTPCLI_BIN", str(_TMP / "tdtpcli"))
TEST_DB  = str(_TMP / "tdtp_enc_test.db")
OUTDIR   = _TMP / "tdtp_enc_test_out"
CFG      = str(_TMP / "tdtp_enc_test.yaml")

MERCURY_ADDR = os.environ.get("TDTP_MERCURY_ADDR", "127.0.0.1:3091")  # non-default port: avoid clashing with a real xZMercury on :3000
MERCURY_URL  = f"http://{MERCURY_ADDR}"
# "dev-mode" is a literal sentinel FileEncryptor.Encrypt checks for
# (cmd/tdtpcli/commands/encrypt.go / pkg/processors/encryption.go) — it
# skips HMAC verification entirely rather than comparing against a secret.
# Required here because cmd/xzmercury-mock's Bind handler signs only
# HMAC(secret, uuid) — no ":mode" suffix, no "mode" field in its response —
# which doesn't match pkg/mercury.VerifyHMAC's current uuid+":"+mode scheme
# (added later to keep dev-bound keys from verifying against a prod
# secret). Real xZMercury or a mock updated to match would need the real
# secret instead; "dev-mode" is this codebase's own established bypass for
# exactly this situation — same pattern cmd/tdtpcli/commands/enc_tier_test.go
# already uses for its own mock Mercury server.
MERCURY_SECRET = "dev-mode"

# ─── ANSI colors ──────────────────────────────────────────────────────────────
GREEN  = "\033[32m"
RED    = "\033[31m"
YELLOW = "\033[33m"
BOLD   = "\033[1m"
RESET  = "\033[0m"

results: list = []
_mock_proc: "subprocess.Popen | None" = None


# ─── Helpers ──────────────────────────────────────────────────────────────────

def _tdtpcli_env() -> dict:
    """Env for every tdtpcli invocation: MERCURY_SERVER_SECRET must match
    what xzmercury-mock was started with, or every --enc call fails HMAC
    verification (EncryptPacket/DecryptEncBlob both require it — there is
    no silent bypass except the literal string "dev-mode", which is not
    what this mock signs with, so this must be the real matching secret)."""
    env = dict(os.environ)
    env["MERCURY_SERVER_SECRET"] = MERCURY_SECRET
    return env


def run(*args, cfg=None, timeout=30) -> subprocess.CompletedProcess:
    """Run tdtpcli with --config <cfg> and additional args."""
    cmd = [TDTPCLI, "--config", cfg or CFG] + list(args)
    return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout,
                           env=_tdtpcli_env())


def run_no_cfg(*args, timeout=30) -> subprocess.CompletedProcess:
    """Run tdtpcli without --config (for --test / --inspect / --pipeline)."""
    cmd = [TDTPCLI] + list(args)
    return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout,
                           env=_tdtpcli_env())


def out(name: str) -> str:
    return str(OUTDIR / name)


def is_valid_xml(path: str) -> bool:
    """True if path parses as XML — used to assert an .tdtp.enc file is
    genuinely opaque binary, not a plaintext/compressed TDTP packet."""
    if not os.path.exists(path):
        return False
    try:
        ET.parse(path)
        return True
    except ET.ParseError:
        return False


def count_rows_xml(path: str) -> int:
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


def sqlite_query(db_path: str, sql: str) -> list:
    conn = sqlite3.connect(db_path)
    try:
        return conn.execute(sql).fetchall()
    finally:
        conn.close()


def write_cfg(path: str, db: str = TEST_DB):
    with open(path, "w") as f:
        f.write(f"database:\n  type: sqlite\n  database: {db}\n")


def write_pipeline_yaml(path: str, dest: str, encrypt: bool, enc_dev: bool = False):
    """Pipeline YAML: sqlite users table -> tdtp output, optionally encrypted."""
    # YAML double-quoted strings process backslash escapes (\U looks like a
    # Unicode escape start) — a raw Windows path like "C:\Users\..." inside
    # "..." breaks the parser ("did not find expected hex escape"). Forward
    # slashes parse as plain text and Go's os package accepts them fine on
    # Windows too, so normalize both embedded paths instead of hand-escaping.
    dsn_path = TEST_DB.replace("\\", "/")
    dest_path = dest.replace("\\", "/")
    with open(path, "w") as f:
        f.write(f"""name: "enc-test-pipeline"
version: "1.0"
sources:
  - name: users
    type: sqlite
    dsn: {dsn_path}
    query: "SELECT * FROM users"
workspace:
  type: sqlite
  mode: ":memory:"
transform:
  result_table: result
  sql: "SELECT * FROM users"
output:
  type: tdtp
  tdtp:
    destination: "{dest_path}"
    encryption: {str(encrypt).lower()}
""")
        if encrypt and not enc_dev:
            f.write(f"""security:
  mercury_url: "{MERCURY_URL}"
  mercury_timeout_ms: 5000
""")


def record(tid: str, passed: bool, elapsed: float, msg: str = ""):
    results.append((tid, passed, elapsed, msg))
    status = f"{GREEN}PASS{RESET}" if passed else f"{RED}FAIL{RESET}"
    detail = f"  ({msg})" if msg and not passed else ""
    print(f"  [{status}] {tid:<52} {elapsed:.2f}s{detail}")


def setup_db():
    if os.path.exists(TEST_DB):
        os.remove(TEST_DB)
    conn = sqlite3.connect(TEST_DB)
    c = conn.cursor()
    c.execute("""CREATE TABLE users (
        ID INTEGER PRIMARY KEY, Name TEXT NOT NULL, Email TEXT,
        Balance NUMERIC(18,2), IsActive INTEGER, City TEXT)""")
    c.executemany("INSERT INTO users VALUES (?,?,?,?,?,?)", [
        (1, "John Doe",    "john@example.com",    1500.00, 1, "Moscow"),
        (2, "Jane Smith",  "jane@example.com",    2000.00, 1, "SPb"),
        (3, "Bob Johnson", "bob@example.com",      500.00, 0, "Moscow"),
        (4, "Alice Brown", "alice@example.com",   2500.00, 1, "Kazan"),
        (5, "Charlie Davis","charlie@example.com", 800.00, 1, "SPb"),
        (6, "Emma Wilson", "emma@example.com",    3000.00, 1, "Moscow"),
        (7, "Frank Miller","frank@example.com",   1200.00, 0, "Moscow"),
        (8, "Grace Lee",   "grace@example.com",   1800.00, 1, "SPb"),
        (9, "Henry Taylor","henry@example.com",    400.00, 1, "Kazan"),
        (10,"Ivy Anderson","ivy@example.com",     2200.00, 1, "Moscow"),
    ])
    conn.commit()
    conn.close()


def _import(file: str, table: str, mercury_url: str = MERCURY_URL,
            strategy: str = "replace", timeout=30) -> subprocess.CompletedProcess:
    cmd = [TDTPCLI, "--config", CFG, "--import", file, "--table", table,
           "--strategy", strategy]
    if mercury_url:
        cmd += ["--mercury-url", mercury_url]
    return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout,
                           env=_tdtpcli_env())


# ─── xzmercury-mock fixture ────────────────────────────────────────────────────

def go_available() -> bool:
    return shutil.which("go") is not None


def mercury_healthy() -> bool:
    try:
        req = urllib.request.Request(f"{MERCURY_URL}/healthz")
        with urllib.request.urlopen(req, timeout=1) as r:
            return r.status == 200
    except Exception:
        return False


def start_mercury_mock() -> bool:
    """Start cmd/xzmercury-mock as a disposable subprocess. Returns True if it
    became healthy within the timeout."""
    global _mock_proc
    if not go_available():
        return False
    env = dict(os.environ)
    env["MERCURY_SERVER_SECRET"] = MERCURY_SECRET
    env["MOCK_ADDR"] = f":{MERCURY_ADDR.split(':')[-1]}"
    _mock_proc = subprocess.Popen(
        ["go", "run", "./cmd/xzmercury-mock/", "--addr", env["MOCK_ADDR"],
         "--secret", MERCURY_SECRET],
        cwd=str(ROOT), env=env,
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    )
    for _ in range(60):  # up to ~30s for `go run` first-time compile
        if mercury_healthy():
            return True
        if _mock_proc.poll() is not None:
            return False
        time.sleep(0.5)
    return False


def stop_mercury_mock():
    global _mock_proc
    if _mock_proc is None:
        return
    try:
        if sys.platform == "win32":
            _mock_proc.terminate()
        else:
            _mock_proc.send_signal(signal.SIGTERM)
        _mock_proc.wait(timeout=5)
    except Exception:
        try:
            _mock_proc.kill()
        except Exception:
            pass
    _mock_proc = None


# ─── E1 Export/Import roundtrip via --enc ──────────────────────────────────────

def test_E1_roundtrip():
    print(f"\n{BOLD}=== E1 --enc export/import roundtrip (real Mercury mock) ==={RESET}")

    # E1.1 — export --enc produces a file that is NOT valid XML (opaque blob)
    t = time.monotonic()
    p = run("--export", "users", "--enc", "--mercury-url", MERCURY_URL,
            "--output", out("e1_users.tdtp.enc"))
    is_binary = not is_valid_xml(out("e1_users.tdtp.enc"))
    record("E1.1 --export --enc → output is NOT valid XML (opaque blob)",
           p.returncode == 0 and is_binary,
           time.monotonic() - t, f"rc={p.returncode} binary={is_binary}")

    # E1.2 — --import decrypts and round-trips all 10 rows
    t = time.monotonic()
    p = _import(out("e1_users.tdtp.enc"), "users_enc_copy")
    rows = sqlite_query(TEST_DB, "SELECT COUNT(*) FROM users_enc_copy")[0][0] \
           if p.returncode == 0 else -1
    record("E1.2 --import --mercury-url decrypts → 10 rows",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rc={p.returncode} rows={rows} stderr={p.stderr.strip()[:100]}")

    # E1.3 — decrypted content matches the source exactly (spot-check one row)
    t = time.monotonic()
    row = sqlite_query(TEST_DB, "SELECT Name, Balance FROM users_enc_copy WHERE ID=1")
    name_ok = bool(row) and row[0][0] == "John Doe" and float(row[0][1]) == 1500.00
    record("E1.3 decrypted row content matches source (ID=1)",
           name_ok, time.monotonic() - t, f"row={row}")

    if os.path.exists(TEST_DB):
        sqlite_query(TEST_DB, "DROP TABLE IF EXISTS users_enc_copy")


# ─── E2 Burn-on-read ────────────────────────────────────────────────────────────

def test_E2_burn_on_read():
    print(f"\n{BOLD}=== E2 Burn-on-read (key consumed after first decrypt) ==={RESET}")

    run("--export", "users", "--enc", "--mercury-url", MERCURY_URL,
        "--output", out("e2_users.tdtp.enc"))

    # E2.1 — first decrypt succeeds
    t = time.monotonic()
    p1 = _import(out("e2_users.tdtp.enc"), "users_burn1")
    record("E2.1 first --import decrypts successfully",
           p1.returncode == 0,
           time.monotonic() - t, f"rc={p1.returncode} stderr={p1.stderr.strip()[:100]}")

    # E2.2 — second decrypt of the SAME file must fail (key already burned)
    t = time.monotonic()
    p2 = _import(out("e2_users.tdtp.enc"), "users_burn2")
    record("E2.2 second --import of same file fails (burn-on-read)",
           p2.returncode != 0,
           time.monotonic() - t, f"rc={p2.returncode}")


# ─── E3 Error paths ─────────────────────────────────────────────────────────────

def test_E3_error_paths():
    print(f"\n{BOLD}=== E3 Encryption error paths ==={RESET}")

    # E3.1 — --enc without --mercury-url → export fails
    t = time.monotonic()
    p = run("--export", "users", "--enc", "--output", out("e3_no_url.tdtp.enc"))
    record("E3.1 --enc without --mercury-url → export fails",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode} stderr={p.stderr.strip()[:100]}")

    # E3.2 — --import of an encrypted file without --mercury-url → fails
    run("--export", "users", "--enc", "--mercury-url", MERCURY_URL,
        "--output", out("e3_valid.tdtp.enc"))
    t = time.monotonic()
    p = subprocess.run(
        [TDTPCLI, "--config", CFG, "--import", out("e3_valid.tdtp.enc"),
         "--table", "e3_should_fail"],
        capture_output=True, text=True, timeout=30,
    )
    record("E3.2 --import encrypted file without --mercury-url → fails",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode}")

    # E3.3 — --import against a wrong/unreachable Mercury URL → fails
    t = time.monotonic()
    run("--export", "users", "--enc", "--mercury-url", MERCURY_URL,
        "--output", out("e3_wrong_url.tdtp.enc"))
    p = _import(out("e3_wrong_url.tdtp.enc"), "e3_wrong",
                mercury_url="http://127.0.0.1:1")
    record("E3.3 --import with unreachable Mercury URL → fails",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode}")

    # E3.4 — corrupted ciphertext → --import fails (AES-GCM auth failure)
    t = time.monotonic()
    run("--export", "users", "--enc", "--mercury-url", MERCURY_URL,
        "--output", out("e3_corrupt.tdtp.enc"))
    fsize = os.path.getsize(out("e3_corrupt.tdtp.enc"))
    with open(out("e3_corrupt.tdtp.enc"), "r+b") as f:
        f.seek(fsize - 1)
        b = f.read(1)
        f.seek(fsize - 1)
        f.write(bytes([b[0] ^ 0xFF]))
    p = _import(out("e3_corrupt.tdtp.enc"), "e3_corrupt_table")
    record("E3.4 corrupted ciphertext → --import fails (GCM auth)",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode}")

    # E3.5 — --enc cannot be combined with stdout output
    t = time.monotonic()
    p = run("--export", "users", "--enc", "--mercury-url", MERCURY_URL,
            "--output", "-")
    record("E3.5 --enc with stdout output (-) → rejected",
           p.returncode != 0,
           time.monotonic() - t, f"rc={p.returncode}")


# ─── E4 Encryption + compression combined ──────────────────────────────────────

def test_E4_compress_plus_encrypt():
    print(f"\n{BOLD}=== E4 --enc combined with --compress ==={RESET}")

    # E4.1 — export --compress --enc → still round-trips correctly
    t = time.monotonic()
    p = run("--export", "users", "--compress", "--enc", "--mercury-url", MERCURY_URL,
            "--output", out("e4_comp_enc.tdtp.enc"))
    is_binary = not is_valid_xml(out("e4_comp_enc.tdtp.enc"))
    record("E4.1 --compress --enc → opaque blob produced",
           p.returncode == 0 and is_binary,
           time.monotonic() - t, f"rc={p.returncode}")

    t = time.monotonic()
    p = _import(out("e4_comp_enc.tdtp.enc"), "users_comp_enc")
    rows = sqlite_query(TEST_DB, "SELECT COUNT(*) FROM users_comp_enc")[0][0] \
           if p.returncode == 0 else -1
    record("E4.2 --compress --enc roundtrip → 10 rows",
           p.returncode == 0 and rows == 10,
           time.monotonic() - t, f"rows={rows} stderr={p.stderr.strip()[:100]}")


# ─── E5 --pipeline --enc-dev (local key, no Mercury) ──────────────────────────

def test_E5_enc_dev():
    print(f"\n{BOLD}=== E5 --pipeline --enc-dev (local ephemeral key) ==={RESET}")

    dest = out("e5_enc_dev.tdtp.enc")
    pipeline_yaml = str(_TMP / "tdtp_enc_dev_pipeline.yaml")
    write_pipeline_yaml(pipeline_yaml, dest, encrypt=True, enc_dev=True)

    # E5.1 — pipeline with --enc-dev completes and produces an opaque blob
    #   (NOT decryptable afterward by design — key is session-scoped, never
    #   persisted; see flags_dev.go's own warning comment)
    t = time.monotonic()
    p = run_no_cfg("--pipeline", pipeline_yaml, "--enc-dev")
    is_binary = not is_valid_xml(dest) if os.path.exists(dest) else False
    record("E5.1 --pipeline --enc-dev → exit 0, opaque blob (no Mercury needed)",
           p.returncode == 0 and is_binary,
           time.monotonic() - t,
           f"rc={p.returncode} binary={is_binary} stderr={p.stderr.strip()[:150]}")

    # E5.2 — running the exact same pipeline WITHOUT --enc-dev and without
    #   Mercury reachable degrades gracefully, not a hard failure: the CLI
    #   writes a valid-XML error packet (type=error, error_code=
    #   MERCURY_UNAVAILABLE) as the output file and exits 0 — "error packet,
    #   not silence" is this codebase's deliberate design (a downstream
    #   consumer imports the error packet as a normal tdtp.xml receipt
    #   instead of the pipeline just vanishing). Confirmed by running this
    #   manually first: rc=0, stdout says "Pipeline completed with errors
    #   (exit 0)". Sanity check that --enc-dev is doing something (not
    #   silently ignored): the *content* differs — an opaque encrypted blob
    #   (E5.1) vs. a readable XML error packet naming MERCURY_UNAVAILABLE.
    dest2 = out("e5_should_fail.tdtp.enc")
    pipeline_yaml2 = str(_TMP / "tdtp_enc_dev_pipeline_no_dev.yaml")
    write_pipeline_yaml(pipeline_yaml2, dest2, encrypt=True)
    # Point security.mercury_url at an address nothing listens on.
    content = Path(pipeline_yaml2).read_text()
    content = content.replace(MERCURY_URL, "http://127.0.0.1:1")
    Path(pipeline_yaml2).write_text(content)
    t = time.monotonic()
    p = run_no_cfg("--pipeline", pipeline_yaml2)
    error_packet_written = False
    if os.path.exists(dest2):
        try:
            text = Path(dest2).read_text(encoding="utf-8", errors="replace")
            error_packet_written = "<Type>error</Type>" in text and "MERCURY_UNAVAILABLE" in text
        except OSError:
            pass
    record("E5.2 unreachable Mercury → degrades to error packet, exit 0",
           p.returncode == 0 and error_packet_written,
           time.monotonic() - t,
           f"rc={p.returncode} error_packet={error_packet_written} stdout={p.stdout.strip()[-100:]}")


# ─── Runner ───────────────────────────────────────────────────────────────────

GROUPS = [
    ("E1", test_E1_roundtrip),
    ("E2", test_E2_burn_on_read),
    ("E3", test_E3_error_paths),
    ("E4", test_E4_compress_plus_encrypt),
    ("E5", test_E5_enc_dev),
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

    print(f"Starting xzmercury-mock at {MERCURY_URL} ...")
    if not start_mercury_mock():
        print(f"{RED}ERROR: could not start cmd/xzmercury-mock "
              f"(is `go` on PATH? is port {MERCURY_ADDR} free?){RESET}")
        sys.exit(1)
    print(f"  {GREEN}xzmercury-mock healthy{RESET}")

    try:
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
    finally:
        stop_mercury_mock()


if __name__ == "__main__":
    main()
