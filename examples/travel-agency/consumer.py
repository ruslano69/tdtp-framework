#!/usr/bin/env python3
"""
Travel Agency Data Consumer вЂ” per-node
РџРѕРґРїРёСЃС‹РІР°РµС‚СЃСЏ РЅР° Redis pub/sub tdtp:travel:notify,
Р·Р°РїСѓСЃРєР°РµС‚ tdtpcli --import-broker (named queue в†’ staging),
РІС‹Р·С‹РІР°РµС‚ merge-РїСЂРѕС†РµРґСѓСЂСѓ, РїРёС€РµС‚ РјРµС‚РєСѓ РІ S3 (Р¶СѓСЂРЅР°Р»).

Usage:
    python consumer.py --node branch
    python consumer.py --node central

Dependencies:
    pip install redis psycopg2-binary boto3 botocore colorama
"""

import argparse
import json
import re
import subprocess
import sys
import time
from datetime import datetime
from pathlib import Path

import boto3
import botocore.config
import psycopg2
import redis
from colorama import Fore, Style, init

init(autoreset=True)

# в”Ђв”Ђв”Ђ Config в”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђ

REDIS_PREFIX    = "tdtp:travel"
NOTIFY_CHANNEL  = f"{REDIS_PREFIX}:notify"
TRAVEL_DIR      = Path(__file__).parent
TDTPCLI_DEFAULT = str(Path(__file__).parents[2] / "tdtpcli.exe")

S3_ENDPOINT   = "http://localhost:8333"
S3_BUCKET     = "travel-agency"
S3_ACCESS_KEY = "tdtp_access"
S3_SECRET_KEY = "tdtp_secret"

DSN_CENTRAL = "host=localhost port=5432 dbname=tdtp user=tdtp password=tdtp"
DSN_BRANCH  = "host=localhost port=5433 dbname=tdtp_branch user=tdtp password=tdtp"

CFG_BRANCH   = str(TRAVEL_DIR / "configs/config_branch.yaml")
CFG_CENTRAL  = str(TRAVEL_DIR / "configs/config_central.yaml")

# в”Ђв”Ђв”Ђ Queue в†’ handler mapping в”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђ
# dst_cfg     вЂ” tdtpcli config (database = destination + broker source queue)
# staging     вЂ” staging table name for --import-broker --table
# merge_proc  вЂ” PostgreSQL stored procedure (CALL proc())
# dsn         вЂ” psycopg2 DSN for merge call
# s3_prefix   вЂ” S3 prefix for audit log entry
# label       вЂ” human-readable name

