#!/usr/bin/env python3
"""
Генератор операционного дня туристического агентства.

Имитирует кипучую деятельность нескольких филиалов за один рабочий день:

  • Новые бронирования               (booking)
  • Оплаты — полные и депозит+остаток (payment)
  • Отмены бронирований              (cancellation)
  • Возвраты (штрафные и безвозмездные)(refund)
  • Раздача новых пакетов туров       (package_open)
  • Создание PNR в GDS               (gds_pnr)

Все операции пишутся в БД tdtp_test и в operations_log (готово к экспорту через tdtpcli pipeline).

Запуск:
    python generate_ops_day.py                         # сегодня, все филиалы
    python generate_ops_day.py --date 2026-03-15       # конкретная дата
    python generate_ops_day.py --branch МСК            # только один филиал
    python generate_ops_day.py --scale 2.0             # двойной объём
    python generate_ops_day.py --dry-run               # только показать план

Требования:
    pip install psycopg2-binary

Связь со средой:
    tdtp_test   (travel_agency)  — основная БД
    tdtp_airline (GDS)           — для создания PNR (необязательна, --no-gds пропускает)
"""

import argparse
import json
import random
import string
import sys
import uuid
from datetime import datetime, timedelta, date, time as dtime

try:
    import psycopg2
    import psycopg2.extras
except ImportError:
    print('pip install psycopg2-binary')
    sys.exit(1)

# ─── конфиг ───────────────────────────────────────────────────────────────────

TA_CONFIG = {
    'host': 'localhost', 'port': 5432,
    'user': 'tdtp_user', 'password': 'tdtp_dev_pass_2025',
    'database': 'tdtp_test',
}
GDS_CONFIG = {
    'host': 'localhost', 'port': 5432,
    'user': 'tdtp_user', 'password': 'tdtp_dev_pass_2025',
    'database': 'tdtp_airline',
}

# Базовый объём операций за день на один филиал (scale=1.0)
BASE_BOOKINGS     = 12   # новых бронирований
BASE_PAYMENTS     = 18   # оплат (включая balance-платежи по старым)
BASE_CANCELLATIONS = 3   # отмен
BASE_REFUNDS       = 2   # возвратов
BASE_NEW_PACKAGES  = 4   # новых пакетов туров

PAYMENT_METHODS = ['card', 'card', 'card', 'cash', 'online', 'transfer']
CANCEL_REASONS  = ['client_request', 'client_request', 'no_visa',
                   'flight_cancelled', 'force_majeure', 'other']
MEAL_PLANS      = ['AI', 'AI', 'HB', 'BB']

FIRST_NAMES = ['Алексей','Дмитрий','Андрей','Сергей','Иван','Елена',
               'Ольга','Наталья','Татьяна','Мария','Максим','Николай',
               'Артём','Анна','Ирина','Светлана','Юлия','Екатерина',
               'Роман','Павел','Ксения','Валентина','Тимур','Руслан']
LAST_NAMES  = ['Иванов','Петров','Сидоров','Козлов','Новиков','Морозов',
               'Волков','Соловьёв','Попов','Лебедев','Смирнов','Кузнецов',
               'Федоров','Алексеев','Андреев','Тихонов','Степанов','Белов',
               'Иванова','Петрова','Сидорова','Козлова','Новикова','Морозова']

HOTELS = [
    ('Grand Palace',     5, 'resort'),
    ('Blue Lagoon',      4, 'resort'),
    ('Sunrise Beach',    4, 'beach'),
    ('Royal Garden',     5, 'luxury'),
    ('Palm Tree Inn',    3, 'budget'),
    ('Azure Sea Club',   4, 'club'),
    ('Crystal Waters',   5, 'spa'),
    ('Golden Sands',     4, 'family'),
    ('Coral Bay Hotel',  3, 'beach'),
    ('Paradise Resort',  5, 'all_inclusive'),
]

AIRLINES = [('Аэрофлот','SU'),('Победа','DP'),('S7 Airlines','S7'),
            ('Уральские авиалинии','U6'),('Nordwind','N4'),('Azur Air','ZF')]


# ─── утилиты ──────────────────────────────────────────────────────────────────

def _pnr() -> str:
    return ''.join(random.choices(string.ascii_uppercase + string.digits, k=6))

def _txn() -> str:
    return f'TXN{random.randint(10**9, 10**10-1)}'

