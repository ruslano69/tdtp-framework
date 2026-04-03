#!/usr/bin/env python3
"""
Travel Agency Activity Simulator — Multi-Node
Симулирует живую активность в трёх узлах и публикует события в RabbitMQ.

Nodes:
  central  — центральный офис (localhost:5432, db=tdtp)
  branch   — филиал           (localhost:5433, db=tdtp_branch)
  airline  — авиакомпания     (localhost:5434, db=tdtp_airline)

После каждой успешной операции публикуется событие в RabbitMQ:
  Exchange: travel (topic, durable)
  Routing keys:
    airline.flights.updated        — новый рейс или смена статуса
    airline.reservations.updated   — новая бронь / отмена
    central.catalog.updated        — изменение расписания или рейтинга гида
    central.sales.changed          — новая продажа / статус оплаты
    branch.sales.created           — новая продажа в филиале
    branch.customers.registered    — новый клиент в филиале

Usage:
    python activity.py --node central  [--pg-dsn "..."] [--amqp "..."] [--redis "..."]
    python activity.py --node branch
    python activity.py --node airline
    python activity.py --node central --interval 2 --burst 3

Dependencies:
    pip install psycopg2-binary redis pika colorama
"""

import argparse
import json
import random
import time
import uuid
from datetime import datetime, date, timedelta

import psycopg2
import psycopg2.extras
import pika
import redis
from colorama import Fore, Style, init

init(autoreset=True)

# ─── Node defaults ────────────────────────────────────────────────────────────

NODE_DEFAULTS = {
    "central": "host=localhost port=5432 dbname=tdtp user=tdtp password=tdtp",
    "branch":  "host=localhost port=5433 dbname=tdtp_branch user=tdtp password=tdtp",
    "airline": "host=localhost port=5434 dbname=tdtp_airline user=tdtp password=tdtp",
}

AMQP_DEFAULT  = "amqp://tdtp:tdtp@localhost:5672/"
REDIS_DEFAULT = "localhost:6379"
EXCHANGE      = "travel"
REDIS_PREFIX  = "tdtp:travel"

# ─── Fake data pools ─────────────────────────────────────────────────────────

FIRST_NAMES = [
    "Alice", "Bob", "Charlie", "Diana", "Eve", "Frank", "Grace", "Hank",
    "Iris", "Jack", "Kate", "Leo", "Mia", "Nick", "Olivia", "Paul",
    "Quinn", "Rose", "Sam", "Tina", "Uma", "Victor", "Wendy", "Xander",
    "Yara", "Zoe", "Artem", "Daria", "Ivan", "Oksana", "Mykola", "Sofia",
]
LAST_NAMES = [
    "Smith", "Jones", "Brown", "Davis", "Miller", "Wilson", "Moore",
    "Taylor", "Anderson", "Thomas", "Jackson", "White", "Harris", "Martin",
    "Kovalenko", "Shevchenko", "Bondarenko", "Tkachenko", "Kravchenko",
]
CITIES = [
    "Kyiv", "Lviv", "Odesa", "Kharkiv", "Dnipro",
    "Warsaw", "Berlin", "Paris", "London", "Madrid", "Rome",
]
PAYMENT_METHODS  = ["Credit Card", "Debit Card", "Bank Transfer", "PayPal", "Cash"]
ACTIVITIES_PREFS = [
    '{"dietary": "vegetarian", "activities": ["hiking", "cultural"]}',
    '{"dietary": "none",       "activities": ["beach", "adventure"]}',
    '{"dietary": "vegan",      "activities": ["eco-tourism", "photography"]}',
    '{"dietary": "halal",      "activities": ["city-tours", "museums"]}',
]
CANCELLATION_REASONS = [
    "Customer request", "Health reasons", "Schedule conflict", "Force majeure",
]
FLIGHT_STATUSES = ["Scheduled", "Boarding", "Departed", "Arrived", "Delayed"]

# ─── DB / MQ helpers ─────────────────────────────────────────────────────────

def connect_pg(dsn: str):
    conn = psycopg2.connect(dsn)
    conn.autocommit = False
    return conn