QUEUE_HANDLERS = {
    # в”Ђв”Ђ Branch receives from Central/Airline в”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђ
    "tdtp.sync.countries": {
        "dst_cfg":    str(TRAVEL_DIR / "configs/config_dst_tdtp_sync_countries.yaml"),
        "staging":    "countries_cache_staging",
        "merge_proc": "merge_countries_cache",
        "dsn":        DSN_BRANCH,
        "s3_prefix":  "archive/countries",
        "label":      "countries в†’ branch",
    },
    "tdtp.sync.guides": {
        "dst_cfg":    str(TRAVEL_DIR / "configs/config_dst_tdtp_sync_guides.yaml"),
        "staging":    "guides_cache_staging",
        "merge_proc": "merge_guides_cache",
        "dsn":        DSN_BRANCH,
        "s3_prefix":  "archive/guides",
        "label":      "guides в†’ branch",
    },
    "tdtp.sync.tours": {
        "dst_cfg":    str(TRAVEL_DIR / "configs/config_dst_tdtp_sync_tours.yaml"),
        "staging":    "tours_cache_staging",
        "merge_proc": "merge_tours_cache",
        "dsn":        DSN_BRANCH,
        "s3_prefix":  "archive/tours",
        "label":      "tours в†’ branch",
    },
    "tdtp.sync.schedule": {
        "dst_cfg":    str(TRAVEL_DIR / "configs/config_dst_tdtp_sync_schedule.yaml"),
        "staging":    "schedule_cache_staging",
        "merge_proc": "merge_schedule_cache",
        "dsn":        DSN_BRANCH,
        "s3_prefix":  "archive/schedule",
        "label":      "schedule в†’ branch",
    },
    # в”Ђв”Ђ Central receives from Airline/Branch в”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђ
    "tdtp.sync.flights": {
        "dst_cfg":    str(TRAVEL_DIR / "configs/config_dst_tdtp_sync_flights.yaml"),
        "staging":    "flights_staging",
        "merge_proc": "merge_flights",
        "dsn":        DSN_CENTRAL,
        "s3_prefix":  "archive/flights",
        "label":      "flights в†’ central",
    },
    "tdtp.sync.reservations": {
        "dst_cfg":    str(TRAVEL_DIR / "configs/config_dst_tdtp_sync_reservations.yaml"),
        "staging":    "flight_reservations_staging",
        "merge_proc": "merge_flight_reservations",
        "dsn":        DSN_CENTRAL,
        "s3_prefix":  "archive/reservations",
        "label":      "reservations в†’ central",
    },
    "tdtp.sync.branch.customers": {
        "map_yaml":  str(TRAVEL_DIR.parents[1] / "mappings/sync_branch_customers.yaml"),
        "s3_prefix": "archive/branch/customers",
        "label":     "customers -> central",
    },
    "tdtp.sync.branch.sales": {
        "dst_cfg":    str(TRAVEL_DIR / "configs/config_dst_tdtp_sync_branch_sales.yaml"),
        "staging":    "branch_sales_inbox_staging",
        "merge_proc": "merge_branch_sales_inbox",
        "dsn":        DSN_CENTRAL,
        "s3_prefix":  "archive/branch/sales",
        "label":      "sales в†’ central",
    },
}

NODE_QUEUES = {
    "branch":  [
        "tdtp.sync.countries",
        "tdtp.sync.guides",
        "tdtp.sync.tours",
        "tdtp.sync.schedule",
    ],
    "central": [
        "tdtp.sync.flights",
        "tdtp.sync.reservations",
        "tdtp.sync.branch.customers",
        "tdtp.sync.branch.sales",
    ],
}

# в”Ђв”Ђв”Ђ Logging в”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђ

def _ts():
    return datetime.now().strftime("%H:%M:%S")

def log_ok(tag, msg):   print(f"{Fore.WHITE}[{_ts()}] {Fore.GREEN}вњ“ {tag:<36}{Style.RESET_ALL} {msg}")
def log_err(tag, msg):  print(f"{Fore.WHITE}[{_ts()}] {Fore.RED}вњ— {tag:<36}{Style.RESET_ALL} {msg}")
def log_info(tag, msg): print(f"{Fore.WHITE}[{_ts()}] {Fore.CYAN}  {tag:<36}{Style.RESET_ALL} {msg}")
def log_mq(tag, msg):   print(f"{Fore.WHITE}[{_ts()}] {Fore.MAGENTA}в‡ў {tag:<36}{Style.RESET_ALL} {msg}")
def log_skip(tag, msg): print(f"{Fore.WHITE}[{_ts()}] {Fore.WHITE}В· {tag:<36}{Style.RESET_ALL} {msg}")

# в”Ђв”Ђв”Ђ S3 archive в”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђ

def get_s3():
    return boto3.client(
        "s3",
        endpoint_url=S3_ENDPOINT,
        aws_access_key_id=S3_ACCESS_KEY,
        aws_secret_access_key=S3_SECRET_KEY,
        config=botocore.config.Config(signature_version="s3v4"),
        region_name="us-east-1",
    )


def archive_marker(s3_prefix: str, sync_ts: str, rows: int, elapsed: float):
    """Write a small JSON marker to S3 as audit log entry."""
    key = f"{s3_prefix}/{sync_ts}.json"
    body = json.dumps({"sync_ts": sync_ts, "rows": rows, "elapsed_s": elapsed}).encode()
    try:
        get_s3().put_object(Bucket=S3_BUCKET, Key=key, Body=body)
        log_info("S3 archive", f"s3://{S3_BUCKET}/{key}")
    except Exception as exc:
        log_err("S3 archive", f"failed: {exc}")


# в”Ђв”Ђв”Ђ Import + merge в”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђ

