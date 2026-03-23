#!/usr/bin/env python3
"""
Travel Agency Pipeline Coordinator
Управляет синхронизацией MSSQL → S3 → PostgreSQL через tdtpcli.
Redis — единственный источник состояния. Все решения принимает по Redis.

State model (Redis keys):
  tdtp:travel:<table>:cursor          — ISO timestamp последнего успешного extract
  tdtp:travel:<table>:sync_ts         — SYNC_TS последнего успешного extract
  tdtp:travel:<table>:state           — JSON: {status, rows, duration, error, ts}
  tdtp:travel:coordinator:alive       — heartbeat (TTL 90s)
  tdtp:travel:coordinator:cycle       — номер цикла
  tdtp:pipeline:<name>:state          — результат pipeline (пишет tdtpcli)

Pub/Sub:
  tdtp:travel:events                  — JSON события для мониторинга

Usage:
    python coordinator.py [--interval 30] [--tdtpcli ./tdtpcli.exe]
                          [--redis localhost:6379] [--once]

Dependencies:
    pip install redis colorama
"""

import argparse
import json
import os
import re
import subprocess
import sys
import tempfile
import time
from datetime import datetime
from pathlib import Path

import redis
from colorama import Fore, Style, init

init(autoreset=True)

# ─── Config ──────────────────────────────────────────────────────────────────

REDIS_KEY_PREFIX = "tdtp:travel"
PIPELINES_DIR    = Path(__file__).parent / "pipelines"
TDTPCLI_DEFAULT  = str(Path(__file__).parents[3] / "tdtpcli.exe")

# Таблицы: (имя, интервал-кратность, начальный cursor)
# кратность = синхронизируем каждые N циклов координатора
TABLES = [
    # name,         mult, init_cursor
    ("reference",   10,   "2020-01-01 00:00:00"),   # справочники — редко
    ("customers",   1,    "2020-01-01 00:00:00"),   # часто
    ("schedule",    2,    "2020-01-01 00:00:00"),   # средне
    ("sales",       1,    "2020-01-01 00:00:00"),   # часто
]

# ─── Logging ─────────────────────────────────────────────────────────────────

def log(color, tag, msg):
    ts = datetime.now().strftime("%H:%M:%S")
    print(f"{Fore.WHITE}[{ts}] {color}{tag:<22}{Style.RESET_ALL} {msg}")


def log_ok(tag, msg):   log(Fore.GREEN,   f"✓ {tag}", msg)
def log_err(tag, msg):  log(Fore.RED,     f"✗ {tag}", msg)
def log_info(tag, msg): log(Fore.CYAN,    f"  {tag}", msg)
def log_skip(tag, msg): log(Fore.WHITE,   f"· {tag}", msg)

# ─── Redis state helpers ──────────────────────────────────────────────────────

def get_cursor(r: redis.Redis, table: str, default: str) -> str:
    key = f"{REDIS_KEY_PREFIX}:{table}:cursor"
    val = r.get(key)
    return val if val else default


def set_cursor(r: redis.Redis, table: str, ts: str):
    r.set(f"{REDIS_KEY_PREFIX}:{table}:cursor", ts)


def set_state(r: redis.Redis, table: str, stage: str, state: dict):
    key = f"{REDIS_KEY_PREFIX}:{table}:{stage}:state"
    r.set(key, json.dumps(state), ex=86400)


def get_last_sync_ts(r: redis.Redis, table: str) -> str | None:
    key = f"{REDIS_KEY_PREFIX}:{table}:sync_ts"
    return r.get(key)


def set_sync_ts(r: redis.Redis, table: str, sync_ts: str):
    r.set(f"{REDIS_KEY_PREFIX}:{table}:sync_ts", sync_ts, ex=86400)


def publish(r: redis.Redis, event: dict):
    r.publish(f"{REDIS_KEY_PREFIX}:events", json.dumps(event))

# ─── Template substitution ────────────────────────────────────────────────────

def render_template(yaml_path: Path, vars: dict) -> str:
    """Substitute {{VAR}} placeholders in YAML template, return rendered text."""
    text = yaml_path.read_text(encoding="utf-8")
    for k, v in vars.items():
        text = text.replace(f"{{{{{k}}}}}", v)
    # Detect unreplaced placeholders
    remaining = re.findall(r"\{\{[A-Z_]+\}\}", text)
    if remaining:
        raise ValueError(f"Unreplaced template vars in {yaml_path.name}: {remaining}")
    return text


def write_temp_yaml(content: str) -> str:
    """Write content to a temp file, return its path."""
    fd, path = tempfile.mkstemp(suffix=".yaml", prefix="tdtp_travel_")
    os.write(fd, content.encode("utf-8"))
    os.close(fd)
    return path

# ─── Pipeline runner ──────────────────────────────────────────────────────────

