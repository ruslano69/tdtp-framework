#!/usr/bin/env python3
"""
libtdtp — J_ParseBytes / D_ParseBytes integration tests

Tests that both connectors can parse a TDTP packet from a raw byte buffer
without touching the filesystem (broker consumer scenario).

Usage:
    python3 tests/libtdtp/test_parsebytes.py          # all tests
    python3 tests/libtdtp/test_parsebytes.py T1       # single test
    LIBTDTP_SO=/path/to/libtdtp.so python3 ...        # custom .so path
"""

import ctypes
import json
import os
import subprocess
import sys
import time
from pathlib import Path

# Force UTF-8 output
if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")

# ─── Configuration ────────────────────────────────────────────────────────────
ROOT       = Path(__file__).resolve().parent.parent.parent
LIBTDTP_DIR = ROOT / "pkg" / "python" / "libtdtp"
SO_PATH    = Path(os.environ.get("LIBTDTP_SO", "/tmp/libtdtp_test.so"))

# ─── ANSI colors ──────────────────────────────────────────────────────────────
GREEN  = "\033[32m"
RED    = "\033[31m"
YELLOW = "\033[33m"
BOLD   = "\033[1m"
RESET  = "\033[0m"

results: list = []  # (tid, passed, elapsed, msg)

# ─── Sample TDTP XML ──────────────────────────────────────────────────────────
SAMPLE_XML = b"""\
<?xml version="1.0" encoding="utf-8"?>
<DataPacket protocol="TDTP" version="1.0">
  <Header>
    <Type>reference</Type>
    <TableName>Users</TableName>
    <MessageID>TEST-001</MessageID>
    <PartNumber>1</PartNumber>
    <TotalParts>1</TotalParts>
    <RecordsInPart>2</RecordsInPart>
    <Timestamp>2026-01-01T00:00:00Z</Timestamp>
  </Header>
  <Schema>
    <Field name="id"    type="INTEGER" key="true"/>
    <Field name="name"  type="TEXT"    length="100"/>
    <Field name="score" type="DECIMAL"/>
  </Schema>
  <Data>
    <R>1|Alice|99.5</R>
    <R>2|Bob|42.0</R>
  </Data>
</DataPacket>"""

INVALID_XML = b"<not valid xml"

# ─── ctypes structures (mirrors tdtp_structs.h) ───────────────────────────────

class D_Field(ctypes.Structure):
    _fields_ = [
        ("name",       ctypes.c_char * 256),
        ("type_name",  ctypes.c_char * 64),
        ("length",     ctypes.c_int),
        ("precision",  ctypes.c_int),
        ("scale",      ctypes.c_int),
        ("is_key",     ctypes.c_int),
        ("is_readonly",ctypes.c_int),
    ]

class D_Schema(ctypes.Structure):
    _fields_ = [
        ("fields",      ctypes.POINTER(D_Field)),
        ("field_count", ctypes.c_int),
    ]

class D_Row(ctypes.Structure):
    _fields_ = [
        ("values",      ctypes.POINTER(ctypes.c_char_p)),
        ("value_count", ctypes.c_int),
    ]

class D_Packet(ctypes.Structure):
    _fields_ = [
        ("rows",           ctypes.POINTER(D_Row)),
        ("row_count",      ctypes.c_int),
        ("schema",         D_Schema),
        ("msg_type",       ctypes.c_char * 32),
        ("table_name",     ctypes.c_char * 256),
        ("message_id",     ctypes.c_char * 64),
        ("timestamp_unix", ctypes.c_longlong),
        ("compression",    ctypes.c_char * 16),
        ("error",          ctypes.c_char * 1024),
    ]

# ─── Build and load .so ───────────────────────────────────────────────────────

def build_so() -> bool:
    """Build libtdtp.so with -tags compress. Returns True on success."""
    print(f"  Building {SO_PATH} …", end=" ", flush=True)
    r = subprocess.run(
        ["go", "build", "-tags", "compress", "-buildmode=c-shared", "-o", str(SO_PATH)],
        cwd=str(LIBTDTP_DIR),
        capture_output=True, text=True,
        env={**os.environ, "GOWORK": "off", "GOPROXY": "https://goproxy.io", "GONOSUMDB": "*"},
    )
    if r.returncode != 0:
        print(f"{RED}FAILED{RESET}")
        print(r.stderr[:800])
        return False
    print(f"{GREEN}OK{RESET}")
    return True