def rand_email(first: str, last: str) -> str:
    domains = ["gmail.com", "yahoo.com", "outlook.com", "travel.ua", "mail.com"]
    return f"{first.lower()}.{last.lower()}{random.randint(100, 9999)}@{random.choice(domains)}"


def rand_dob(start_year=1960, end_year=2000) -> date:
    s = date(start_year, 1, 1)
    e = date(end_year, 12, 31)
    return s + timedelta(days=random.randint(0, (e - s).days))


def rand_future_date(days_min=30, days_max=365) -> date:
    return date.today() + timedelta(days=random.randint(days_min, days_max))


# ─── RabbitMQ publisher (lazy connection) ────────────────────────────────────

class MQPublisher:
    def __init__(self, amqp_url: str):
        self.amqp_url = amqp_url
        self._conn    = None
        self._channel = None

    def _ensure(self):
        if self._conn and not self._conn.is_closed:
            return
        params = pika.URLParameters(self.amqp_url)
        params.socket_timeout = 5
        self._conn    = pika.BlockingConnection(params)
        self._channel = self._conn.channel()
        self._channel.exchange_declare(
            exchange=EXCHANGE, exchange_type="topic", durable=True
        )

    def publish(self, routing_key: str, payload: dict):
        try:
            self._ensure()
            self._channel.basic_publish(
                exchange=EXCHANGE,
                routing_key=routing_key,
                body=json.dumps(payload),
                properties=pika.BasicProperties(
                    content_type="application/json",
                    delivery_mode=2,  # persistent
                ),
            )
        except Exception as exc:
            print(f"{Fore.YELLOW}[MQ] {routing_key}: {exc}{Style.RESET_ALL}")
            self._conn = None  # force reconnect next call


# ═══════════════════════════════════════════════════════════════════════════════
# NODE: central
# ═══════════════════════════════════════════════════════════════════════════════

def central_insert_customer(conn) -> tuple[str, str]:
    first, last = random.choice(FIRST_NAMES), random.choice(LAST_NAMES)
    email = rand_email(first, last)
    cur = conn.cursor()
    cur.execute("SELECT country_id FROM countries ORDER BY RANDOM() LIMIT 1")
    row = cur.fetchone()
    if not row:
        return None, None
    cur.execute("""
        INSERT INTO customers
            (customer_uuid, first_name, last_name, email, phone,
             date_of_birth, nationality_country_id, city,
             loyalty_points, is_vip, preferences, last_updated)
        VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,NOW())
    """, (
        str(uuid.uuid4()), first, last, email,
        f"+38050{random.randint(1000000, 9999999)}",
        rand_dob(), row[0], random.choice(CITIES),
        random.randint(0, 500), random.random() < 0.05,
        random.choice(ACTIVITIES_PREFS),
    ))
    conn.commit()
    return f"{first} {last} <{email}>", "central.customers.new"


def central_insert_booking(conn) -> tuple[str, str]:
    cur = conn.cursor()
    cur.execute("""
        SELECT ts.schedule_id, t.base_price, ts.price_modifier,
               ts.available_slots - ts.booked_slots AS free
        FROM tour_schedule ts
        JOIN tours t ON ts.tour_id = t.tour_id
        WHERE ts.status IN ('Scheduled','Confirmed')
          AND ts.start_date > NOW()
          AND (ts.available_slots - ts.booked_slots) > 0
        ORDER BY RANDOM() LIMIT 1
    """)
    sched = cur.fetchone()
    if not sched:
        return None, None
    schedule_id, base_price, modifier, free = sched
    travelers = random.randint(1, min(3, free))
    total     = round(float(base_price) * float(modifier) * travelers, 2)
    deposit   = round(total * 0.3, 2)
    ref = f"BK{datetime.now().strftime('%y%m%d')}{random.randint(1000,9999)}"

    cur.execute("SELECT customer_id FROM customers ORDER BY RANDOM() LIMIT 1")
    cust = cur.fetchone()
    if not cust:
        return None, None

    cur.execute("""
        INSERT INTO tour_sales
            (booking_reference, schedule_id, customer_id,
             number_of_travelers, total_amount, deposit_paid, balance_due,
             payment_status, payment_method, insurance_purchased,
             sales_agent, last_updated)
        VALUES (%s,%s,%s,%s,%s,%s,%s,'Deposit Paid',%s,%s,%s,NOW())
    """, (
        ref, schedule_id, cust[0],
        travelers, total, deposit, round(total - deposit, 2),
        random.choice(PAYMENT_METHODS),
        random.random() < 0.3,
        random.choice(["online", "agent_kyiv", "agent_lviv"]),
    ))
    cur.execute("""
        UPDATE tour_schedule
        SET booked_slots = booked_slots + %s, last_updated = NOW()
        WHERE schedule_id = %s
    """, (travelers, schedule_id))
    conn.commit()
    return f"{ref} sched={schedule_id} x{travelers} total={total}", "central.sales.changed"


