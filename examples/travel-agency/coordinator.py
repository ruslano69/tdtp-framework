#!/usr/bin/env python3
"""
Travel Agency Coordinator — Event-Driven
Слушает topic exchange RabbitMQ (события от activity.py),
запускает tdtpcli --export-broker (source DB → named queue),
публикует уведомление в Redis pub/sub.

Импорт данных — задача consumer.py.

Dependencies:
    pip install pika redis colorama
"""

import json
import os
import re
import subprocess
import sys
import time
from datetime import datetime
from pathlib import Path

import pika
import redis
from colorama import Fore, Style, init

init(autoreset=True)

# ─── Config ───────────────────────────────────────────────────────────────────

REDIS_PREFIX    = "tdtp:travel"
NOTIFY_CHANNEL  = f"{REDIS_PREFIX}:notify"
EXCHANGE        = "travel"
QUEUE           = "tdtp.coordinator"
TRAVEL_DIR      = Path(__file__).parent
TDTPCLI_DEFAULT = str(Path(__file__).parents[2] / "tdtpcli.exe")
DEBOUNCE_TTL    = 10
DEFAULT_CURSOR  = "2020-01-01 00:00:00"

# ─── Routing table ────────────────────────────────────────────────────────────
# table       — имя таблицы или VIEW в source DB
# fields      — колонки для --fields (None = все)
# incremental — поле для курсора инкрементала (None = полная выгрузка)
# src_cfg     — имя config файла (в TRAVEL_DIR) с source DB + broker settings
# queue       — named RabbitMQ queue
# label       — читаемое название

ROUTE_MAP: dict[str, list[dict]] = {
    "airline.flights.updated": [
        {
            "table":       "v_flights",
            "fields":      None,
            "incremental": "last_updated",
            "src_cfg":     "config_src_tdtp_sync_flights.yaml",
            "queue":       "tdtp.sync.flights",
            "label":       "Airline → Central: flights",
        },
    ],
    "airline.reservations.updated": [
        {
            "table":       "flight_reservations",
            "fields":      "reservation_id,flight_id,booking_ref_external,passenger_name,seat_class,price_paid,status,agency_id,last_updated",
            "incremental": "last_updated",
            "src_cfg":     "config_src_tdtp_sync_reservations.yaml",
            "queue":       "tdtp.sync.reservations",
            "label":       "Airline → Central: reservations",
        },
    ],
    "central.catalog.updated": [
        {
            "table":       "countries",
            "fields":      "country_id,country_code,country_name,continent,currency_code,is_visa_required",
            "incremental": None,
            "src_cfg":     "config_src_tdtp_sync_countries.yaml",
            "queue":       "tdtp.sync.countries",
            "label":       "Central → Branch: countries",
        },
        {
            "table":       "guides",
            "fields":      "guide_id,first_name,last_name,specialization,rating,languages,is_active",
            "incremental": "last_updated",
            "src_cfg":     "config_src_tdtp_sync_guides.yaml",
            "queue":       "tdtp.sync.guides",
            "label":       "Central → Branch: guides",
        },
        {
            "table":       "tours",
            "fields":      "tour_id,tour_code,tour_name,description,destination_country_id,duration_days,difficulty_level,max_group_size,base_price,is_active",
            "incremental": None,
            "src_cfg":     "config_src_tdtp_sync_tours.yaml",
            "queue":       "tdtp.sync.tours",
            "label":       "Central → Branch: tours",
        },
        {
            "table":       "tour_schedule",
            "fields":      "schedule_id,tour_id,guide_id,start_date,end_date,available_slots,booked_slots,price_modifier,status",
            "incremental": "last_updated",
            "src_cfg":     "config_src_tdtp_sync_schedule.yaml",
            "queue":       "tdtp.sync.schedule",
            "label":       "Central → Branch: schedule",
        },
    ],
    "branch.customers.registered": [
        {
            "table":       "v_local_customers",
            "fields":      None,
            "incremental": "last_updated",
            "src_cfg":     "config_src_tdtp_sync_branch_customers.yaml",
            "queue":       "tdtp.sync.branch.customers",
            "label":       "Branch → Central: customers",
        },
    ],
    "branch.sales.created": [
        {
            "table":       "v_local_sales",
            "fields":      None,
            "incremental": "last_updated",
            "src_cfg":     "config_src_tdtp_sync_branch_sales.yaml",
            "queue":       "tdtp.sync.branch.sales",
            "label":       "Branch → Central: sales",
        },
    ],
    "central.sales.changed":  [],
    "central.customers.new":  [],
}