def load_so(path: Path) -> ctypes.CDLL:
    lib = ctypes.CDLL(str(path))

    # J_ParseBytes(data *C.char, length C.int) *C.char
    # restype=c_void_p preserves the raw pointer so J_FreeString can release it.
    lib.J_ParseBytes.argtypes = [ctypes.c_char_p, ctypes.c_int]
    lib.J_ParseBytes.restype  = ctypes.c_void_p

    # J_FreeString(*C.char)
    lib.J_FreeString.argtypes = [ctypes.c_void_p]
    lib.J_FreeString.restype  = None

    # D_ParseBytes(data *C.char, length C.int, out *D_Packet) C.int
    lib.D_ParseBytes.argtypes = [ctypes.c_char_p, ctypes.c_int, ctypes.POINTER(D_Packet)]
    lib.D_ParseBytes.restype  = ctypes.c_int

    # D_FreePacket(*D_Packet)
    lib.D_FreePacket.argtypes = [ctypes.POINTER(D_Packet)]
    lib.D_FreePacket.restype  = None

    return lib

# ─── Test runner ─────────────────────────────────────────────────────────────

def record(tid: str, passed: bool, elapsed: float, msg: str = "") -> None:
    results.append((tid, passed, elapsed, msg))
    icon = f"{GREEN}✓{RESET}" if passed else f"{RED}✗{RESET}"
    label = f"{BOLD}{tid}{RESET}"
    extra = f"  {YELLOW}{msg}{RESET}" if msg else ""
    print(f"  {icon} {label} ({elapsed*1000:.0f} ms){extra}")


def run_test(tid: str, fn) -> None:
    t0 = time.perf_counter()
    try:
        ok, msg = fn()
    except Exception as e:
        ok, msg = False, str(e)
    record(tid, ok, time.perf_counter() - t0, msg)

# ─── Tests ───────────────────────────────────────────────────────────────────

def j_call(lib, xml_bytes: bytes) -> dict:
    """Call J_ParseBytes, decode JSON, free the pointer, return dict."""
    ptr = lib.J_ParseBytes(xml_bytes, len(xml_bytes))
    result = json.loads(ctypes.string_at(ptr))
    lib.J_FreeString(ptr)
    return result


def t1_j_parsebytes_schema(lib) -> tuple[bool, str]:
    """J_ParseBytes: correct schema (3 fields, names and types)."""
    data = j_call(lib, SAMPLE_XML)
    if "error" in data:
        return False, data["error"]

    fields = data["schema"]["Fields"]
    if len(fields) != 3:
        return False, f"expected 3 fields, got {len(fields)}"
    names = [f["Name"] for f in fields]
    if names != ["id", "name", "score"]:
        return False, f"unexpected field names: {names}"
    if fields[0]["Type"] != "INTEGER":
        return False, f"id type: {fields[0]['Type']}"
    return True, ""


def t2_j_parsebytes_data(lib) -> tuple[bool, str]:
    """J_ParseBytes: correct row count and values."""
    data = j_call(lib, SAMPLE_XML)
    if "error" in data:
        return False, data["error"]

    rows = data["data"]
    if len(rows) != 2:
        return False, f"expected 2 rows, got {len(rows)}"
    if rows[0][0] != "1" or rows[0][1] != "Alice":
        return False, f"unexpected row[0]: {rows[0]}"
    if rows[1][1] != "Bob":
        return False, f"unexpected row[1]: {rows[1]}"
    return True, ""


def t3_j_parsebytes_header(lib) -> tuple[bool, str]:
    """J_ParseBytes: header fields (table_name, message_id, type)."""
    data = j_call(lib, SAMPLE_XML)
    h = data.get("header", {})
    if h.get("table_name") != "Users":
        return False, f"table_name: {h.get('table_name')}"
    if h.get("message_id") != "TEST-001":
        return False, f"message_id: {h.get('message_id')}"
    if h.get("type") != "reference":
        return False, f"type: {h.get('type')}"
    return True, ""


def t4_j_parsebytes_invalid(lib) -> tuple[bool, str]:
    """J_ParseBytes: invalid XML returns error JSON."""
    data = j_call(lib, INVALID_XML)
    if "error" not in data:
        return False, "expected error field in response"
    return True, ""