def _rand_time(op_date: date, hour_from=8, hour_to=20) -> datetime:
    h = random.randint(hour_from, hour_to)
    m = random.randint(0, 59)
    s = random.randint(0, 59)
    return datetime.combine(op_date, dtime(h, m, s))


def _log_op(cur, branch_id, branch_code, op_type, entity_type, entity_id,
            op_date, op_time, operator, amount_rub, status, details: dict):
    cur.execute("""
        INSERT INTO operations_log
          (branch_id, branch_code, op_type, entity_type, entity_id,
           op_date, op_time, operator, amount_rub, status, details)
        VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)
    """, (branch_id, branch_code, op_type, entity_type, entity_id,
          op_date, op_time, operator,
          amount_rub, status, json.dumps(details, ensure_ascii=False, default=str)))


# ─── операции ─────────────────────────────────────────────────────────────────

def do_new_bookings(cur, branch, op_date: date, scale: float, gds_conn):
    """Создаёт новые бронирования для филиала."""
    branch_id, branch_code, branch_city = branch
    n = max(1, int(BASE_BOOKINGS * scale * random.uniform(0.7, 1.4)))

    # Берём активные пакеты с наличием мест в этом или других филиалах
    cur.execute("""
        SELECT tp.package_id, tp.branch_id, tp.price_rub, tp.seats_total,
               t.duration_nights, d.country, d.resort
        FROM tour_packages tp
        JOIN tours t ON t.tour_id = tp.tour_id
        JOIN destinations d ON d.dest_id = t.dest_id
        WHERE tp.status = 'active'
          AND tp.depart_date >= %s
          AND tp.seats_total - COALESCE(
              (SELECT SUM(persons) FROM bookings
               WHERE package_id = tp.package_id
               AND status IN ('confirmed','pending')), 0) > 0
        ORDER BY RANDOM()
        LIMIT %s
    """, (op_date, n * 3))
    packages = cur.fetchall()

    if not packages:
        return 0

    created = 0
    for pkg_id, pkg_branch_id, price, total_seats, nights, country, resort in packages[:n]:
        # Клиент — новый или существующий
        cur.execute("SELECT client_id FROM clients ORDER BY RANDOM() LIMIT 1")
        row = cur.fetchone()
        if not row or random.random() < 0.15:
            # Создаём нового клиента
            fn = random.choice(FIRST_NAMES)
            ln = random.choice(LAST_NAMES)
            client_id = str(uuid.uuid4())
            birth = date(1970, 1, 1) + timedelta(days=random.randint(0, 18000))
            cur.execute("""
                INSERT INTO clients
                  (client_id, full_name, phone, email,
                   passport_series, birth_date, preferred_branch)
                VALUES (%s,%s,%s,%s,%s,%s,%s)
            """, (client_id, f'{ln} {fn}',
                  f'+7{random.randint(9000000000,9999999999)}',
                  f'{fn.lower()}{random.randint(1,999)}@mail.ru',
                  f'{random.randint(1000,9999)} {random.randint(100000,999999)}',
                  birth, branch_id))
        else:
            client_id = str(row[0])

        persons  = random.choices([1, 2, 3], weights=[5, 3, 1])[0]
        total_rub = round(float(price) * persons, 2)
        status   = random.choices(
            ['confirmed', 'confirmed', 'confirmed', 'pending'],
            weights=[7, 7, 7, 2]
        )[0]

        booked_at = _rand_time(op_date, 9, 19)
        paid_at   = booked_at + timedelta(minutes=random.randint(10,120)) if status == 'confirmed' else None
        gds_pnr   = _pnr() if status == 'confirmed' else None

        cur.execute("""
            INSERT INTO bookings
              (package_id, branch_id, client_id, persons, total_rub,
               status, booked_at, paid_at, gds_pnr_code)
            VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s)
            RETURNING booking_id
        """, (pkg_id, branch_id, client_id, persons, total_rub,
              status, booked_at, paid_at, gds_pnr))
        booking_id = str(cur.fetchone()[0])

        _log_op(cur, branch_id, branch_code, 'booking',
                'booking', booking_id, op_date, booked_at,
                f'agent_{random.randint(1,8)}', total_rub if status == 'confirmed' else None,
                status, {'persons': persons, 'country': country,
                         'resort': resort, 'nights': nights, 'gds_pnr': gds_pnr})

        # Создать PNR в GDS если есть соединение
        if gds_conn and gds_pnr:
            _create_gds_pnr(gds_conn, gds_pnr, branch_code, booked_at,
                            persons, float(price))
        created += 1

    return created