# ─── Logging ──────────────────────────────────────────────────────────────────

def _ts():
    return datetime.now().strftime("%H:%M:%S")

def log_ok(tag, msg):   print(f"{Fore.WHITE}[{_ts()}] {Fore.GREEN}✓ {tag:<34}{Style.RESET_ALL} {msg}")
def log_err(tag, msg):  print(f"{Fore.WHITE}[{_ts()}] {Fore.RED}✗ {tag:<34}{Style.RESET_ALL} {msg}")
def log_info(tag, msg): print(f"{Fore.WHITE}[{_ts()}] {Fore.CYAN}  {tag:<34}{Style.RESET_ALL} {msg}")
def log_skip(tag, msg): print(f"{Fore.WHITE}[{_ts()}] {Fore.WHITE}· {tag:<34}{Style.RESET_ALL} {msg}")
def log_mq(tag, msg):   print(f"{Fore.WHITE}[{_ts()}] {Fore.MAGENTA}⇢ {tag:<34}{Style.RESET_ALL} {msg}")

# ─── Redis helpers ────────────────────────────────────────────────────────────

def get_cursor(r: redis.Redis, key: str) -> str:
    v = r.get(f"{REDIS_PREFIX}:cursor:{key}")
    return v if v else DEFAULT_CURSOR


def set_cursor(r: redis.Redis, key: str, ts: str):
    r.set(f"{REDIS_PREFIX}:cursor:{key}", ts)


def debounce_check(r: redis.Redis, key: str) -> bool:
    dkey = f"{REDIS_PREFIX}:debounce:{key}"
    return not r.set(dkey, "1", nx=True, ex=DEBOUNCE_TTL)


def set_state(r: redis.Redis, key: str, state: dict):
    r.set(f"{REDIS_PREFIX}:coord:{key}", json.dumps(state), ex=86400)


# ─── Export runner ────────────────────────────────────────────────────────────

def run_export_broker(tdtpcli: str, entry: dict, cursor: str) -> tuple[bool, int]:
    """Run tdtpcli --export-broker. Returns (ok, rows)."""
    cfg_path = str(TRAVEL_DIR / entry["src_cfg"])
    cmd = [tdtpcli, "--export-broker", entry["table"]]
    if entry.get("fields"):
        cmd += ["--fields", entry["fields"]]
    if entry.get("incremental") and cursor != DEFAULT_CURSOR:
        cmd += ["--where", f"{entry['incremental']} >= \"{cursor}\""]
    cmd += ["--config", cfg_path]

    t0 = time.time()
    try:
        result = subprocess.run(cmd, capture_output=True, text=True, timeout=120)
        elapsed = round(time.time() - t0, 1)
        out = (result.stdout + result.stderr).strip()
        rows = 0
        m = re.search(r"(\d+)\s+row", out, re.IGNORECASE)
        if not m:
            m = re.search(r"Exported\s+(\d+)\s+packet", out, re.IGNORECASE)
        if m:
            rows = int(m.group(1))
        # parse "Exported N packet(s)" → actual rows from import will tell us
        # but for now estimate from output
        ok = result.returncode == 0
        return ok, elapsed, out, rows
    except subprocess.TimeoutExpired:
        return False, 120, "timeout (120s)", 0
    except FileNotFoundError:
        return False, 0, f"tdtpcli not found: {tdtpcli}", 0


# ─── Core sync logic ──────────────────────────────────────────────────────────

def sync_route(r: redis.Redis, tdtpcli: str, routing_key: str, entry: dict) -> bool:
    label      = entry["label"]
    cursor_key = routing_key.replace(".", "_") + "__" + entry["table"]
    cursor     = get_cursor(r, cursor_key)

    log_info(label, f"cursor={cursor}  →  {entry['queue']}")

    ok, elapsed, out, rows = run_export_broker(tdtpcli, entry, cursor)

    if not ok:
        log_err(label, f"FAILED {elapsed}s\n{out[:1000]}")
        set_state(r, cursor_key, {"status": "error", "error": out[:2000], "ts": datetime.now().isoformat()})
        return False

    log_ok(label, f"{elapsed}s → {entry['queue']}")
    new_cursor = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    set_cursor(r, cursor_key, new_cursor)

    # Notify consumer via Redis pub/sub
    r.publish(NOTIFY_CHANNEL, json.dumps({
        "queue":    entry["queue"],
        "label":    label,
        "sync_ts":  datetime.now().strftime("%Y%m%d_%H%M%S"),
        "cursor":   new_cursor,
    }))
    set_state(r, cursor_key, {
        "status": "queued", "queue": entry["queue"],
        "ts": datetime.now().isoformat(), "elapsed": elapsed,
    })
    return True


