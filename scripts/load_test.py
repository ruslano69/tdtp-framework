#!/usr/bin/env python3
"""
Load test for the --map cross-system sync path (ZTR-Live → EDM PostgreSQL).

For each scale N it measures three phases:
  1. export  — bulk pipeline pulls N employees from ZTR-Live → TDTP file
  2. insert  — --map into an empty table (pure INSERT)
  3. upsert  — --map the same data again (pure UPDATE via ON CONFLICT)

Reports wall-clock time and rows/sec per phase.

Usage:
    python scripts/load_test.py                 # real data, default scales
    python scripts/load_test.py 100 500 1478    # real data, custom scales
    python scripts/load_test.py stress          # synthetic large-volume stress
    python scripts/load_test.py stress 5000 100000

Stress mode amplifies the real bulk export (realistic Cyrillic content) to
arbitrary row counts with unique ext_ids, then measures only the --map write
path into PostgreSQL — isolating DB throughput from the MSSQL export.

Requires: Docker container `tdtp-postgres` (db/user=tdtp) with edm.edm_employees,
and ZTR-Live reachable (sql-srv1). Uses mappings/edm_mapping_load.yaml
(min_interval=0 so repeated runs are not blocked by loop guard).
"""

import subprocess
import sys
import os
import time
import pathlib

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8")

WORK_DIR = pathlib.Path(__file__).parent.parent.resolve()
TDTP = str(WORK_DIR / ("tdtpcli.exe" if os.name == "nt" else "tdtpcli"))
BULK_PIPELINE = "pipelines/export-employees-bulk.yaml"
LOAD_MAPPING = "mappings/edm_mapping_load.yaml"

# How to reset the target table between runs (container-specific).
PG_CONTAINER = "tdtp-postgres"
TRUNCATE_CMD = ["docker", "exec", "-i", PG_CONTAINER,
                "psql", "-U", "tdtp", "-d", "tdtp",
                "-c", "TRUNCATE edm.edm_employees;"]
COUNT_CMD = ["docker", "exec", "-i", PG_CONTAINER,
             "psql", "-U", "tdtp", "-d", "tdtp", "-t",
             "-c", "SELECT COUNT(*) FROM edm.edm_employees;"]

DEFAULT_SCALES = [100, 500, 1000, 1478]
STRESS_SCALES = [5000, 10000, 50000, 100000]
BASE_FILE = "out/bulk_1478.tdtp.xml"  # source of realistic rows for stress mode


def run_timed(args: list[str]) -> tuple[float, str]:
    """Run a command, return (elapsed_seconds, stdout). Raises on failure."""
    t0 = time.perf_counter()
    r = subprocess.run(args, cwd=WORK_DIR, capture_output=True,
                       text=True, encoding="utf-8", errors="replace")
    elapsed = time.perf_counter() - t0
    if r.returncode != 0:
        sys.exit(f"FAILED ({r.returncode}): {' '.join(args)}\n{r.stdout}\n{r.stderr}")
    return elapsed, r.stdout


def truncate():
    subprocess.run(TRUNCATE_CMD, cwd=WORK_DIR, capture_output=True)


def table_count() -> str:
    r = subprocess.run(COUNT_CMD, cwd=WORK_DIR, capture_output=True, text=True)
    return r.stdout.strip()


def rate(n: int, secs: float) -> str:
    return f"{n / secs:,.0f}" if secs > 0 else "—"


def generate_synthetic(n: int) -> str:
    """Amplify the real bulk export to n rows with unique ext_ids.

    Cycles through real rows (keeping realistic Cyrillic names/departments) and
    overwrites the first field (employee_code/ext_id) with a unique value so the
    ON CONFLICT key never collides. Returns the output file path.
    """
    import re

    base_path = WORK_DIR / BASE_FILE
    if not base_path.exists():
        sys.exit(f"Base file {BASE_FILE} missing — run a real scale first "
                 f"(python scripts/load_test.py 1478)")

    text = base_path.read_text(encoding="utf-8")
    rows = re.findall(r"<R>(.*?)</R>", text)
    if not rows:
        sys.exit("No rows found in base file")

    prefix = text[: text.index("<R>")]
    suffix = text[text.rindex("</R>") + len("</R>"):]
    prefix = re.sub(r"<RecordsInPart>\d+</RecordsInPart>",
                    f"<RecordsInPart>{n}</RecordsInPart>", prefix)

    out_path = WORK_DIR / f"out/syn_{n}.tdtp.xml"
    with out_path.open("w", encoding="utf-8") as f:
        f.write(prefix)
        for i in range(n):
            fields = rows[i % len(rows)].split("|")
            fields[0] = f"L{i}"          # unique ext_id
            f.write("<R>" + "|".join(fields) + "</R>")
        f.write(suffix)
    return f"out/syn_{n}.tdtp.xml"


def run_stress(scales: list[int]):
    print(f"\nStress test: --map write path → PostgreSQL (synthetic rows)")
    print(f"Scales: {scales}\n")

    header = f"{'rows':>7} | {'gen':>8} | {'insert':>9} {'ins r/s':>9} | {'upsert':>9} {'ups r/s':>9}"
    print(header)
    print("-" * len(header))

    for n in scales:
        t0 = time.perf_counter()
        input_file = generate_synthetic(n)
        t_gen = time.perf_counter() - t0

        truncate()
        t_insert, _ = run_timed([TDTP, "--map", LOAD_MAPPING, "--input", input_file])
        t_upsert, _ = run_timed([TDTP, "--map", LOAD_MAPPING, "--input", input_file])

        print(f"{n:>7} | {t_gen:>7.3f}s | {t_insert:>8.3f}s {rate(n, t_insert):>9} | "
              f"{t_upsert:>8.3f}s {rate(n, t_upsert):>9}")
        os.remove(WORK_DIR / input_file)

    print(f"\nFinal table count: {table_count()}")


def main():
    args = sys.argv[1:]
    if args and args[0] == "stress":
        scales = [int(x) for x in args[1:]] or STRESS_SCALES
        run_stress(scales)
        return

    scales = [int(x) for x in args] or DEFAULT_SCALES

    print(f"\nLoad test: --map (ZTR-Live → EDM PostgreSQL)")
    print(f"Scales: {scales}\n")

    header = f"{'rows':>6} | {'export':>9} | {'insert':>9} {'ins r/s':>9} | {'upsert':>9} {'ups r/s':>9}"
    print(header)
    print("-" * len(header))

    for n in scales:
        # 1. export N employees from ZTR-Live
        t_export, _ = run_timed(
            [TDTP, "--pipeline", BULK_PIPELINE, f"@limit={n}"])
        input_file = f"out/bulk_{n}.tdtp.xml"

        # 2. insert into empty table
        truncate()
        t_insert, _ = run_timed(
            [TDTP, "--map", LOAD_MAPPING, "--input", input_file])

        # 3. upsert (same data, all rows conflict → UPDATE)
        t_upsert, _ = run_timed(
            [TDTP, "--map", LOAD_MAPPING, "--input", input_file])

        print(f"{n:>6} | {t_export:>8.3f}s | {t_insert:>8.3f}s {rate(n, t_insert):>9} | "
              f"{t_upsert:>8.3f}s {rate(n, t_upsert):>9}")

    print(f"\nFinal table count: {table_count()}")


if __name__ == "__main__":
    main()