def _create_gds_pnr(gds_conn, pnr_code, agency_code, created_at,
                    n_pax: int, price_rub: float):
    """Создаёт PNR в GDS (tdtp_airline) если доступна."""
    try:
        gcur = gds_conn.cursor()
        # Найдём любой подходящий рейс
        gcur.execute("""
            SELECT f.flight_id FROM gds_flights f
            JOIN gds_inventory i ON i.flight_id = f.flight_id
            WHERE f.flight_date > CURRENT_DATE
              AND f.status = 'scheduled'
              AND i.is_available = true
              AND i.seats_total - i.seats_sold > %s
            ORDER BY RANDOM() LIMIT 1
        """, (n_pax,))
        row = gcur.fetchone()
        if not row:
            return
        flight_id = row[0]

        gcur.execute("""
            SELECT fare_class, price_rub, fare_basis, baggage_kg
            FROM gds_inventory
            WHERE flight_id = %s AND is_available = true
            ORDER BY price_rub LIMIT 3
        """, (flight_id,))
        inv = gcur.fetchall()
        if not inv:
            return
        fare_class, inv_price, fare_basis, baggage = random.choice(inv)

        total_amount = round(inv_price * n_pax, 2)
        ttl = created_at + timedelta(hours=48)
        gcur.execute("""
            INSERT INTO gds_pnr
              (pnr_code, created_at, agency_code, agent_id,
               status, ticket_time_limit, total_amount_rub)
            VALUES (%s,%s,%s,%s,'active',%s,%s)
            ON CONFLICT (pnr_code) DO NOTHING
        """, (pnr_code, created_at, agency_code,
              f'AGT{random.randint(100,999)}', ttl, total_amount))

        for _ in range(n_pax):
            ln = random.choice(['IVANOV','PETROV','SIDOROV','NOVIKOV','KOZLOV'])
            fn = random.choice(['ALEXEI','DMITRY','SERGEI','IVAN','ELENA'])
            birth = date(1975, 1, 1) + timedelta(days=random.randint(0, 15000))
            doc = f'{random.randint(1000,9999)} {random.randint(100000,999999)}'
            gcur.execute("""
                INSERT INTO gds_pnr_passengers
                  (pnr_code, last_name, first_name, birth_date,
                   gender, doc_type, doc_number, doc_expires, nationality)
                VALUES (%s,%s,%s,%s,%s,'passport',%s,%s,'RU')
            """, (pnr_code, ln, fn, birth,
                  random.choice('MF'), doc,
                  date.today() + timedelta(days=random.randint(365,1800))))

            seat = f'{random.randint(10,36)}{random.choice("ABCDEF")}'
            gcur.execute("""
                INSERT INTO gds_pnr_segments
                  (pnr_code, flight_id, fare_class, seat_number,
                   baggage_kg, status)
                VALUES (%s,%s,%s,%s,%s,'confirmed')
            """, (pnr_code, flight_id, fare_class, seat, baggage))

        gcur.execute("""
            UPDATE gds_inventory
            SET seats_sold = seats_sold + %s
            WHERE flight_id = %s AND fare_class = %s
        """, (n_pax, flight_id, fare_class))

        gcur.execute("""
            INSERT INTO gds_changes_log
              (pnr_code, changed_at, change_type, changed_by, new_value, details)
            VALUES (%s,%s,'pnr_created',%s,'active',%s)
        """, (pnr_code, created_at, agency_code,
              json.dumps({'source': 'generate_ops_day', 'pax': n_pax})))

        gds_conn.commit()
        gcur.close()
    except Exception:
        gds_conn.rollback()