def run_map_broker(tdtpcli: str, map_yaml: str, queue: str) -> tuple[bool, str, int]:
    """tdtpcli --map yaml --input broker://queue — direct upsert, no staging."""
    t0 = time.time()
    try:
        result = subprocess.run(
            [tdtpcli, "--map", map_yaml, "--input", f"broker://{queue}"],
            capture_output=True, text=True, timeout=120,
        )
        elapsed = round(time.time() - t0, 1)
        out = (result.stdout + result.stderr).strip()
        rows = 0
        m = re.search(r"(\d+) rows upserted", out)
        if m:
            rows = int(m.group(1))
        ok = result.returncode == 0
        return ok, f"{elapsed}s rows={rows}" if ok else f"{elapsed}s\n{out}", rows
    except subprocess.TimeoutExpired:
        return False, "timeout (120s)", 0
    except FileNotFoundError:
        return False, f"tdtpcli not found: {tdtpcli}", 0


def run_import_broker(tdtpcli: str, dst_cfg: str, table: str) -> tuple[bool, str, int]:
    """tdtpcli --import-broker в†’ staging table."""
    t0 = time.time()
    try:
        result = subprocess.run(
            [tdtpcli, "--import-broker",
             "--table",    table,
             "--strategy", "replace",
             "--config",   dst_cfg],
            capture_output=True, text=True, timeout=120,
        )
        elapsed = round(time.time() - t0, 1)
        out  = (result.stdout + result.stderr).strip()
        rows = 0
        m = re.search(r"(\d+)\s+row", out, re.IGNORECASE)
        if not m:
            m = re.search(r"Imported\s+(\d+)", out, re.IGNORECASE)
        if m:
            rows = int(m.group(1))
        ok = result.returncode == 0
        return ok, f"{elapsed}s\n{out}" if not ok else f"{elapsed}s rows={rows}", rows
    except subprocess.TimeoutExpired:
        return False, "timeout (120s)", 0
    except FileNotFoundError:
        return False, f"tdtpcli not found: {tdtpcli}", 0


def run_merge(dsn: str, proc: str) -> tuple[bool, str]:
    """CALL merge_proc() via psycopg2."""
    t0 = time.time()
    try:
        conn = psycopg2.connect(dsn)
        conn.autocommit = True
        with conn.cursor() as cur:
            cur.execute(f"CALL {proc}()")
        conn.close()
        elapsed = round(time.time() - t0, 1)
        return True, f"{elapsed}s"
    except Exception as exc:
        elapsed = round(time.time() - t0, 1)
        return False, f"{elapsed}s  {exc}"


def set_state(r: redis.Redis, queue: str, state: dict):
    key = queue.replace(".", ":")
    r.set(f"{REDIS_PREFIX}:consumer:{key}", json.dumps(state), ex=86400)


# в”Ђв”Ђв”Ђ Notification handler в”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђ

def handle_notify(r: redis.Redis, tdtpcli: str, my_queues: set,
                  raw: str):
    try:
        msg = json.loads(raw)
    except Exception:
        log_err("NOTIFY", f"bad JSON: {raw[:200]}")
        return

    queue   = msg.get("queue", "")
    label   = msg.get("label", queue)
    sync_ts = msg.get("sync_ts", datetime.now().strftime("%Y%m%d_%H%M%S"))

    if queue not in my_queues:
        log_skip("NOTIFY", f"skip {queue} (not my queue)")
        return

    handler = QUEUE_HANDLERS.get(queue)
    if not handler:
        log_err("HANDLER", f"no handler for {queue}")
        return

    log_mq(label, f”notify received -> {queue}”)

    if handler.get(“map_yaml”):
        # Sprint 6: direct upsert via --map (no staging table, no merge proc)
        ok, out, rows = run_map_broker(tdtpcli, handler[“map_yaml”], queue)
        if not ok:
            log_err(f”{label}/map”, f”FAILED\n{out[:1500]}”)
            set_state(r, queue, {“status”: “map_error”, “error”: out[:2000],
                                 “ts”: datetime.now().isoformat()})
            return
        log_ok(f”{label}/map”, out)
        elapsed_s = 0.0
        try:
            elapsed_s = float(out.split(“s”)[0])
        except Exception:
            pass
    else:
        # Legacy: import-broker -> staging -> merge
        ok, out, rows = run_import_broker(tdtpcli, handler[“dst_cfg”], handler[“staging”])
        if not ok:
            log_err(f”{label}/import”, f”FAILED\n{out[:1500]}”)
            set_state(r, queue, {“status”: “import_error”, “error”: out[:2000],
                                 “ts”: datetime.now().isoformat()})
            return
        elapsed_str = out.split(“\n”)[0]
        log_ok(f”{label}/import”, f”{rows} rows -> {handler[‘staging’]}  {elapsed_str}”)

        ok, merge_out = run_merge(handler[“dsn”], handler[“merge_proc”])
        if not ok:
            log_err(f”{label}/merge”, f”FAILED: {merge_out}”)
            set_state(r, queue, {“status”: “merge_error”, “error”: merge_out,
                                 “ts”: datetime.now().isoformat()})
            return
        log_ok(f”{label}/merge”, f”{handler[‘merge_proc’]}()  {merge_out}”)
        elapsed_s = 0.0
        try:
            elapsed_s = float(elapsed_str.rstrip(“s”).split(“rows”)[0].strip())
        except Exception:
            pass

    # S3 audit marker (fire-and-forget)
    archive_marker(handler[“s3_prefix”], sync_ts, rows, elapsed_s)

    # в”Ђв”Ђ 4. Update state в”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђ
    set_state(r, queue, {
        "status": "ok", "rows": rows, "sync_ts": sync_ts,
        "ts": datetime.now().isoformat(),
    })
    r.set(f"{REDIS_PREFIX}:consumer:alive", datetime.now().isoformat(), ex=90)


# в”Ђв”Ђв”Ђ Main в”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђ

def main():
    parser = argparse.ArgumentParser(description="Travel Agency Data Consumer")
    parser.add_argument("--node",    required=True, choices=["branch", "central"])
    parser.add_argument("--tdtpcli", default=TDTPCLI_DEFAULT)
    parser.add_argument("--redis",   default="localhost:6379")
    args = parser.parse_args()

    if not Path(args.tdtpcli).exists():
        local = Path("tdtpcli.exe")
        if local.exists():
            args.tdtpcli = str(local)
        else:
            print(f"{Fore.RED}tdtpcli not found: {args.tdtpcli}{Style.RESET_ALL}")
            sys.exit(1)

    my_queues = set(NODE_QUEUES[args.node])

    host, port = args.redis.rsplit(":", 1)
    r = redis.Redis(host=host, port=int(port), decode_responses=True)
    r.ping()

    pubsub = r.pubsub()
    pubsub.subscribe(NOTIFY_CHANNEL)

    print(f"\n{Fore.GREEN}Travel Agency Consumer вЂ” {args.node.upper()}{Style.RESET_ALL}")
    print(f"  tdtpcli : {args.tdtpcli}")
    print(f"  redis   : {args.redis}  channel: {NOTIFY_CHANNEL}")
    print(f"\n  Listening queues:")
    for q in sorted(my_queues):
        h = QUEUE_HANDLERS[q]
        if h.get("map_yaml"):
            print(f"    {q:<35} -> --map {Path(h[‘map_yaml’]).name}")
        else:
            print(f"    {q:<35} -> {h[‘staging’]} -> CALL {h[‘merge_proc’]}()")
    print(f"\n  Waiting for notifications... Ctrl+C to stop\n")

    r.set(f"{REDIS_PREFIX}:consumer:alive", datetime.now().isoformat(), ex=90)
    try:
        while True:
            message = pubsub.get_message(ignore_subscribe_messages=True, timeout=30)
            if message and message["type"] == "message":
                handle_notify(r, args.tdtpcli, my_queues, message["data"])
            else:
                r.set(f"{REDIS_PREFIX}:consumer:alive", datetime.now().isoformat(), ex=90)
    except KeyboardInterrupt:
        print(f"\n{Fore.YELLOW}Consumer stopped.{Style.RESET_ALL}")
    finally:
        pubsub.unsubscribe()


if __name__ == "__main__":
    main()
