#!/usr/bin/env python3
"""
Travel Agency Activity Simulator
Generates realistic MSSQL activity: new customers, bookings, schedule updates.
Logs counters and last-activity timestamp to Redis.

Usage:
    python activity.py [--interval 3] [--burst 3] [--redis localhost:6379]

Dependencies:
    pip install pyodbc redis colorama
"""

import argparse
import json
import random
import time
import uuid
from datetime import datetime, date, timedelta

import pyodbc
import redis
from colorama import Fore, Style, init

init(autoreset=True)

# ─── Config ──────────────────────────────────────────────────────────────────

MSSQL_DSN = (
    "DRIVER={ODBC Driver 17 for SQL Server};"
    "SERVER=sql-srv1;"
    "DATABASE=TravelAgency;"
    "Trusted_Connection=yes;"
    "Encrypt=no;"
)

REDIS_KEY_PREFIX = "tdtp:travel"

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
    "Garcia", "Martinez", "Robinson", "Clark", "Lewis", "Walker",
    "Kovalenko", "Shevchenko", "Bondarenko", "Tkachenko", "Kravchenko",
]
CITIES = [
    "Kyiv", "Lviv", "Odesa", "Kharkiv", "Dnipro", "Warsaw", "Berlin",
    "Paris", "London", "Madrid", "Rome", "Vienna", "Prague", "Krakow",
]
PAYMENT_METHODS = ["Credit Card", "Debit Card", "Bank Transfer", "PayPal", "Cash"]
PAYMENT_STATUSES = ["Pending", "Deposit Paid", "Fully Paid"]
SCHEDULE_STATUSES = ["Scheduled", "Confirmed", "In Progress", "Completed"]
ACTIVITIES_PREFS = [
    '{"dietary": "vegetarian", "activities": ["hiking", "cultural"]}',
    '{"dietary": "none", "activities": ["beach", "adventure"]}',
    '{"dietary": "vegan", "activities": ["eco-tourism", "photography"]}',
    '{"dietary": "halal", "activities": ["city-tours", "museums"]}',
    '{"dietary": "kosher", "activities": ["history", "architecture"]}',
]

# ─── DB helpers ──────────────────────────────────────────────────────────────

def connect_mssql():
    return pyodbc.connect(MSSQL_DSN, autocommit=False)


def rand_email(first, last):
    domains = ["gmail.com", "yahoo.com", "outlook.com", "travel.ua", "mail.com"]
    suffix = random.randint(100, 9999)
    return f"{first.lower()}.{last.lower()}{suffix}@{random.choice(domains)}"


def rand_date(start_year=1960, end_year=2000):
    start = date(start_year, 1, 1)
    end = date(end_year, 12, 31)
    return start + timedelta(days=random.randint(0, (end - start).days))


def rand_future_date(days_min=30, days_max=365):
    return date.today() + timedelta(days=random.randint(days_min, days_max))


# ─── Activity generators ─────────────────────────────────────────────────────

def insert_customer(conn) -> str:
    first = random.choice(FIRST_NAMES)
    last  = random.choice(LAST_NAMES)
    email = rand_email(first, last)
    dob   = rand_date()
    city  = random.choice(CITIES)

    # Pick random nationality
    cur = conn.cursor()
    cur.execute("SELECT TOP 1 country_id FROM countries ORDER BY NEWID()")
    row = cur.fetchone()
    if not row:
        return None
    country_id = row[0]

    cur.execute("""
        INSERT INTO customers
            (customer_uuid, first_name, last_name, email, phone,
             date_of_birth, nationality_country_id, city,
             loyalty_points, is_vip, preferences, last_updated)
        VALUES
            (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, GETDATE())
    """,
        str(uuid.uuid4()), first, last, email,
        f"+38050{random.randint(1000000, 9999999)}",
        dob, country_id, city,
        random.randint(0, 500),
        1 if random.random() < 0.05 else 0,
        random.choice(ACTIVITIES_PREFS),
    )
    conn.commit()
    return f"{first} {last} <{email}>"


