#!/usr/bin/env python3
"""
Travel Agency Coordinator вЂ” Event-Driven
РЎР»СѓС€Р°РµС‚ topic exchange RabbitMQ (СЃРѕР±С‹С‚РёСЏ РѕС‚ activity.py),
Р·Р°РїСѓСЃРєР°РµС‚ tdtpcli --export-broker (source DB в†’ named queue),
РїСѓР±Р»РёРєСѓРµС‚ СѓРІРµРґРѕРјР»РµРЅРёРµ РІ Redis pub/sub.

РРјРїРѕСЂС‚ РґР°РЅРЅС‹С… вЂ” Р·Р°РґР°С‡Р° consumer.py.

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

# в”Ђв”Ђв”Ђ Config в”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђ

REDIS_PREFIX    = "tdtp:travel"
NOTIFY_CHANNEL  = f"{REDIS_PREFIX}:notify"
EXCHANGE        = "travel"
QUEUE           = "tdtp.coordinator"
TRAVEL_DIR      = Path(__file__).parent
TDTPCLI_DEFAULT = str(Path(__file__).parents[2] / "tdtpcli.exe")
DEBOUNCE_TTL    = 10
DEFAULT_CURSOR  = "2020-01-01 00:00:00"

# в”Ђв”Ђв”Ђ Routing table в”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђ
# table       вЂ” РёРјСЏ С‚Р°Р±Р»РёС†С‹ РёР»Рё VIEW РІ source DB
# fields      вЂ” РєРѕР»РѕРЅРєРё РґР»СЏ --fields (None = РІСЃРµ)
# incremental вЂ” РїРѕР»Рµ РґР»СЏ РєСѓСЂСЃРѕСЂР° РёРЅРєСЂРµРјРµРЅС‚Р°Р»Р° (None = РїРѕР»РЅР°СЏ РІС‹РіСЂСѓР·РєР°)
# src_cfg     вЂ” РёРјСЏ config С„Р°Р№Р»Р° (РІ TRAVEL_DIR) СЃ source DB + broker settings
# queue       вЂ” named RabbitMQ queue
# label       вЂ” С‡РёС‚Р°РµРјРѕРµ РЅР°Р·РІР°РЅРёРµ

ROUTE_MAP: dict[str, list[dict]] = {
    "airline.flights.updated": [
        {
            "table":       "v_flights",
            "fields":      None,
            "incremental": "last_updated",
            "src_cfg":     "configs/config_src_tdtp_sync_flights.yaml",
            "queue":       "tdtp.sync.flights",
            "label":       "Airline в†’ Central: flights",
        },
    ],
    "airline.reservations.updated": [
        {
            "table":       "flight_reservations",
            "fields":      "reservation_id,flight_id,booking_ref_external,passenger_name,seat_class,price_paid,status,agency_id,last_updated",
            "incremental": "last_updated",
            "src_cfg":     "configs/config_src_tdtp_sync_reservations.yaml",
            "queue":       "tdtp.sync.reservations",
            "label":       "Airline в†’ Central: reservations",
        },
    ],
    "central.catalog.updated": [
        {
            "table":       "countries",
            "fields":      "country_id,country_code,country_name,continent,currency_code,is_visa_required",
            "incremental": None,
            "src_cfg":     "configs/config_src_tdtp_sync_countries.yaml",
            "queue":       "tdtp.sync.countries",
            "label":       "Central в†’ Branch: countries",
        },
        {
            "table":       "guides",
            "fields":      "guide_id,first_name,last_name,specialization,rating,languages,is_active",
            "incremental": "last_updated",
            "src_cfg":     "configs/config_src_tdtp_sync_guides.yaml",
            "queue":       "tdtp.sync.guides",
            "label":       "Central в†’ Branch: guides",
        },
        {
            "table":       "tours",
            "fields":      "tour_id,tour_code,tour_name,description,destination_country_id,duration_days,difficulty_level,max_group_size,base_price,is_active",
            "incremental": None,
            "src_cfg":     "configs/config_src_tdtp_sync_tours.yaml",
            "queue":       "tdtp.sync.tours",
            "label":       "Central в†’ Branch: tours",
        },
        {
            "table":       "tour_schedule",
            "fields":      "schedule_id,tour_id,guide_id,start_date,end_date,available_slots,booked_slots,price_modifier,status",
            "incremental": "last_updated",
            "src_cfg":     "configs/config_src_tdtp_sync_schedule.yaml",
            "queue":       "tdtp.sync.schedule",
            "label":       "Central в†’ Branch: schedule",
        },
    ],
    "branch.customers.registered": [
        {
            "table":       "v_local_customers",
            "fields":      None,
            "incremental": "last_updated",
            "src_cfg":     "configs/config_src_tdtp_sync_branch_customers.yaml",
            "queue":       "tdtp.sync.branch.customers",
            "label":       "Branch в†’ Central: customers",
        },
    ],
    "branch.sales.created": [
        {
            "table":       "v_local_sales",
            "fields":      None,
            "incremental": "last_updated",
            "src_cfg":     "configs/config_src_tdtp_sync_branch_sales.yaml",
            "queue":       "tdtp.sync.branch.sales",
            "label":       "Branch в†’ Central: sales",
        },
    ],
    "central.sales.changed":  [],
    "central.customers.new":  [],
}

# в”Ђв”Ђв”Ђ Logging в”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђ

def _ts():
    return datetime.now().strftime("%H:%M:%S")

def log_ok(tag, msg):   print(f"{Fore.WHITE}[{_ts()}] {Fore.GREEN}вњ“ {tag:<34}{Style.RESET_ALL} {msg}")
def log_err(tag, msg):  print(f"{Fore.WHITE}[{_ts()}] {Fore.RED}вњ— {tag:<34}{Style.RESET_ALL} {msg}")
def log_info(tag, msg): print(f"{Fore.WHITE}[{_ts()}] {Fore.CYAN}  {tag:<34}{Style.RESET_ALL} {msg}")
def log_skip(tag, msg): print(f"{Fore.WHITE}[{_ts()}] {Fore.WHITE}В· {tag:<34}{Style.RESET_ALL} {msg}")
def log_mq(tag, msg):   print(f"{Fore.WHITE}[{_ts()}] {Fore.MAGENTA}в‡ў {tag:<34}{Style.RESET_ALL} {msg}")

# в”Ђв”Ђв”Ђ Redis helpers в”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђ

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


# в”Ђв”Ђв”Ђ Export runner в”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђ

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
        # parse "Exported N packet(s)" в†’ actual rows from import will tell us
        # but for now estimate from output
        ok = result.returncode == 0
        return ok, elapsed, out, rows
    except subprocess.TimeoutExpired:
        return False, 120, "timeout (120s)", 0
    except FileNotFoundError:
        return False, 0, f"tdtpcli not found: {tdtpcli}", 0


# в”Ђв”Ђв”Ђ Core sync logic в”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђ

def sync_route(r: redis.Redis, tdtpcli: str, routing_key: str, entry: dict) -> bool:
    label      = entry["label"]
    cursor_key = routing_key.replace(".", "_") + "__" + entry["table"]
    cursor     = get_cursor(r, cursor_key)

    log_info(label, f"cursor={cursor}  в†’  {entry['queue']}")

    ok, elapsed, out, rows = run_export_broker(tdtpcli, entry, cursor)

    if not ok:
        log_err(label, f"FAILED {elapsed}s\n{out[:1000]}")
        set_state(r, cursor_key, {"status": "error", "error": out[:2000], "ts": datetime.now().isoformat()})
        return False

    log_ok(label, f"{elapsed}s в†’ {entry['queue']}")
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


# в”Ђв”Ђв”Ђ Message handler в”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђ

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


# в”Ђв”Ђв”Ђ Main в”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђв”Ђ

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
            print(f"    {rk:<42} в†’ {e['queue']}{inc}")
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
