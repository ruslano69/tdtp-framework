#!/usr/bin/env python3
"""Export all tables from a Microsoft Access (.mdb) database to TDTP XML.

Requirements:
  - Windows OS
  - 32-bit ODBC driver: Microsoft Access Driver (*.mdb) — ships with Windows
  - tdtpcli_x86.exe built with GOARCH=386 (see pkg/adapters/access/README.md)
  - Python 3.x

Usage:
  Place this script next to tdtpcli_x86.exe, the .mdb file and the .mda workgroup file.
  Then run:
    python export_access_db.py

  All tables are discovered automatically via --list and exported to ./tdtp_export/.

  To target a different database, edit the variables at the top of this file.
"""
import subprocess, sys, os, time, tempfile

# ── Configure paths ────────────────────────────────────────────────────────
SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))

TDTPCLI  = os.path.join(SCRIPT_DIR, "tdtpcli_x86.exe")  # 32-bit binary
MDB_PATH = os.path.join(SCRIPT_DIR, "database.mdb")      # Access database
MDA_PATH = os.path.join(SCRIPT_DIR, "system.mda")        # workgroup file (optional)
UID      = "Admin"                                         # Access username
PWD      = ""                                              # Access password
CHARSET  = "windows-1251"                                  # source encoding; "" for UTF-8
OUT_DIR  = os.path.join(SCRIPT_DIR, "tdtp_export")
# ───────────────────────────────────────────────────────────────────────────


def check_paths():
    errors = []
    if not os.path.exists(TDTPCLI):
        errors.append(f"  missing: {TDTPCLI}")
    if not os.path.exists(MDB_PATH):
        errors.append(f"  missing: {MDB_PATH}")
    if errors:
        print("ERROR — required files not found:")
        for e in errors:
            print(e)
        print("\nPlace all files next to this script or edit the path variables above.")
        sys.exit(1)


def make_temp_config():
    mdb = MDB_PATH.replace(chr(92), chr(92) * 2)
    mda = MDA_PATH.replace(chr(92), chr(92) * 2)
    has_mda = os.path.exists(MDA_PATH)

    dsn = f"Driver={{Microsoft Access Driver (*.mdb)}};DBQ={mdb};"
    if has_mda:
        dsn += f"SystemDB={mda};"
    dsn += f"UID={UID};PWD={PWD};"

    content = f"""database:
  type: access
  dsn: "{dsn}"
  charset: {CHARSET}
export:
  compress: false
"""
    tmp = tempfile.NamedTemporaryFile(mode="w", suffix=".yaml",
                                     delete=False, encoding="utf-8")
    tmp.write(content)
    tmp.close()
    return tmp.name


def get_tables(cfg):
    """Discover all user tables via --list."""
    res = subprocess.run([TDTPCLI, "--config", cfg, "--list"], capture_output=True)
    tables = []
    for line in res.stdout.decode("utf-8", errors="replace").splitlines():
        line = line.strip()
        if ". " in line:
            tables.append(line.split(". ", 1)[1].strip())
    return tables


def main():
    check_paths()
    os.makedirs(OUT_DIR, exist_ok=True)

    cfg = make_temp_config()
    try:
        tables = get_tables(cfg)
        if not tables:
            print("ERROR: --list returned no tables. Check connection settings.")
            sys.exit(1)

        print(f"Database : {MDB_PATH}")
        print(f"Output   : {OUT_DIR}")
        print(f"Tables   : {len(tables)}\n")

        ok, fail = [], []
        for table in tables:
            safe_name = table.replace("/", "_").replace(" ", "_")
            out_file  = os.path.join(OUT_DIR, f"{safe_name}.tdtp.xml")
            cmd = [TDTPCLI, "--config", cfg, "--export", table, "--output", out_file]
            t0  = time.time()
            res = subprocess.run(cmd, capture_output=True)
            elapsed = time.time() - t0
            stderr = res.stderr.decode("utf-8", errors="replace").strip()
            if res.returncode == 0 and os.path.exists(out_file):
                size = os.path.getsize(out_file)
                print(f"  OK  {table:40s}  {size:>10,} bytes  {elapsed:.1f}s")
                ok.append(table)
            else:
                print(f" ERR  {table}")
                if stderr:
                    print(f"      {stderr[:200]}")
                fail.append(table)
    finally:
        os.unlink(cfg)

    print(f"\n{'='*60}")
    print(f"Done: {len(ok)} exported, {len(fail)} failed")
    if fail:
        print("Failed:", fail)
    print(f"Output: {OUT_DIR}")


if __name__ == "__main__":
    main()