def central_update_payment(conn) -> tuple[str, str]:
    cur = conn.cursor()
    cur.execute("""
        SELECT sale_id, booking_reference, payment_status FROM tour_sales
        WHERE payment_status IN ('Pending','Deposit Paid')
        ORDER BY RANDOM() LIMIT 1
    """)
    row = cur.fetchone()
    if not row:
        return None, None
    sale_id, ref, status = row
    new_status = "Deposit Paid" if status == "Pending" else "Fully Paid"
    cur.execute("""
        UPDATE tour_sales SET payment_status=%s, last_updated=NOW() WHERE sale_id=%s
    """, (new_status, sale_id))
    conn.commit()
    return f"{ref}: {status} → {new_status}", "central.sales.changed"


def central_add_schedule(conn) -> tuple[str, str]:
    cur = conn.cursor()
    cur.execute("SELECT tour_id, duration_days, max_group_size FROM tours WHERE is_active = TRUE ORDER BY RANDOM() LIMIT 1")
    tour = cur.fetchone()
    cur.execute("SELECT guide_id FROM guides WHERE is_active = TRUE ORDER BY RANDOM() LIMIT 1")
    guide = cur.fetchone()
    if not tour or not guide:
        return None, None
    tour_id, duration, max_size = tour
    start    = rand_future_date(60, 300)
    end      = start + timedelta(days=duration)
    slots    = random.randint(max(4, max_size // 2), max_size)
    modifier = round(random.uniform(0.85, 1.25), 2)
    cur.execute("""
        INSERT INTO tour_schedule
            (tour_id, guide_id, start_date, end_date,
             available_slots, booked_slots, price_modifier, status, last_updated)
        VALUES (%s,%s,%s,%s,%s,0,%s,'Scheduled',NOW())
    """, (tour_id, guide[0], start, end, slots, modifier))
    conn.commit()
    return f"tour={tour_id} {start}→{end} slots={slots} x{modifier}", "central.catalog.updated"


def central_update_guide_rating(conn) -> tuple[str, str]:
    cur = conn.cursor()
    cur.execute("SELECT guide_id, first_name, last_name FROM guides ORDER BY RANDOM() LIMIT 1")
    row = cur.fetchone()
    if not row:
        return None, None
    guide_id, first, last = row
    delta = round(random.uniform(-0.1, 0.15), 2)
    cur.execute("""
        UPDATE guides
        SET rating = ROUND(LEAST(GREATEST(COALESCE(rating,3.0) + %s, 0), 5), 2),
            last_updated = NOW()
        WHERE guide_id = %s
    """, (delta, guide_id))
    conn.commit()
    sign = "+" if delta >= 0 else ""
    return f"{first} {last}: {sign}{delta}", "central.catalog.updated"


def central_cancel_booking(conn) -> tuple[str, str]:
    cur = conn.cursor()
    cur.execute("""
        SELECT sale_id, booking_reference, total_amount FROM tour_sales
        WHERE payment_status = 'Deposit Paid'
          AND booking_date < NOW() - INTERVAL '1 day'
        ORDER BY RANDOM() LIMIT 1
    """)
    row = cur.fetchone()
    if not row:
        return None, None
    sale_id, ref, total = row
    refund = round(float(total) * random.uniform(0.0, 0.5), 2)
    cur.execute("""
        UPDATE tour_sales
        SET payment_status='Cancelled', cancellation_date=NOW(),
            cancellation_reason=%s, refund_amount=%s, last_updated=NOW()
        WHERE sale_id=%s
    """, (random.choice(CANCELLATION_REASONS), refund, sale_id))
    conn.commit()
    return f"{ref} refund={refund:.2f}", "central.sales.changed"


CENTRAL_ACTIVITIES = [
    ("NEW_CUSTOMER",   central_insert_customer,    Fore.GREEN,   40),
    ("NEW_BOOKING",    central_insert_booking,     Fore.CYAN,    35),
    ("PAYMENT_UPDATE", central_update_payment,     Fore.YELLOW,  30),
    ("NEW_SCHEDULE",   central_add_schedule,       Fore.BLUE,    15),
    ("GUIDE_RATING",   central_update_guide_rating,Fore.MAGENTA, 20),
    ("CANCEL_BOOKING", central_cancel_booking,     Fore.RED,      8),
]

# ═══════════════════════════════════════════════════════════════════════════════
# NODE: airline
# ═══════════════════════════════════════════════════════════════════════════════

def airline_add_flight(conn) -> tuple[str, str]:
    cur = conn.cursor()
    cur.execute("""
        SELECT aircraft_id, economy_seats, business_seats
        FROM aircraft WHERE is_active = TRUE ORDER BY RANDOM() LIMIT 1
    """)
    ac = cur.fetchone()
    if not ac:
        return None, None
    aircraft_id, econ_seats, biz_seats = ac

    cur.execute("SELECT airport_id, iata_code FROM airports ORDER BY RANDOM() LIMIT 2")
    airports = cur.fetchall()
    if len(airports) < 2 or airports[0][0] == airports[1][0]:
        return None, None
    origin_id,   origin_iata   = airports[0]
    dest_id,     dest_iata     = airports[1]

    departure = datetime.now() + timedelta(
        days=random.randint(1, 90),
        hours=random.randint(5, 22),
    )
    duration  = random.randint(60, 480)
    arrival   = departure + timedelta(minutes=duration)
    flight_num = f"ZT{random.randint(100, 999)}"
    econ_price = round(random.uniform(50, 500), 2)
    biz_price  = round(econ_price * random.uniform(2.0, 4.0), 2)

    cur.execute("""
        INSERT INTO flights
            (flight_number, origin_airport_id, destination_airport_id,
             departure_time, arrival_time, duration_minutes,
             aircraft_id, economy_price, business_price,
             available_economy, available_business, status, last_updated)
        VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,'Scheduled',NOW())
        RETURNING flight_id
    """, (flight_num, origin_id, dest_id, departure, arrival, duration,
          aircraft_id, econ_price, biz_price, econ_seats, biz_seats))
    fid = cur.fetchone()[0]
    conn.commit()
    return (f"{flight_num} #{fid} {origin_iata}→{dest_iata} "
            f"dep={departure.strftime('%m-%d %H:%M')} €{econ_price}"),\
           "airline.flights.updated"


def airline_update_flight_status(conn) -> tuple[str, str]:
    cur = conn.cursor()
    # Status progression: Scheduled→Boarding→Departed→Arrived (or Delayed)
    cur.execute("""
        SELECT flight_id, flight_number, status
        FROM flights
        WHERE status IN ('Scheduled','Boarding','Delayed')
          AND departure_time > NOW() - INTERVAL '6 hours'
        ORDER BY RANDOM() LIMIT 1
    """)
    row = cur.fetchone()
    if not row:
        return None, None
    fid, fnum, status = row
    next_status = {
        "Scheduled": random.choice(["Boarding", "Delayed"]),
        "Boarding":  "Departed",
        "Delayed":   random.choice(["Boarding", "Cancelled"]),
    }.get(status, "Arrived")
    delay = random.randint(15, 120) if next_status == "Delayed" else 0
    cur.execute("""
        UPDATE flights SET status=%s, delay_minutes=%s, last_updated=NOW()
        WHERE flight_id=%s
    """, (next_status, delay, fid))
    conn.commit()
    delay_str = f" (+{delay}min)" if delay else ""
    return f"{fnum} #{fid}: {status} → {next_status}{delay_str}", "airline.flights.updated"


def airline_add_reservation(conn) -> tuple[str, str]:
    cur = conn.cursor()
    cur.execute("""
        SELECT flight_id, flight_number, available_economy
        FROM flights
        WHERE status = 'Scheduled' AND available_economy > 0
          AND departure_time > NOW()
        ORDER BY RANDOM() LIMIT 1
    """)
    flight = cur.fetchone()
    if not flight:
        return None, None
    fid, fnum, avail = flight
    seats      = random.randint(1, min(4, avail))
    seat_class = "Economy"
    first, last = random.choice(FIRST_NAMES), random.choice(LAST_NAMES)
    price  = round(random.uniform(80, 450), 2)
    ref    = f"BK{datetime.now().strftime('%y%m%d')}{random.randint(1000, 9999)}"

    cur.execute("""
        INSERT INTO flight_reservations
            (flight_id, booking_ref_external, passenger_name,
             seat_class, price_paid, status, agency_id, last_updated)
        VALUES (%s,%s,%s,%s,%s,'Confirmed','TRAVEL_CENTRAL',NOW())
        RETURNING reservation_id
    """, (fid, ref, f"{first} {last}", seat_class, price))
    rid = cur.fetchone()[0]
    cur.execute("""
        UPDATE flights SET available_economy = available_economy - %s, last_updated=NOW()
        WHERE flight_id = %s
    """, (seats, fid))
    conn.commit()
    return f"#{rid} {fnum} {first} {last} {seat_class} €{price}", "airline.reservations.updated"


def airline_cancel_reservation(conn) -> tuple[str, str]:
    cur = conn.cursor()
    cur.execute("""
        SELECT r.reservation_id, r.booking_ref_external, r.passenger_name, r.flight_id
        FROM flight_reservations r
        JOIN flights f ON r.flight_id = f.flight_id
        WHERE r.status = 'Confirmed'
          AND f.departure_time > NOW() + INTERVAL '2 days'
        ORDER BY RANDOM() LIMIT 1
    """)
    row = cur.fetchone()
    if not row:
        return None, None
    rid, ref, name, fid = row
    cur.execute("""
        UPDATE flight_reservations SET status='Cancelled', last_updated=NOW()
        WHERE reservation_id=%s
    """, (rid,))
    cur.execute("""
        UPDATE flights SET available_economy = available_economy + 1, last_updated=NOW()
        WHERE flight_id=%s
    """, (fid,))
    conn.commit()
    return f"#{rid} {ref} ({name})", "airline.reservations.updated"


AIRLINE_ACTIVITIES = [
    ("NEW_FLIGHT",       airline_add_flight,           Fore.BLUE,    30),
    ("FLIGHT_STATUS",    airline_update_flight_status, Fore.YELLOW,  40),
    ("NEW_RESERVATION",  airline_add_reservation,      Fore.CYAN,    35),
    ("CANCEL_RESERV",    airline_cancel_reservation,   Fore.RED,     10),
]

# ═══════════════════════════════════════════════════════════════════════════════
# NODE: branch
# ═══════════════════════════════════════════════════════════════════════════════

def branch_register_customer(conn) -> tuple[str, str]:
    first, last = random.choice(FIRST_NAMES), random.choice(LAST_NAMES)
    email = rand_email(first, last)
    cur = conn.cursor()
    cur.execute("SELECT country_id FROM countries_cache ORDER BY RANDOM() LIMIT 1")
    row = cur.fetchone()
    country_id = row[0] if row else None
    cur.execute("""
        INSERT INTO local_customers
            (customer_uuid, first_name, last_name, email, phone,
             date_of_birth, nationality_country_id, city,
             preferences, registered_by, last_updated)
        VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,NOW())
    """, (
        str(uuid.uuid4()), first, last, email,
        f"+38067{random.randint(1000000, 9999999)}",
        rand_dob(), country_id, random.choice(CITIES),
        random.choice(ACTIVITIES_PREFS),
        random.choice(["agent_lviv", "agent_uzhhorod", "agent_ivano"]),
    ))
    conn.commit()
    return f"{first} {last} <{email}>", "branch.customers.registered"


def branch_create_sale(conn) -> tuple[str, str]:
    cur = conn.cursor()
    cur.execute("""
        SELECT sc.schedule_id, tc.base_price, sc.price_modifier,
               sc.available_slots - sc.booked_slots AS free
        FROM schedule_cache sc
        JOIN tours_cache tc ON sc.tour_id = tc.tour_id
        WHERE sc.status IN ('Scheduled','Confirmed')
          AND sc.start_date > NOW()
          AND (sc.available_slots - sc.booked_slots) > 0
        ORDER BY RANDOM() LIMIT 1
    """)
    sched = cur.fetchone()
    if not sched:
        return None, None
    schedule_id, base_price, modifier, free = sched

    cur.execute("SELECT customer_uuid FROM local_customers ORDER BY RANDOM() LIMIT 1")
    cust = cur.fetchone()
    if not cust:
        return None, None

    travelers = random.randint(1, min(3, free))
    total     = round(float(base_price) * float(modifier) * travelers, 2)
    deposit   = round(total * 0.3, 2)
    ref = f"BR{datetime.now().strftime('%y%m%d')}{random.randint(1000, 9999)}"

    cur.execute("""
        INSERT INTO local_sales
            (booking_reference, schedule_id, customer_uuid,
             number_of_travelers, total_amount, deposit_paid, balance_due,
             payment_status, payment_method, insurance_purchased,
             sales_agent, last_updated)
        VALUES (%s,%s,%s,%s,%s,%s,%s,'Deposit Paid',%s,%s,%s,NOW())
    """, (
        ref, schedule_id, cust[0],
        travelers, total, deposit, round(total - deposit, 2),
        random.choice(PAYMENT_METHODS),
        random.random() < 0.25,
        random.choice(["agent_lviv", "agent_uzhhorod"]),
    ))
    conn.commit()
    return f"{ref} sched={schedule_id} x{travelers} total={total}", "branch.sales.created"


def branch_update_sale_payment(conn) -> tuple[str, str]:
    cur = conn.cursor()
    cur.execute("""
        SELECT local_sale_id, booking_reference, payment_status
        FROM local_sales
        WHERE payment_status IN ('Pending','Deposit Paid')
        ORDER BY RANDOM() LIMIT 1
    """)
    row = cur.fetchone()
    if not row:
        return None, None
    sid, ref, status = row
    new_status = "Deposit Paid" if status == "Pending" else "Fully Paid"
    cur.execute("""
        UPDATE local_sales SET payment_status=%s, last_updated=NOW()
        WHERE local_sale_id=%s
    """, (new_status, sid))
    conn.commit()
    return f"{ref}: {status} → {new_status}", "branch.sales.created"


def branch_cancel_sale(conn) -> tuple[str, str]:
    cur = conn.cursor()
    cur.execute("""
        SELECT local_sale_id, booking_reference, total_amount
        FROM local_sales
        WHERE payment_status = 'Deposit Paid'
          AND created_at < NOW() - INTERVAL '1 day'
        ORDER BY RANDOM() LIMIT 1
    """)
    row = cur.fetchone()
    if not row:
        return None, None
    sid, ref, total = row
    refund = round(float(total) * random.uniform(0.0, 0.5), 2)
    cur.execute("""
        UPDATE local_sales
        SET payment_status='Cancelled', cancellation_date=NOW(),
            cancellation_reason=%s, refund_amount=%s, last_updated=NOW()
        WHERE local_sale_id=%s
    """, (random.choice(CANCELLATION_REASONS), refund, sid))
    conn.commit()
    return f"{ref} refund={refund:.2f}", "branch.sales.created"


BRANCH_ACTIVITIES = [
    ("NEW_CUSTOMER",     branch_register_customer,  Fore.GREEN,  30),
    ("NEW_SALE",         branch_create_sale,        Fore.CYAN,   40),
    ("PAYMENT_UPDATE",   branch_update_sale_payment,Fore.YELLOW, 25),
    ("CANCEL_SALE",      branch_cancel_sale,        Fore.RED,     8),
]

# ─── Dispatch table ───────────────────────────────────────────────────────────

NODE_ACTIVITIES = {
    "central": CENTRAL_ACTIVITIES,
    "airline": AIRLINE_ACTIVITIES,
    "branch":  BRANCH_ACTIVITIES,
}

# ─── Main tick ───────────────────────────────────────────────────────────────

def tick(node: str, conn, mq: MQPublisher, r: redis.Redis, burst: int):
    activities = NODE_ACTIVITIES[node]
    weights    = [a[3] for a in activities]
    chosen     = random.choices(activities, weights=weights, k=burst)

    for name, fn, color, _ in chosen:
        try:
            detail, routing_key = fn(conn)
            if detail is None:
                continue
            ts = datetime.now().strftime("%H:%M:%S")
            node_tag = f"[{node.upper()[:3]}]"
            print(f"{Fore.WHITE}[{ts}]{color}{node_tag} {name:<18}{Style.RESET_ALL} {detail}")

            mq.publish(routing_key, {
                "node": node,
                "event": name,
                "ts": datetime.now().isoformat(),
            })

            r.hincrby(f"{REDIS_PREFIX}:{node}:activity:counts", name, 1)
            r.hset(f"{REDIS_PREFIX}:{node}:activity:last", name, datetime.now().isoformat())
            r.set(f"{REDIS_PREFIX}:{node}:activity:alive", datetime.now().isoformat(), ex=60)

        except Exception as exc:
            print(f"{Fore.RED}[ERROR] {node}/{name}: {exc}{Style.RESET_ALL}")
            try:
                conn.rollback()
            except Exception:
                pass


# ─── Main ────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description="Travel Agency activity simulator")
    parser.add_argument("--node",     choices=["central", "branch", "airline"],
                        default="central", help="Which node to simulate (default: central)")
    parser.add_argument("--pg-dsn",  default=None,
                        help="PostgreSQL DSN (default: node-specific localhost)")
    parser.add_argument("--amqp",    default=AMQP_DEFAULT,
                        help=f"RabbitMQ AMQP URL (default: {AMQP_DEFAULT})")
    parser.add_argument("--redis",   default=REDIS_DEFAULT,
                        help=f"Redis address (default: {REDIS_DEFAULT})")
    parser.add_argument("--interval",type=float, default=3.0,
                        help="Seconds between ticks (default: 3)")
    parser.add_argument("--burst",   type=int,   default=2,
                        help="Activities per tick (default: 2)")
    args = parser.parse_args()

    dsn = args.pg_dsn or NODE_DEFAULTS[args.node]

    host, port = args.redis.rsplit(":", 1)
    r   = redis.Redis(host=host, port=int(port), decode_responses=True)
    r.ping()

    mq = MQPublisher(args.amqp)

    print(f"{Fore.GREEN}Travel Agency Activity Simulator — {args.node.upper()}{Style.RESET_ALL}")
    print(f"  PG  : {dsn}")
    print(f"  AMQP: {args.amqp}")
    print(f"  Redis: {args.redis}")
    print(f"  Interval: {args.interval}s  Burst: {args.burst}")
    print(f"  Press Ctrl+C to stop\n")

    conn  = connect_pg(dsn)
    cycle = 0
    try:
        while True:
            cycle += 1
            tick(args.node, conn, mq, r, args.burst)
            if cycle % 20 == 0:
                counts = r.hgetall(f"{REDIS_PREFIX}:{args.node}:activity:counts")
                total  = sum(int(v) for v in counts.values())
                print(f"\n{Fore.WHITE}── {args.node} stats (total {total}) ──{Style.RESET_ALL}")
                for k, v in sorted(counts.items()):
                    print(f"  {k:<22} {v}")
                print()
            time.sleep(args.interval)
    except KeyboardInterrupt:
        print(f"\n{Fore.YELLOW}Stopped.{Style.RESET_ALL}")
    finally:
        conn.close()


if __name__ == "__main__":
    main()