def insert_booking(conn) -> str:
    cur = conn.cursor()

    # Random active schedule with free slots
    cur.execute("""
        SELECT TOP 1 ts.schedule_id, ts.tour_id, ts.available_slots, ts.booked_slots,
               t.base_price, ts.price_modifier
        FROM tour_schedule ts
        JOIN tours t ON ts.tour_id = t.tour_id
        WHERE ts.status IN ('Scheduled','Confirmed')
          AND ts.start_date > GETDATE()
          AND (ts.available_slots - ts.booked_slots) > 0
        ORDER BY NEWID()
    """)
    sched = cur.fetchone()
    if not sched:
        return None

    schedule_id, tour_id, avail, booked, base_price, modifier = sched
    travelers = random.randint(1, min(3, avail - booked))
    total = float(base_price) * float(modifier) * travelers
    deposit = round(total * 0.3, 2)
    ref = f"BK{datetime.now().strftime('%y%m%d')}{random.randint(1000,9999)}"

    # Random customer
    cur.execute("SELECT TOP 1 customer_id FROM customers ORDER BY NEWID()")
    cust = cur.fetchone()
    if not cust:
        return None

    cur.execute("""
        INSERT INTO tour_sales
            (booking_reference, schedule_id, customer_id,
             number_of_travelers, total_amount, deposit_paid, balance_due,
             payment_status, payment_method, insurance_purchased,
             sales_agent, last_updated)
        VALUES (?, ?, ?, ?, ?, ?, ?, 'Deposit Paid', ?, ?, ?, GETDATE())
    """,
        ref, schedule_id, cust[0],
        travelers, round(total, 2), deposit, round(total - deposit, 2),
        random.choice(PAYMENT_METHODS),
        1 if random.random() < 0.3 else 0,
        random.choice(["online", "agent_kyiv", "agent_lviv", "agent_odesa"]),
    )

    # Update booked_slots
    cur.execute("""
        UPDATE tour_schedule SET booked_slots = booked_slots + ?, last_updated = GETDATE()
        WHERE schedule_id = ?
    """, travelers, schedule_id)

    conn.commit()
    return f"{ref} schedule={schedule_id} travelers={travelers} total={total:.2f}"


def update_booking_status(conn) -> str:
    cur = conn.cursor()
    cur.execute("""
        SELECT TOP 1 sale_id, booking_reference, payment_status
        FROM tour_sales
        WHERE payment_status IN ('Pending', 'Deposit Paid')
        ORDER BY NEWID()
    """)
    row = cur.fetchone()
    if not row:
        return None

    sale_id, ref, status = row
    new_status = "Deposit Paid" if status == "Pending" else "Fully Paid"

    cur.execute("""
        UPDATE tour_sales SET payment_status = ?, last_updated = GETDATE()
        WHERE sale_id = ?
    """, new_status, sale_id)
    conn.commit()
    return f"{ref}: {status} → {new_status}"