def do_payments(cur, branch, op_date: date, scale: float):
    """Обрабатывает оплаты по pending-бронированиям + balance-платежи."""
    branch_id, branch_code, _ = branch
    n = max(1, int(BASE_PAYMENTS * scale * random.uniform(0.8, 1.3)))

    # Pending бронирования этого филиала
    cur.execute("""
        SELECT bk.booking_id, bk.total_rub
        FROM bookings bk
        WHERE bk.branch_id = %s AND bk.status = 'pending'
        ORDER BY bk.booked_at ASC
        LIMIT %s
    """, (branch_id, n))
    pending = cur.fetchall()

    # Также balance-платежи (confirmed, но ещё нет second payment)
    cur.execute("""
        SELECT bk.booking_id, bk.total_rub
        FROM bookings bk
        WHERE bk.branch_id = %s AND bk.status = 'confirmed'
          AND (SELECT COUNT(*) FROM payments p
               WHERE p.booking_id = bk.booking_id) = 1
          AND (SELECT payment_type FROM payments p
               WHERE p.booking_id = bk.booking_id LIMIT 1) = 'deposit'
        ORDER BY RANDOM()
        LIMIT %s
    """, (branch_id, max(1, n // 2)))
    balance_due = cur.fetchall()

    created = 0
    paid_at = _rand_time(op_date, 9, 18)

    for booking_id, total_rub in pending:
        # Подтверждаем pending → confirmed
        cur.execute("""
            UPDATE bookings SET status='confirmed', paid_at=%s
            WHERE booking_id=%s
        """, (paid_at, booking_id))

        amount = float(total_rub)
        cur.execute("""
            INSERT INTO payments
              (booking_id, branch_id, amount_rub, method,
               payment_type, status, paid_at, transaction_ref)
            VALUES (%s,%s,%s,%s,'full','completed',%s,%s)
            RETURNING payment_id
        """, (booking_id, branch_id, amount,
              random.choice(PAYMENT_METHODS), paid_at, _txn()))
        payment_id = str(cur.fetchone()[0])

        _log_op(cur, branch_id, branch_code, 'payment',
                'payment', payment_id, op_date, paid_at,
                f'cashier_{random.randint(1,4)}', amount, 'completed',
                {'booking_id': str(booking_id), 'type': 'full'})
        created += 1
        paid_at += timedelta(minutes=random.randint(5, 40))

    for booking_id, total_rub in balance_due:
        # Остаток от депозита
        cur.execute("""
            SELECT COALESCE(SUM(amount_rub),0) FROM payments
            WHERE booking_id = %s AND status = 'completed'
        """, (booking_id,))
        already_paid = float(cur.fetchone()[0])
        balance = round(float(total_rub) - already_paid, 2)
        if balance <= 0:
            continue

        cur.execute("""
            INSERT INTO payments
              (booking_id, branch_id, amount_rub, method,
               payment_type, status, paid_at, transaction_ref)
            VALUES (%s,%s,%s,%s,'balance','completed',%s,%s)
            RETURNING payment_id
        """, (booking_id, branch_id, balance,
              random.choice(PAYMENT_METHODS), paid_at, _txn()))
        payment_id = str(cur.fetchone()[0])

        _log_op(cur, branch_id, branch_code, 'payment',
                'payment', payment_id, op_date, paid_at,
                f'cashier_{random.randint(1,4)}', balance, 'completed',
                {'booking_id': str(booking_id), 'type': 'balance'})
        created += 1
        paid_at += timedelta(minutes=random.randint(5, 40))

    return created


def do_cancellations(cur, branch, op_date: date, scale: float):
    """Отменяет несколько бронирований."""
    branch_id, branch_code, _ = branch
    n = max(1, int(BASE_CANCELLATIONS * scale * random.uniform(0.5, 1.5)))

    cur.execute("""
        SELECT bk.booking_id, bk.total_rub
        FROM bookings bk
        WHERE bk.branch_id = %s AND bk.status IN ('confirmed','pending')
          AND bk.booked_at < %s
        ORDER BY RANDOM()
        LIMIT %s
    """, (branch_id, op_date, n))
    to_cancel = cur.fetchall()

    created = 0
    for booking_id, total_rub in to_cancel:
        cancel_at = _rand_time(op_date, 10, 17)
        cur.execute("""
            UPDATE bookings SET status='cancelled' WHERE booking_id=%s
        """, (booking_id,))

        _log_op(cur, branch_id, branch_code, 'cancellation',
                'booking', str(booking_id), op_date, cancel_at,
                f'agent_{random.randint(1,8)}', None, 'ok',
                {'reason': random.choice(CANCEL_REASONS),
                 'total_rub': float(total_rub)})
        created += 1

    return created


def do_refunds(cur, branch, op_date: date, scale: float):
    """Обрабатывает возвраты по отменённым бронированиям."""
    branch_id, branch_code, _ = branch
    n = max(1, int(BASE_REFUNDS * scale * random.uniform(0.5, 1.5)))

    # Cancelled бронирования без возврата
    cur.execute("""
        SELECT bk.booking_id, bk.total_rub
        FROM bookings bk
        WHERE bk.branch_id = %s AND bk.status = 'cancelled'
          AND NOT EXISTS (
              SELECT 1 FROM refunds r WHERE r.booking_id = bk.booking_id
          )
        ORDER BY RANDOM()
        LIMIT %s
    """, (branch_id, n))
    to_refund = cur.fetchall()

    created = 0
    for booking_id, total_rub in to_refund:
        # Найдём платёж
        cur.execute("""
            SELECT payment_id, amount_rub FROM payments
            WHERE booking_id = %s AND status = 'reversed'
            LIMIT 1
        """, (booking_id,))
        prow = cur.fetchone()
        payment_id = str(prow[0]) if prow else None
        paid_amount = float(prow[1]) if prow else float(total_rub)

        penalty_pct = random.choice([0, 0, 0.10, 0.15, 0.20, 0.30])
        penalty     = round(paid_amount * penalty_pct, 2)
        refund_amt  = round(paid_amount - penalty, 2)
        reason      = random.choice(CANCEL_REASONS)

        req_at  = _rand_time(op_date, 9, 14)
        proc_at = req_at + timedelta(hours=random.randint(1, 8))

        cur.execute("""
            INSERT INTO refunds
              (booking_id, payment_id, branch_id, amount_rub,
               penalty_rub, reason, status, requested_at, processed_at, processed_by)
            VALUES (%s,%s,%s,%s,%s,%s,'completed',%s,%s,%s)
            RETURNING refund_id
        """, (booking_id, payment_id, branch_id, refund_amt,
              penalty, reason, req_at, proc_at,
              f'manager_{random.randint(1,5)}'))
        refund_id = str(cur.fetchone()[0])

        # Обновляем статус booking → refunded
        cur.execute("""
            UPDATE bookings SET status='refunded' WHERE booking_id=%s
        """, (booking_id,))

        _log_op(cur, branch_id, branch_code, 'refund',
                'refund', refund_id, op_date, proc_at,
                f'manager_{random.randint(1,5)}', refund_amt, 'completed',
                {'penalty_rub': penalty, 'reason': reason,
                 'booking_id': str(booking_id)})
        created += 1

    return created


def do_new_packages(cur, branch, op_date: date, scale: float):
    """
    Раздаёт новые пакеты туров филиалу.
    Выбирает активные туры и создаёт новые пакеты на даты в будущем.
    """
    branch_id, branch_code, _ = branch
    n = max(1, int(BASE_NEW_PACKAGES * scale * random.uniform(0.5, 1.5)))

    cur.execute("""
        SELECT t.tour_id, t.base_price_rub, t.duration_nights,
               d.resort, d.country
        FROM tours t
        JOIN destinations d ON d.dest_id = t.dest_id
        WHERE t.is_active = true
        ORDER BY RANDOM()
        LIMIT %s
    """, (n,))
    tours = cur.fetchall()

    created = 0
    open_at = _rand_time(op_date, 8, 10)

    for tour_id, base_price, nights, resort, country in tours:
        # Дата вылета — через 14-120 дней
        depart = op_date + timedelta(days=random.randint(14, 120))
        depart = depart - timedelta(days=depart.weekday())  # на понедельник
        return_date = depart + timedelta(days=nights)
        seats = random.choice([14, 16, 18, 20, 25])
        price = round(float(base_price) * random.uniform(0.95, 1.20), -2)

        cur.execute("""
            INSERT INTO tour_packages
              (tour_id, branch_id, depart_date, return_date,
               seats_total, price_rub, status, notes, created_at)
            VALUES (%s,%s,%s,%s,%s,%s,'active',%s,%s)
            RETURNING package_id
        """, (tour_id, branch_id, depart, return_date, seats, price,
              f'Новый пакет от {op_date.isoformat()}', open_at))
        pkg_id = cur.fetchone()[0]

        airline_name, airline_code = random.choice(AIRLINES)
        fnum = f'{airline_code}{random.randint(100,999)}'
        dep_dt = datetime.combine(depart, dtime(random.randint(5,9), 0))
        arr_dt = dep_dt + timedelta(hours=random.randint(3, 9))
        ret_dt = datetime.combine(return_date, dtime(random.randint(8,20), 0))
        ret_arr_dt = ret_dt + timedelta(hours=random.randint(3, 9))

        # Получаем iata_hub и iata_dest
        cur.execute("""
            SELECT br.iata_hub, ds.iata_dest
            FROM branches br
            JOIN tours t ON t.tour_id = %s
            JOIN destinations ds ON ds.dest_id = t.dest_id
            WHERE br.branch_id = %s
        """, (tour_id, branch_id))
        iata_row = cur.fetchone()
        if iata_row:
            hub, dest_iata = iata_row
            cur.execute("""
                INSERT INTO flights
                  (package_id, direction, airline_name, airline_code,
                   flight_number, origin_iata, dest_iata, depart_dt, arrive_dt, aircraft)
                VALUES
                  (%s,'OUT',%s,%s,%s,%s,%s,%s,%s,%s),
                  (%s,'RET',%s,%s,%s,%s,%s,%s,%s,%s)
            """, (
                pkg_id, airline_name, airline_code, fnum, hub, dest_iata,
                dep_dt, arr_dt, random.choice(['Boeing 737','Airbus A320','Boeing 767']),
                pkg_id, airline_name, airline_code, fnum, dest_iata, hub,
                ret_dt, ret_arr_dt, random.choice(['Boeing 737','Airbus A320','Boeing 767']),
            ))

        _log_op(cur, branch_id, branch_code, 'package_open',
                'package', str(pkg_id), op_date, open_at,
                'system', float(price), 'ok',
                {'tour_id': tour_id, 'depart': depart.isoformat(),
                 'seats': seats, 'country': country, 'resort': resort})
        created += 1
        open_at += timedelta(minutes=random.randint(5, 15))

    return created


# ─── main ─────────────────────────────────────────────────────────────────────

def parse_args():
    p = argparse.ArgumentParser(description='Generate one operational day for travel agency')
    p.add_argument('--date',    default=None,  help='YYYY-MM-DD (default: today)')
    p.add_argument('--branch',  default=None,  help='Branch code, e.g. МСК (default: all)')
    p.add_argument('--scale',   type=float, default=1.0,
                                help='Volume multiplier (default: 1.0)')
    p.add_argument('--dry-run', action='store_true',
                                help='Show plan without writing to DB')
    p.add_argument('--no-gds',  action='store_true',
                                help='Skip GDS PNR creation (if tdtp_airline unavailable)')
    return p.parse_args()


def main():
    args = parse_args()

    op_date = date.fromisoformat(args.date) if args.date else date.today()
    print('='*70)
    print(f'🗓  Operational Day Generator  |  date: {op_date}  |  scale: {args.scale}')
    print('='*70)

    if args.dry_run:
        print('🔍 DRY-RUN mode — no changes will be made to the database')
        total = int(BASE_BOOKINGS * args.scale)
        print(f'   Plan (per branch): ~{total} bookings, ~{int(BASE_PAYMENTS*args.scale)} payments,')
        print(f'                       ~{int(BASE_CANCELLATIONS*args.scale)} cancellations,')
        print(f'                       ~{int(BASE_REFUNDS*args.scale)} refunds,')
        print(f'                       ~{int(BASE_NEW_PACKAGES*args.scale)} new packages')
        return

    # Подключения
    conn = psycopg2.connect(**TA_CONFIG)
    gds_conn = None
    if not args.no_gds:
        try:
            gds_conn = psycopg2.connect(**GDS_CONFIG)
            print('✅ Connected to tdtp_airline (GDS)')
        except Exception as e:
            print(f'⚠️  GDS unavailable ({e}) — PNR creation skipped')

    cur = conn.cursor()

    # Получаем список филиалов
    if args.branch:
        cur.execute("""
            SELECT branch_id, code, city FROM branches
            WHERE code = %s AND is_active = true
        """, (args.branch,))
    else:
        cur.execute("""
            SELECT branch_id, code, city FROM branches
            WHERE is_active = true ORDER BY branch_id
        """)
    branches = cur.fetchall()

    if not branches:
        print(f'❌ Branch not found: {args.branch}')
        sys.exit(1)

    print(f'🏢 Processing {len(branches)} branch(es): {", ".join(b[2] for b in branches)}')
    print()

    # ── сводка по каждому филиалу ──────────────────────────────────────────────
    day_totals = {
        'bookings': 0, 'payments': 0,
        'cancellations': 0, 'refunds': 0, 'packages': 0,
    }

    for branch in branches:
        branch_id, branch_code, branch_city = branch
        print(f'  🏢 [{branch_code}] {branch_city}')

        try:
            nb = do_new_bookings(cur, branch, op_date, args.scale, gds_conn)
            np = do_payments(cur, branch, op_date, args.scale)
            nc = do_cancellations(cur, branch, op_date, args.scale)
            nr = do_refunds(cur, branch, op_date, args.scale)
            npkg = do_new_packages(cur, branch, op_date, args.scale)

            conn.commit()
            print(f'       bookings: {nb:>3}  payments: {np:>3}  '
                  f'cancellations: {nc:>2}  refunds: {nr:>2}  packages: {npkg:>2}')

            day_totals['bookings']      += nb
            day_totals['payments']      += np
            day_totals['cancellations'] += nc
            day_totals['refunds']       += nr
            day_totals['packages']      += npkg

        except Exception as e:
            conn.rollback()
            print(f'  ❌ Error in branch {branch_code}: {e}')
            import traceback
            traceback.print_exc()

    # ── итоги операционного дня ────────────────────────────────────────────────
    print()
    print('='*70)
    print(f'📊  OPERATIONAL DAY SUMMARY  [{op_date}]')
    print('='*70)
    for k, v in day_totals.items():
        print(f'  {k:<20} {v:>5}')

    # Финансовый итог дня
    cur.execute("""
        SELECT
            COALESCE(SUM(p.amount_rub) FILTER (WHERE p.status='completed'), 0) AS received,
            COALESCE(SUM(r.amount_rub) FILTER (WHERE r.status='completed'), 0) AS refunded
        FROM payments p
        FULL OUTER JOIN refunds r
          ON DATE(r.processed_at AT TIME ZONE 'Europe/Moscow') = %s
        WHERE DATE(p.paid_at AT TIME ZONE 'Europe/Moscow') = %s
          OR DATE(r.processed_at AT TIME ZONE 'Europe/Moscow') = %s
    """, (op_date, op_date, op_date))
    row = cur.fetchone()
    if row and row[0]:
        print(f'\n  💰 Received today:  {int(row[0]):>12,} RUB')
        print(f'  💸 Refunded today:  {int(row[1] or 0):>12,} RUB')
        print(f'  📈 Net cash flow:   {int((row[0] or 0) - (row[1] or 0)):>12,} RUB')

    # Кол-во записей в ops_log за день
    cur.execute("""
        SELECT op_type, COUNT(*) FROM operations_log
        WHERE op_date = %s GROUP BY op_type ORDER BY count DESC
    """, (op_date,))
    rows = cur.fetchall()
    if rows:
        print(f'\n  📋 Operations log [{op_date}]:')
        for op_type, cnt in rows:
            print(f'     {op_type:<22} {cnt:>5} records')

    print()
    print('✅  Done. Data is ready for tdtpcli pipeline export.')
    print(f"""
Example export commands:

  # Экспорт операционного дня из всех филиалов в один TDTP-пакет:
  tdtpcli export --config config.postgres.yaml \\
      --query "SELECT * FROM operations_log WHERE op_date = '{op_date}'" \\
      --output ops_{op_date}.tdtp

  # Экспорт новых бронирований по филиалу МСК:
  tdtpcli export --config config.postgres.yaml \\
      --query "SELECT bk.*, b.code as branch_code
               FROM bookings bk JOIN branches b ON b.branch_id = bk.branch_id
               WHERE DATE(bk.booked_at) = '{op_date}'
               AND b.code = 'МСК'" \\
      --output bookings_MSK_{op_date}.tdtp

  # Синхронизация из GDS:
  tdtpcli export --config config.airline_gds.yaml \\
      --query "SELECT * FROM gds_pnr WHERE DATE(created_at) = '{op_date}'" \\
      --output gds_pnr_{op_date}.tdtp
""")

    cur.close()
    conn.close()
    if gds_conn:
        gds_conn.close()


if __name__ == '__main__':
    main()