def t5_d_parsebytes_schema(lib) -> tuple[bool, str]:
    """D_ParseBytes: correct field_count and field names."""
    pkt = D_Packet()
    rc = lib.D_ParseBytes(SAMPLE_XML, len(SAMPLE_XML), ctypes.byref(pkt))
    if rc != 0:
        return False, pkt.error.decode()

    if pkt.schema.field_count != 3:
        lib.D_FreePacket(ctypes.byref(pkt))
        return False, f"field_count: {pkt.schema.field_count}"

    fields = [pkt.schema.fields[i].name.decode() for i in range(pkt.schema.field_count)]
    lib.D_FreePacket(ctypes.byref(pkt))

    if fields != ["id", "name", "score"]:
        return False, f"fields: {fields}"
    return True, ""


def t6_d_parsebytes_rows(lib) -> tuple[bool, str]:
    """D_ParseBytes: correct row_count and cell values."""
    pkt = D_Packet()
    rc = lib.D_ParseBytes(SAMPLE_XML, len(SAMPLE_XML), ctypes.byref(pkt))
    if rc != 0:
        return False, pkt.error.decode()

    if pkt.row_count != 2:
        lib.D_FreePacket(ctypes.byref(pkt))
        return False, f"row_count: {pkt.row_count}"

    row0 = [pkt.rows[0].values[j].decode() for j in range(pkt.rows[0].value_count)]
    row1 = [pkt.rows[1].values[j].decode() for j in range(pkt.rows[1].value_count)]
    lib.D_FreePacket(ctypes.byref(pkt))

    if row0 != ["1", "Alice", "99.5"]:
        return False, f"row0: {row0}"
    if row1[1] != "Bob":
        return False, f"row1: {row1}"
    return True, ""


def t7_d_parsebytes_header(lib) -> tuple[bool, str]:
    """D_ParseBytes: table_name and msg_type populated."""
    pkt = D_Packet()
    rc = lib.D_ParseBytes(SAMPLE_XML, len(SAMPLE_XML), ctypes.byref(pkt))
    if rc != 0:
        return False, pkt.error.decode()

    table = pkt.table_name.decode()
    mtype = pkt.msg_type.decode()
    lib.D_FreePacket(ctypes.byref(pkt))

    if table != "Users":
        return False, f"table_name: {table}"
    if mtype != "reference":
        return False, f"msg_type: {mtype}"
    return True, ""


def t8_d_parsebytes_invalid(lib) -> tuple[bool, str]:
    """D_ParseBytes: invalid XML returns rc=1 with error message."""
    pkt = D_Packet()
    rc = lib.D_ParseBytes(INVALID_XML, len(INVALID_XML), ctypes.byref(pkt))
    if rc == 0:
        lib.D_FreePacket(ctypes.byref(pkt))
        return False, "expected rc=1 for invalid XML"
    if not pkt.error.decode():
        return False, "expected non-empty error message"
    return True, ""


TESTS = {
    "T1": ("J_ParseBytes schema",       t1_j_parsebytes_schema),
    "T2": ("J_ParseBytes data rows",    t2_j_parsebytes_data),
    "T3": ("J_ParseBytes header",       t3_j_parsebytes_header),
    "T4": ("J_ParseBytes invalid XML",  t4_j_parsebytes_invalid),
    "T5": ("D_ParseBytes schema",       t5_d_parsebytes_schema),
    "T6": ("D_ParseBytes data rows",    t6_d_parsebytes_rows),
    "T7": ("D_ParseBytes header",       t7_d_parsebytes_header),
    "T8": ("D_ParseBytes invalid XML",  t8_d_parsebytes_invalid),
}

# ─── Main ─────────────────────────────────────────────────────────────────────

def main() -> None:
    filter_ids = set(a.upper() for a in sys.argv[1:])

    print(f"\n{BOLD}=== libtdtp J_ParseBytes / D_ParseBytes ==={RESET}\n")

    # Build if .so is missing or stale
    if not SO_PATH.exists():
        if not build_so():
            sys.exit(1)

    lib = load_so(SO_PATH)

    for tid, (desc, fn) in TESTS.items():
        if filter_ids and tid not in filter_ids:
            continue
        print(f"  {BOLD}{tid}{RESET} {desc}")
        run_test(tid, lambda f=fn: f(lib))

    # Summary
    total  = len(results)
    passed = sum(1 for _, ok, *_ in results if ok)
    failed = total - passed
    print(f"\n  {'─'*40}")
    if failed == 0:
        print(f"  {GREEN}{BOLD}All {total} tests passed{RESET}")
    else:
        print(f"  {RED}{BOLD}{failed}/{total} tests FAILED{RESET}")
        for tid, ok, _, msg in results:
            if not ok:
                print(f"    {RED}✗ {tid}: {msg}{RESET}")
    print()
    sys.exit(0 if failed == 0 else 1)


if __name__ == "__main__":
    main()