def run_pipeline(tdtpcli: str, yaml_path: str, label: str) -> tuple[bool, str]:
    """Run tdtpcli --pipeline, return (success, output)."""
    cmd = [tdtpcli, "--pipeline", yaml_path]
    t0 = time.time()
    try:
        result = subprocess.run(
            cmd,
            capture_output=True,
            text=True,
            timeout=300,
        )
        elapsed = round(time.time() - t0, 1)
        out = (result.stdout + result.stderr).strip()
        if result.returncode == 0:
            return True, f"{elapsed}s\n{out}"
        else:
            return False, f"exit={result.returncode} {elapsed}s\n{out}"
    except subprocess.TimeoutExpired:
        return False, "timeout (300s)"
    except FileNotFoundError:
        return False, f"tdtpcli not found: {tdtpcli}"


def extract_rows_from_output(output: str) -> int:
    """Parse row count from tdtpcli output if available."""
    m = re.search(r"(\d+)\s+row", output, re.IGNORECASE)
    return int(m.group(1)) if m else -1

# ─── Sync one table ───────────────────────────────────────────────────────────

def sync_table(r: redis.Redis, tdtpcli: str, table: str, default_cursor: str) -> bool:
    """
    Full sync cycle for one table:
      1. Read cursor from Redis
      2. Render extract template → temp YAML → run tdtpcli
      3. On success: render load template → temp YAML → run tdtpcli
      4. Update cursor and state in Redis
    Returns True if both stages succeeded.
    """
    sync_ts = datetime.now().strftime("%Y%m%d_%H%M%S")
    cursor  = get_cursor(r, table, default_cursor)

    log_info(table, f"cursor={cursor}  sync_ts={sync_ts}")

    # ── Stage 1: Extract (MSSQL → S3) ────────────────────────────────────────
    extract_tpl = PIPELINES_DIR / f"extract_{table}.yaml"
    if not extract_tpl.exists():
        log_err(table, f"missing template: {extract_tpl}")
        return False

    try:
        extract_yaml = render_template(extract_tpl, {
            "LAST_SYNC": cursor,
            "SYNC_TS":   sync_ts,
        })
        tmp_extract = write_temp_yaml(extract_yaml)
    except Exception as e:
        log_err(table, f"template render error: {e}")
        return False

    t0 = datetime.now()
    try:
        ok, out = run_pipeline(tdtpcli, tmp_extract, f"{table}/extract")
    finally:
        os.unlink(tmp_extract)

    rows = extract_rows_from_output(out)
    duration = round((datetime.now() - t0).total_seconds(), 1)

    if not ok:
        log_err(f"{table}/extract", f"FAILED ({duration}s)\n{out[:300]}")
        set_state(r, table, "extract", {
            "status": "error", "error": out[:500],
            "ts": datetime.now().isoformat(), "duration": duration,
        })
        publish(r, {"event": "extract_failed", "table": table,
                    "sync_ts": sync_ts, "error": out[:200]})
        return False

    log_ok(f"{table}/extract", f"{rows} rows  {duration}s")
    set_state(r, table, "extract", {
        "status": "ok", "rows": rows,
        "ts": datetime.now().isoformat(), "duration": duration,
        "sync_ts": sync_ts, "cursor": cursor,
    })
    set_sync_ts(r, table, sync_ts)
    publish(r, {"event": "extract_ok", "table": table,
                "sync_ts": sync_ts, "rows": rows, "duration": duration})

    # Skip load if nothing new
    if rows == 0:
        log_skip(f"{table}/load", "nothing new, skipped")
        return True

    # ── Stage 2: Load (S3 → PostgreSQL) ──────────────────────────────────────
    load_tpl = PIPELINES_DIR / f"load_{table}.yaml"
    if not load_tpl.exists():
        log_skip(f"{table}/load", f"no load template ({load_tpl.name}), skip")
        # Still update cursor — extract succeeded
        set_cursor(r, table, datetime.now().strftime("%Y-%m-%d %H:%M:%S"))
        return True

    try:
        load_yaml = render_template(load_tpl, {"SYNC_TS": sync_ts})
        tmp_load  = write_temp_yaml(load_yaml)
    except Exception as e:
        log_err(f"{table}/load", f"template render error: {e}")
        return False

    t0 = datetime.now()
    try:
        ok, out = run_pipeline(tdtpcli, tmp_load, f"{table}/load")
    finally:
        os.unlink(tmp_load)

    duration = round((datetime.now() - t0).total_seconds(), 1)
    rows_loaded = extract_rows_from_output(out)

    if not ok:
        log_err(f"{table}/load", f"FAILED ({duration}s)\n{out[:300]}")
        set_state(r, table, "load", {
            "status": "error", "error": out[:500],
            "ts": datetime.now().isoformat(), "duration": duration,
            "sync_ts": sync_ts,
        })
        publish(r, {"event": "load_failed", "table": table,
                    "sync_ts": sync_ts, "error": out[:200]})
        return False

    log_ok(f"{table}/load", f"{rows_loaded} rows  {duration}s → PostgreSQL")
    set_state(r, table, "load", {
        "status": "ok", "rows": rows_loaded,
        "ts": datetime.now().isoformat(), "duration": duration,
        "sync_ts": sync_ts,
    })

    # ── Advance cursor only after both stages succeed ──────────────────────
    new_cursor = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    set_cursor(r, table, new_cursor)
    log_info(f"{table}/cursor", f"→ {new_cursor}")

    publish(r, {"event": "sync_complete", "table": table,
                "sync_ts": sync_ts, "rows": rows_loaded,
                "cursor": new_cursor})
    return True