def add_tour_schedule(conn) -> str:
    cur = conn.cursor()

    cur.execute("SELECT TOP 1 tour_id FROM tours WHERE is_active=1 ORDER BY NEWID()")
    tour = cur.fetchone()
    cur.execute("SELECT TOP 1 guide_id FROM guides WHERE is_active=1 ORDER BY NEWID()")
    guide = cur.fetchone()
    if not tour or not guide:
        return None

    start = rand_future_date(60, 300)
    cur.execute("""
        SELECT TOP 1 duration_days, max_group_size FROM tours WHERE tour_id = ?
    """, tour[0])
    t = cur.fetchone()
    if not t:
        return None
    duration, max_size = t

    end = start + timedelta(days=duration)
    slots = random.randint(max(4, max_size // 2), max_size)
    modifier = round(random.uniform(0.85, 1.25), 2)

    cur.execute("""
        INSERT INTO tour_schedule
            (tour_id, guide_id, start_date, end_date,
             available_slots, booked_slots, price_modifier,
             status, last_updated)
        VALUES (?, ?, ?, ?, ?, 0, ?, 'Scheduled', GETDATE())
    """, tour[0], guide[0], start, end, slots, modifier)
    conn.commit()
    return f"tour={tour[0]} guide={guide[0]} {start}→{end} slots={slots} x{modifier}"


def update_guide_rating(conn) -> str:
    cur = conn.cursor()
    cur.execute("SELECT TOP 1 guide_id, first_name, last_name FROM guides ORDER BY NEWID()")
    row = cur.fetchone()
    if not row:
        return None

    guide_id, first, last = row
    delta = round(random.uniform(-0.1, 0.15), 2)
    cur.execute("""
        UPDATE guides
        SET rating = ROUND(CASE WHEN rating + ? > 5 THEN 5
                                WHEN rating + ? < 0 THEN 0
                                ELSE rating + ? END, 2),
            last_updated = GETDATE()
        WHERE guide_id = ?
    """, delta, delta, delta, guide_id)
    conn.commit()
    sign = "+" if delta >= 0 else ""
    return f"{first} {last}: rating {sign}{delta}"


def cancel_booking(conn) -> str:
    cur = conn.cursor()
    cur.execute("""
        SELECT TOP 1 sale_id, booking_reference, total_amount
        FROM tour_sales
        WHERE payment_status = 'Deposit Paid'
          AND booking_date < DATEADD(day, -1, GETDATE())
        ORDER BY NEWID()
    """)
    row = cur.fetchone()
    if not row:
        return None

    sale_id, ref, total = row
    refund = round(float(total) * random.uniform(0.0, 0.5), 2)
    cur.execute("""
        UPDATE tour_sales
        SET payment_status = 'Cancelled',
            cancellation_date = GETDATE(),
            cancellation_reason = ?,
            refund_amount = ?,
            last_updated = GETDATE()
        WHERE sale_id = ?
    """, random.choice([
        "Customer request", "Health reasons",
        "Schedule conflict", "Force majeure",
    ]), refund, sale_id)
    conn.commit()
    return f"{ref} refund={refund:.2f}"


# ─── Activity dispatch ────────────────────────────────────────────────────────

ACTIVITIES = [
    ("NEW_CUSTOMER",     insert_customer,      Fore.GREEN,   40),
    ("NEW_BOOKING",      insert_booking,       Fore.CYAN,    35),
    ("PAYMENT_UPDATE",   update_booking_status,Fore.YELLOW,  30),
    ("NEW_SCHEDULE",     add_tour_schedule,    Fore.BLUE,    15),
    ("GUIDE_RATING",     update_guide_rating,  Fore.MAGENTA, 20),
    ("CANCEL_BOOKING",   cancel_booking,       Fore.RED,     8),
]

WEIGHTS = [a[3] for a in ACTIVITIES]


def tick(conn, r: redis.Redis, burst: int):
    chosen = random.choices(ACTIVITIES, weights=WEIGHTS, k=burst)
    for name, fn, color, _ in chosen:
        try:
            detail = fn(conn)
            if detail is None:
                continue
            ts = datetime.now().strftime("%H:%M:%S")
            print(f"{Fore.WHITE}[{ts}] {color}{name:<18}{Style.RESET_ALL} {detail}")

            # Redis counters
            r.hincrby(f"{REDIS_KEY_PREFIX}:activity:counts", name, 1)
            r.hset(f"{REDIS_KEY_PREFIX}:activity:last", name,
                   datetime.now().isoformat())
            r.set(f"{REDIS_KEY_PREFIX}:activity:alive",
                  datetime.now().isoformat(), ex=60)
        except Exception as e:
            print(f"{Fore.RED}[ERROR] {name}: {e}{Style.RESET_ALL}")
            try:
                conn.rollback()
            except Exception:
                pass


# ─── Main ────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description="Travel Agency activity simulator")
    parser.add_argument("--interval", type=float, default=3.0,
                        help="Seconds between ticks (default: 3)")
    parser.add_argument("--burst", type=int, default=2,
                        help="Activities per tick (default: 2)")
    parser.add_argument("--redis", default="localhost:6379",
                        help="Redis address (default: localhost:6379)")
    args = parser.parse_args()

    host, port = args.redis.rsplit(":", 1)
    r = redis.Redis(host=host, port=int(port), decode_responses=True)
    r.ping()

    print(f"{Fore.GREEN}Travel Agency Activity Simulator{Style.RESET_ALL}")
    print(f"  Interval : {args.interval}s | Burst: {args.burst} | Redis: {args.redis}")
    print(f"  Press Ctrl+C to stop\n")

    conn = connect_mssql()
    print(f"{Fore.GREEN}✓ Connected to MSSQL TravelAgency{Style.RESET_ALL}\n")

    cycle = 0
    try:
        while True:
            cycle += 1
            tick(conn, r, args.burst)
            if cycle % 20 == 0:
                counts = r.hgetall(f"{REDIS_KEY_PREFIX}:activity:counts")
                total = sum(int(v) for v in counts.values())
                print(f"\n{Fore.WHITE}── Stats (total {total} events) ──{Style.RESET_ALL}")
                for k, v in sorted(counts.items()):
                    print(f"  {k:<20} {v}")
                print()
            time.sleep(args.interval)
    except KeyboardInterrupt:
        print(f"\n{Fore.YELLOW}Stopped.{Style.RESET_ALL}")
    finally:
        conn.close()


if __name__ == "__main__":
    main()
