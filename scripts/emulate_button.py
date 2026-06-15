#!/usr/bin/env python3
"""
Emulates the "Send to EDM" button click in the source app UI.

Usage:
    python scripts/emulate_button.py <emp_code>
    python scripts/emulate_button.py 00247

What it does:
    1. Runs tdtpcli --pipeline to export one employee from ZTR-Live → XML
    2. Runs tdtpcli --map to remap fields and upsert into EDM PostgreSQL

The binary is expected at ./tdtpcli or ./tdtpcli.exe.
"""

import subprocess
import sys
import os
import pathlib

# Windows consoles often default to a legacy code page (cp1251/cp866) that can't
# encode box-drawing or arrow glyphs. Force UTF-8 so prints never crash.
if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8")
    sys.stderr.reconfigure(encoding="utf-8")

# Working directory — where tdtpcli lives alongside pipelines/ and mappings/
WORK_DIR = pathlib.Path(__file__).parent.parent.resolve()

TDTP = str(WORK_DIR / ("tdtpcli.exe" if os.name == "nt" else "tdtpcli"))
PIPELINE = "pipelines/export-single-employee.yaml"
MAPPING  = "mappings/edm_mapping.yaml"


def run(args: list[str], label: str) -> None:
    print(f"\n{'='*60}")
    print(f"  {label}")
    print(f"  $ {' '.join(args)}")
    print(f"{'='*60}")
    result = subprocess.run(args, cwd=WORK_DIR)
    if result.returncode != 0:
        print(f"\n[ERROR] {label} failed (exit code {result.returncode})")
        sys.exit(result.returncode)


def main() -> None:
    if len(sys.argv) < 2:
        emp_code = input("Employee code (e.g. 00247): ").strip()
    else:
        emp_code = sys.argv[1].strip()

    if not emp_code:
        print("[ERROR] employee code is required")
        sys.exit(1)

    print(f"\n>>> Syncing employee {emp_code!r} to EDM...")

    # Step 1: Export from ZTR-Live
    run(
        [TDTP, "--pipeline", PIPELINE, f"@emp_code={emp_code}"],
        f"[1/2] Export employee {emp_code} from ZTR-Live",
    )

    input_file = f"out/emp_{emp_code}.tdtp.xml"

    # Step 2: Map and upsert into EDM
    run(
        [TDTP, "--map", MAPPING, "--input", input_file],
        f"[2/2] Map + upsert → EDM (edm.edm_employees)",
    )

    print(f"\n>>> Done. Employee {emp_code!r} is now in EDM.")
    print(f"    Verify (adjust -U/-d to your target DB): "
          f"psql -c \"SELECT * FROM edm.edm_employees WHERE ext_id = '{emp_code}'\"")


if __name__ == "__main__":
    main()