# ─── Dashboard ────────────────────────────────────────────────────────────────

def print_dashboard(r: redis.Redis, cycle: int):
    print(f"\n{Fore.WHITE}{'─'*60}")
    print(f"  Coordinator  cycle={cycle}  "
          f"{datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    print(f"{'─'*60}{Style.RESET_ALL}")
    for table, _, default in TABLES:
        cursor = get_cursor(r, table, default)
        ext = r.get(f"{REDIS_KEY_PREFIX}:{table}:extract:state")
        lod = r.get(f"{REDIS_KEY_PREFIX}:{table}:load:state")

        ext_s = json.loads(ext) if ext else {}
        lod_s = json.loads(lod) if lod else {}

        ext_ok  = "✓" if ext_s.get("status") == "ok"  else ("✗" if ext_s else "·")
        lod_ok  = "✓" if lod_s.get("status") == "ok"  else ("✗" if lod_s else "·")
        rows    = ext_s.get("rows", "-")
        ldrows  = lod_s.get("rows", "-")

        color = Fore.GREEN if ext_ok == "✓" and lod_ok == "✓" else \
                Fore.RED   if "✗" in (ext_ok, lod_ok) else Fore.WHITE

        print(f"  {color}{table:<12}{Style.RESET_ALL}"
              f"  cursor={cursor[:16]}"
              f"  extract={ext_ok}({rows})"
              f"  load={lod_ok}({ldrows})")
    print()

# ─── Main loop ────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description="Travel Agency Pipeline Coordinator")
    parser.add_argument("--interval",  type=float, default=30.0,
                        help="Seconds between coordinator cycles (default: 30)")
    parser.add_argument("--tdtpcli",   default=TDTPCLI_DEFAULT,
                        help="Path to tdtpcli executable")
    parser.add_argument("--redis",     default="localhost:6379",
                        help="Redis address (default: localhost:6379)")
    parser.add_argument("--once",      action="store_true",
                        help="Run one cycle and exit")
    args = parser.parse_args()

    if not Path(args.tdtpcli).exists():
        # Try current directory
        local = Path("tdtpcli.exe")
        if local.exists():
            args.tdtpcli = str(local)
        else:
            print(f"{Fore.RED}tdtpcli not found: {args.tdtpcli}{Style.RESET_ALL}")
            print(f"  Use --tdtpcli /path/to/tdtpcli.exe")
            sys.exit(1)

    host, port = args.redis.rsplit(":", 1)
    r = redis.Redis(host=host, port=int(port), decode_responses=True)
    r.ping()

    print(f"{Fore.GREEN}Travel Agency Coordinator{Style.RESET_ALL}")
    print(f"  tdtpcli  : {args.tdtpcli}")
    print(f"  interval : {args.interval}s")
    print(f"  redis    : {args.redis}")
    print(f"  pipelines: {PIPELINES_DIR}")
    print(f"  Press Ctrl+C to stop\n")

    cycle = int(r.get(f"{REDIS_KEY_PREFIX}:coordinator:cycle") or 0)

    try:
        while True:
            cycle += 1
            r.set(f"{REDIS_KEY_PREFIX}:coordinator:alive",
                  datetime.now().isoformat(), ex=90)
            r.set(f"{REDIS_KEY_PREFIX}:coordinator:cycle", cycle)

            log_info("CYCLE", f"#{cycle} starting")
            publish(r, {"event": "cycle_start", "cycle": cycle,
                        "ts": datetime.now().isoformat()})

            for table, mult, default in TABLES:
                if cycle % mult != 0:
                    log_skip(table, f"every {mult} cycles, skip")
                    continue
                try:
                    sync_table(r, args.tdtpcli, table, default)
                except Exception as e:
                    log_err(table, f"unhandled error: {e}")

            publish(r, {"event": "cycle_done", "cycle": cycle,
                        "ts": datetime.now().isoformat()})

            print_dashboard(r, cycle)

            if args.once:
                break

            log_info("SLEEP", f"{args.interval}s until cycle #{cycle+1}")
            time.sleep(args.interval)

    except KeyboardInterrupt:
        print(f"\n{Fore.YELLOW}Coordinator stopped at cycle {cycle}.{Style.RESET_ALL}")


if __name__ == "__main__":
    main()