# ─── Message handler ──────────────────────────────────────────────────────────

def handle_message(channel, method, _properties, body, r: redis.Redis, tdtpcli: str):
    routing_key = method.routing_key
    try:
        event = json.loads(body) if body else {}
    except Exception:
        event = {}

    log_mq("MSG", f"{routing_key}  node={event.get('node','?')} event={event.get('event','?')}")

    entries = ROUTE_MAP.get(routing_key)
    if entries is None:
        log_skip("ROUTE", f"unknown: {routing_key}")
        channel.basic_ack(delivery_tag=method.delivery_tag)
        return
    if not entries:
        channel.basic_ack(delivery_tag=method.delivery_tag)
        return

    if debounce_check(r, routing_key):
        log_skip("DEBOUNCE", f"{routing_key} (within {DEBOUNCE_TTL}s)")
        channel.basic_ack(delivery_tag=method.delivery_tag)
        return

    all_ok = True
    for entry in entries:
        if not sync_route(r, tdtpcli, routing_key, entry):
            all_ok = False

    r.set(f"{REDIS_PREFIX}:coordinator:alive", datetime.now().isoformat(), ex=90)
    if all_ok:
        log_ok("DONE", routing_key)
    else:
        log_err("PARTIAL", f"{routing_key}  some exports failed")

    channel.basic_ack(delivery_tag=method.delivery_tag)


# ─── Main ─────────────────────────────────────────────────────────────────────

def main():
    import argparse
    parser = argparse.ArgumentParser(description="Travel Agency Coordinator")
    parser.add_argument("--tdtpcli", default=TDTPCLI_DEFAULT)
    parser.add_argument("--amqp",    default="amqp://tdtp:tdtp@localhost:5672/")
    parser.add_argument("--redis",   default="localhost:6379")
    args = parser.parse_args()

    if not Path(args.tdtpcli).exists():
        local = Path("tdtpcli.exe")
        if local.exists():
            args.tdtpcli = str(local)
        else:
            print(f"{Fore.RED}tdtpcli not found{Style.RESET_ALL}")
            sys.exit(1)

    host, port = args.redis.rsplit(":", 1)
    r = redis.Redis(host=host, port=int(port), decode_responses=True)
    r.ping()
    log_ok("Redis", args.redis)

    params = pika.URLParameters(args.amqp)
    params.heartbeat = 60
    conn    = pika.BlockingConnection(params)
    channel = conn.channel()
    channel.exchange_declare(exchange=EXCHANGE, exchange_type="topic", durable=True)
    channel.queue_declare(queue=QUEUE, durable=True)
    channel.queue_bind(exchange=EXCHANGE, queue=QUEUE, routing_key="#")
    channel.basic_qos(prefetch_count=1)

    def _on_message(ch, method, props, body):
        handle_message(ch, method, props, body, r, args.tdtpcli)

    channel.basic_consume(queue=QUEUE, on_message_callback=_on_message)

    print(f"\n{Fore.GREEN}Travel Agency Coordinator{Style.RESET_ALL}")
    print(f"  tdtpcli : {args.tdtpcli}")
    print(f"  amqp    : {args.amqp}")
    print(f"  redis   : {args.redis}  notify: {NOTIFY_CHANNEL}")
    print(f"  debounce: {DEBOUNCE_TTL}s\n")
    print(f"  Routes:")
    for rk, entries in ROUTE_MAP.items():
        for e in entries:
            inc = f"  [incr: {e['incremental']}]" if e.get("incremental") else "  [full]"
            print(f"    {rk:<42} → {e['queue']}{inc}")
        if not entries:
            print(f"    {rk:<42}   (local only)")
    print(f"\n  Waiting for events... Ctrl+C to stop\n")

    r.set(f"{REDIS_PREFIX}:coordinator:alive", datetime.now().isoformat(), ex=90)
    try:
        while True:
            conn.process_data_events(time_limit=30)
            r.set(f"{REDIS_PREFIX}:coordinator:alive", datetime.now().isoformat(), ex=90)
    except KeyboardInterrupt:
        print(f"\n{Fore.YELLOW}Coordinator stopped.{Style.RESET_ALL}")
    finally:
        conn.close()


if __name__ == "__main__":
    main()
