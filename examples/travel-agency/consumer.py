#!/usr/bin/env python3
"""
Travel Agency Data Consumer — per-node
Subscribes to Redis pub/sub tdtp:travel:notify,
runs tdtpcli --map broker://<queue> for each entity,
writes audit marker to S3.

Usage:
    python consumer.py --node branch
    python consumer.py --node central

Dependencies:
    pip install redis boto3 botocore colorama
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
import redis
from colorama import Fore, Style, init

init(autoreset=True)

# ─── Config ───────────────────────────────────────────────────────────────────

REDIS_PREFIX    = "tdtp:travel"
NOTIFY_CHANNEL  = f"{REDIS_PREFIX}:notify"
TRAVEL_DIR      = Path(__file__).parent
TDTPCLI_DEFAULT = str(Path(__file__).parents[2] / "tdtpcli.exe")

S3_ENDPOINT   = "http://localhost:8333"
S3_BUCKET     = "travel-agency"
S3_ACCESS_KEY = "tdtp_access"
S3_SECRET_KEY = "tdtp_secret"

# ─── Queue → handler mapping ──────────────────────────────────────────────────
# map_yaml   — path to mapping YAML (input_source.broker + targets)
# s3_prefix  — S3 prefix for audit log entry
# label      — human-readable name

MAPPINGS_DIR = TRAVEL_DIR.parents[1] / "mappings"

QUEUE_HANDLERS = {
    # ── Branch receives from Central/Airline ─────────────────────────────────
    "tdtp.sync.countries": {
        "map_yaml":  str(MAPPINGS_DIR / "sync_countries.yaml"),
        "s3_prefix": "archive/countries",
        "label":     "countries -> branch",
    },
    "tdtp.sync.guides": {
        "map_yaml":  str(MAPPINGS_DIR / "sync_guides.yaml"),
        "s3_prefix": "archive/guides",
        "label":     "guides -> branch",
    },
    "tdtp.sync.tours": {
        "map_yaml":  str(MAPPINGS_DIR / "sync_tours.yaml"),
        "s3_prefix": "archive/tours",
        "label":     "tours -> branch",
    },
    "tdtp.sync.schedule": {
        "map_yaml":  str(MAPPINGS_DIR / "sync_schedule.yaml"),
        "s3_prefix": "archive/schedule",
        "label":     "schedule -> branch",
    },
    # ── Central receives from Airline/Branch ─────────────────────────────────
    "tdtp.sync.flights": {
        "map_yaml":  str(MAPPINGS_DIR / "sync_flights.yaml"),
        "s3_prefix": "archive/flights",
        "label":     "flights -> central",
    },
    "tdtp.sync.reservations": {
        "map_yaml":  str(MAPPINGS_DIR / "sync_reservations.yaml"),
        "s3_prefix": "archive/reservations",
        "label":     "reservations -> central",
    },
    "tdtp.sync.branch.customers": {
        "map_yaml":  str(MAPPINGS_DIR / "sync_branch_customers.yaml"),
        "s3_prefix": "archive/branch/customers",
        "label":     "customers -> central",
    },
    "tdtp.sync.branch.sales": {
        "map_yaml":  str(MAPPINGS_DIR / "sync_branch_sales.yaml"),
        "s3_prefix": "archive/branch/sales",
        "label":     "sales -> central",
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

# ─── Logging ──────────────────────────────────────────────────────────────────

def _ts():
    return datetime.now().strftime("%H:%M:%S")

def log_ok(tag, msg):   print(f"{Fore.WHITE}[{_ts()}] {Fore.GREEN}✓ {tag:<36}{Style.RESET_ALL} {msg}")
def log_err(tag, msg):  print(f"{Fore.WHITE}[{_ts()}] {Fore.RED}✗ {tag:<36}{Style.RESET_ALL} {msg}")
def log_info(tag, msg): print(f"{Fore.WHITE}[{_ts()}] {Fore.CYAN}  {tag:<36}{Style.RESET_ALL} {msg}")
def log_mq(tag, msg):   print(f"{Fore.WHITE}[{_ts()}] {Fore.MAGENTA}⇢ {tag:<36}{Style.RESET_ALL} {msg}")
def log_skip(tag, msg): print(f"{Fore.WHITE}[{_ts()}] {Fore.WHITE}· {tag:<36}{Style.RESET_ALL} {msg}")

# ─── S3 archive ───────────────────────────────────────────────────────────────

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


# ─── Import + merge ───────────────────────────────────────────────────────────

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


def set_state(r: redis.Redis, queue: str, state: dict):
    key = queue.replace(".", ":")
    r.set(f"{REDIS_PREFIX}:consumer:{key}", json.dumps(state), ex=86400)


# ─── Notification handler ─────────────────────────────────────────────────────

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

    log_mq(label, f"notify received -> {queue}")

    ok, out, rows = run_map_broker(tdtpcli, handler["map_yaml"], queue)
    if not ok:
        log_err(f"{label}/map", f"FAILED\n{out[:1500]}")
        set_state(r, queue, {"status": "map_error", "error": out[:2000],
                             "ts": datetime.now().isoformat()})
        return
    log_ok(f"{label}/map", out)
    elapsed_s = 0.0
    try:
        elapsed_s = float(out.split("s")[0])
    except Exception:
        pass

    # S3 audit marker (fire-and-forget)
    archive_marker(handler["s3_prefix"], sync_ts, rows, elapsed_s)

    # ── 4. Update state ───────────────────────────────────────────────────────
    set_state(r, queue, {
        "status": "ok", "rows": rows, "sync_ts": sync_ts,
        "ts": datetime.now().isoformat(),
    })
    r.set(f"{REDIS_PREFIX}:consumer:alive", datetime.now().isoformat(), ex=90)


# ─── Main ─────────────────────────────────────────────────────────────────────

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

    print(f"\n{Fore.GREEN}Travel Agency Consumer — {args.node.upper()}{Style.RESET_ALL}")
    print(f"  tdtpcli : {args.tdtpcli}")
    print(f"  redis   : {args.redis}  channel: {NOTIFY_CHANNEL}")
    print(f"\n  Listening queues:")
    for q in sorted(my_queues):
        h = QUEUE_HANDLERS[q]
        print(f"    {q:<35} -> --map {Path(h['map_yaml']).name}")
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
