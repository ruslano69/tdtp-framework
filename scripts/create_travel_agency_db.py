#!/usr/bin/env python3
"""
Тестовая база данных PostgreSQL — Туристическое агентство с филиалами

Схема:
  branches        — филиалы в городах России
  destinations    — направления (страны/курорты)
  tours           — туры (отель + программа + направление)
  tour_packages   — пакеты (конкретные даты, места, статус active/closed/cancelled)
  flights         — авиарейсы под пакеты (туда + обратно)
  clients         — клиенты
  bookings        — бронирования (продажи путевок)

Ключевые запросы:
  - Активные / неактивные туры
  - Календарный график: дата вылета, тур, мест всего / продано / свободно
  - Продажи по филиалам
"""

import psycopg2
import random
from datetime import datetime, timedelta, date
import json
import uuid

DB_CONFIG = {
    'host': 'localhost',
    'port': 5432,
    'user': 'tdtp_user',
    'password': 'tdtp_dev_pass_2025',
    'database': 'tdtp_test'
}

# ── справочники ────────────────────────────────────────────────────────────────

BRANCH_CITIES = [
    ('Москва',         'МСК', 'SVO'),
    ('Санкт-Петербург','СПБ', 'LED'),
    ('Казань',         'КЗН', 'KZN'),
    ('Екатеринбург',   'ЕКБ', 'SVX'),
    ('Новосибирск',    'НСК', 'OVB'),
    ('Краснодар',      'КДР', 'KRR'),
    ('Ростов-на-Дону', 'РНД', 'ROV'),
    ('Уфа',            'УФА', 'UFA'),
]

DESTINATIONS = [
    # (страна, курорт, код_аэропорта, популярный_сезон)
    ('Турция',      'Анталья',      'AYT', 'summer'),
    ('Египет',      'Хургада',      'HRG', 'winter'),
    ('Египет',      'Шарм-эль-Шейх','SSH', 'winter'),
    ('Таиланд',     'Пхукет',       'HKT', 'winter'),
    ('ОАЭ',         'Дубай',        'DXB', 'winter'),
    ('Испания',     'Барселона',    'BCN', 'summer'),
    ('Греция',      'Крит',         'HER', 'summer'),
    ('Мальдивы',    'Мале',         'MLE', 'all'),
    ('Куба',        'Варадеро',     'VRA', 'winter'),
    ('Тунис',       'Хаммамет',     'TUN', 'summer'),
    ('Вьетнам',     'Нячанг',       'CXR', 'winter'),
    ('Кипр',        'Ларнака',      'LCA', 'summer'),
]

HOTELS = [
    # (название_шаблон, звезды, тип)
    ('Grand Palace',        5, 'resort'),
    ('Blue Lagoon',         4, 'resort'),
    ('Sunrise Beach',       4, 'beach'),
    ('Royal Garden',        5, 'luxury'),
    ('Palm Tree Inn',       3, 'budget'),
    ('Azure Sea Club',      4, 'club'),
    ('Crystal Waters',      5, 'spa'),
    ('Golden Sands',        4, 'family'),
    ('Coral Bay Hotel',     3, 'beach'),
    ('Paradise Resort',     5, 'all_inclusive'),
    ('Sea Breeze Club',     4, 'all_inclusive'),
    ('Oasis Palace',        4, 'resort'),
]

AIRLINES = [
    ('Аэрофлот',       'SU'),
    ('Победа',         'DP'),
    ('S7 Airlines',    'S7'),
    ('Уральские авиалинии', 'U6'),
    ('Nordwind',       'N4'),
    ('Azur Air',       'ZF'),
    ('TUI fly',        'X3'),
    ('FlyDubai',       'FZ'),
]


def connect():
    conn = psycopg2.connect(**DB_CONFIG)
    return conn


def drop_and_create_tables(conn):
    cur = conn.cursor()
    print('🗑  Dropping old tables...')
    cur.execute("""
        DROP TABLE IF EXISTS bookings    CASCADE;
        DROP TABLE IF EXISTS flights     CASCADE;
        DROP TABLE IF EXISTS tour_packages CASCADE;
        DROP TABLE IF EXISTS tours       CASCADE;
        DROP TABLE IF EXISTS destinations CASCADE;
        DROP TABLE IF EXISTS clients     CASCADE;
        DROP TABLE IF EXISTS branches    CASCADE;
    """)

    print('📋 Creating branches...')
    cur.execute("""
        CREATE TABLE branches (
            branch_id   SERIAL PRIMARY KEY,
            city        VARCHAR(100) NOT NULL,
            code        CHAR(3) NOT NULL UNIQUE,
            iata_hub    CHAR(3) NOT NULL,           -- аэропорт вылета
            address     VARCHAR(255),
            phone       VARCHAR(30),
            manager     VARCHAR(100),
            opened_at   DATE NOT NULL,
            is_active   BOOLEAN DEFAULT true
        );
    """)

    print('📋 Creating destinations...')
    cur.execute("""
        CREATE TABLE destinations (
            dest_id     SERIAL PRIMARY KEY,
            country     VARCHAR(100) NOT NULL,
            resort      VARCHAR(100) NOT NULL,
            iata_dest   CHAR(3) NOT NULL,           -- аэропорт назначения
            season      VARCHAR(10) NOT NULL         -- summer / winter / all
        );
    """)

    print('📋 Creating tours...')
    cur.execute("""
        CREATE TABLE tours (
            tour_id         SERIAL PRIMARY KEY,
            dest_id         INTEGER REFERENCES destinations(dest_id),
            title           VARCHAR(200) NOT NULL,
            hotel_name      VARCHAR(150) NOT NULL,
            hotel_stars     SMALLINT NOT NULL,
            hotel_type      VARCHAR(30) NOT NULL,
            duration_nights SMALLINT NOT NULL,
            meal_plan       VARCHAR(20) NOT NULL,   -- AI / HB / BB / RO
            description     TEXT,
            base_price_rub  NUMERIC(12,2) NOT NULL, -- базовая цена за чел
            is_active       BOOLEAN DEFAULT true,
            created_at      TIMESTAMP WITH TIME ZONE DEFAULT NOW()
        );
    """)

    print('📋 Creating tour_packages...')
    cur.execute("""
        CREATE TABLE tour_packages (
            package_id      SERIAL PRIMARY KEY,
            tour_id         INTEGER REFERENCES tours(tour_id),
            branch_id       INTEGER REFERENCES branches(branch_id),
            depart_date     DATE NOT NULL,          -- дата вылета
            return_date     DATE NOT NULL,           -- дата возврата
            seats_total     SMALLINT NOT NULL,       -- мест в пакете
            price_rub       NUMERIC(12,2) NOT NULL,  -- актуальная цена за чел
            status          VARCHAR(20) NOT NULL     -- active / sold_out / closed / cancelled
                            DEFAULT 'active',
            notes           TEXT,
            created_at      TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
            CONSTRAINT chk_dates CHECK (return_date > depart_date)
        );
    """)

    print('📋 Creating flights...')
    cur.execute("""
        CREATE TABLE flights (
            flight_id       SERIAL PRIMARY KEY,
            package_id      INTEGER REFERENCES tour_packages(package_id),
            direction       CHAR(3) NOT NULL,        -- OUT / RET
            airline_name    VARCHAR(100) NOT NULL,
            airline_code    CHAR(2) NOT NULL,
            flight_number   VARCHAR(10) NOT NULL,
            origin_iata     CHAR(3) NOT NULL,
            dest_iata       CHAR(3) NOT NULL,
            depart_dt       TIMESTAMP NOT NULL,
            arrive_dt       TIMESTAMP NOT NULL,
            aircraft        VARCHAR(50),
            CONSTRAINT chk_flight_time CHECK (arrive_dt > depart_dt)
        );
    """)

    print('📋 Creating clients...')
    cur.execute("""
        CREATE TABLE clients (
            client_id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
            full_name       VARCHAR(150) NOT NULL,
            phone           VARCHAR(30),
            email           VARCHAR(150),
            passport_series VARCHAR(12),
            birth_date      DATE,
            preferred_branch INTEGER REFERENCES branches(branch_id),
            registered_at   TIMESTAMP WITH TIME ZONE DEFAULT NOW()
        );
    """)

    print('📋 Creating bookings...')
    cur.execute("""
        CREATE TABLE bookings (
            booking_id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
            package_id      INTEGER REFERENCES tour_packages(package_id),
            branch_id       INTEGER REFERENCES branches(branch_id),
            client_id       UUID REFERENCES clients(client_id),
            persons         SMALLINT NOT NULL DEFAULT 1,
            total_rub       NUMERIC(14,2) NOT NULL,
            status          VARCHAR(20) NOT NULL    -- confirmed / pending / cancelled / refunded
                            DEFAULT 'confirmed',
            booked_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
            paid_at         TIMESTAMP WITH TIME ZONE,
            gds_pnr_code    VARCHAR(6),              -- PNR в системе GDS авиаперевозчика
            notes           TEXT
        );
    """)

    print('📋 Creating payments...')
    cur.execute("""
        CREATE TABLE payments (
            payment_id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
            booking_id      UUID NOT NULL REFERENCES bookings(booking_id),
            branch_id       INTEGER NOT NULL REFERENCES branches(branch_id),
            amount_rub      NUMERIC(14,2) NOT NULL,
            method          VARCHAR(20) NOT NULL,    -- card / cash / transfer / online / invoice
            payment_type    VARCHAR(20) NOT NULL,    -- deposit / balance / full
            status          VARCHAR(20) NOT NULL     -- completed / pending / failed / reversed
                            DEFAULT 'completed',
            paid_at         TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
            transaction_ref VARCHAR(50),             -- номер транзакции банка
            terminal_id     VARCHAR(20),             -- id терминала / кассы
            notes           TEXT
        );
    """)

    print('📋 Creating refunds...')
    cur.execute("""
        CREATE TABLE refunds (
            refund_id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
            booking_id      UUID NOT NULL REFERENCES bookings(booking_id),
            payment_id      UUID REFERENCES payments(payment_id),
            branch_id       INTEGER NOT NULL REFERENCES branches(branch_id),
            amount_rub      NUMERIC(14,2) NOT NULL,
            penalty_rub     NUMERIC(14,2) NOT NULL DEFAULT 0,   -- штраф за отмену
            reason          VARCHAR(50) NOT NULL,               -- client_request / flight_cancelled / no_visa / force_majeure / other
            status          VARCHAR(20) NOT NULL DEFAULT 'pending',  -- pending / completed / rejected
            requested_at    TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
            processed_at    TIMESTAMP WITH TIME ZONE,
            processed_by    VARCHAR(100)
        );
    """)

    print('📋 Creating operations_log...')
    cur.execute("""
        CREATE TABLE operations_log (
            op_id           BIGSERIAL PRIMARY KEY,
            branch_id       INTEGER NOT NULL REFERENCES branches(branch_id),
            branch_code     CHAR(3) NOT NULL,
            op_type         VARCHAR(30) NOT NULL,    -- booking / payment / cancellation / refund / package_open / package_close / tour_update
            entity_type     VARCHAR(30),             -- booking / payment / refund / package / tour
            entity_id       VARCHAR(50),
            op_date         DATE NOT NULL,
            op_time         TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
            operator        VARCHAR(100),
            amount_rub      NUMERIC(14,2),
            status          VARCHAR(20) DEFAULT 'ok',
            details         JSONB,
            synced_at       TIMESTAMP WITH TIME ZONE  -- время, когда запись была выгружена через pipeline
        );
        CREATE INDEX ops_log_branch_date_idx ON operations_log (branch_id, op_date);
        CREATE INDEX ops_log_type_idx        ON operations_log (op_type);
        CREATE INDEX ops_log_entity_idx      ON operations_log (entity_type, entity_id);
    """)

    # ── полезные VIEW ─────────────────────────────────────────────────────────
    print('👁  Creating views...')

    # Календарный график свободных мест
    cur.execute("""
        CREATE OR REPLACE VIEW v_calendar_availability AS
        SELECT
            tp.package_id,
            tp.depart_date,
            tp.return_date,
            b.city          AS branch_city,
            b.code          AS branch_code,
            d.country,
            d.resort,
            t.title         AS tour_title,
            t.hotel_name,
            t.hotel_stars,
            t.meal_plan,
            t.duration_nights,
            tp.seats_total,
            COALESCE(sold.seats_sold, 0)                        AS seats_sold,
            tp.seats_total - COALESCE(sold.seats_sold, 0)       AS seats_free,
            tp.price_rub,
            tp.status
        FROM tour_packages tp
        JOIN tours       t  ON t.tour_id  = tp.tour_id
        JOIN destinations d  ON d.dest_id  = t.dest_id
        JOIN branches    b  ON b.branch_id = tp.branch_id
        LEFT JOIN (
            SELECT package_id, SUM(persons) AS seats_sold
            FROM bookings
            WHERE status IN ('confirmed','pending')
            GROUP BY package_id
        ) sold ON sold.package_id = tp.package_id
        ORDER BY tp.depart_date, b.city;
    """)

    # Активные туры с остатком мест
    cur.execute("""
        CREATE OR REPLACE VIEW v_active_tours_availability AS
        SELECT *
        FROM v_calendar_availability
        WHERE status = 'active'
          AND depart_date >= CURRENT_DATE
          AND seats_free > 0
        ORDER BY depart_date;
    """)

    # Продажи по филиалам
    cur.execute("""
        CREATE OR REPLACE VIEW v_branch_sales AS
        SELECT
            b.city,
            b.code,
            COUNT(bk.booking_id)                        AS total_bookings,
            SUM(bk.persons)                             AS total_persons,
            SUM(bk.total_rub) FILTER (WHERE bk.status = 'confirmed') AS revenue_rub,
            COUNT(*) FILTER (WHERE bk.status = 'cancelled')          AS cancellations
        FROM branches b
        LEFT JOIN bookings bk ON bk.branch_id = b.branch_id
        GROUP BY b.branch_id, b.city, b.code
        ORDER BY revenue_rub DESC NULLS LAST;
    """)

    # Финансовый итог по филиалу за день
    cur.execute("""
        CREATE OR REPLACE VIEW v_financial_summary AS
        SELECT
            b.city                                                      AS branch_city,
            b.code                                                      AS branch_code,
            DATE(p.paid_at AT TIME ZONE 'Europe/Moscow')                AS op_date,
            COUNT(DISTINCT p.payment_id)                                AS payments_count,
            SUM(p.amount_rub) FILTER (WHERE p.status = 'completed')     AS received_rub,
            SUM(p.amount_rub) FILTER (WHERE p.status = 'reversed')      AS reversed_rub,
            COUNT(DISTINCT r.refund_id)                                 AS refunds_count,
            COALESCE(SUM(r.amount_rub) FILTER (WHERE r.status = 'completed'), 0) AS refunded_rub
        FROM branches b
        LEFT JOIN payments p ON p.branch_id = b.branch_id
        LEFT JOIN refunds  r ON r.branch_id = b.branch_id
          AND DATE(r.processed_at AT TIME ZONE 'Europe/Moscow') =
              DATE(p.paid_at AT TIME ZONE 'Europe/Moscow')
        GROUP BY b.branch_id, b.city, b.code, DATE(p.paid_at AT TIME ZONE 'Europe/Moscow')
        ORDER BY op_date DESC, received_rub DESC NULLS LAST;
    """)

    # Журнал операций — последние 500 записей
    cur.execute("""
        CREATE OR REPLACE VIEW v_ops_journal AS
        SELECT
            ol.op_id,
            ol.op_date,
            ol.op_time,
            b.city                                  AS branch_city,
            ol.branch_code,
            ol.op_type,
            ol.entity_type,
            ol.entity_id,
            ol.operator,
            ol.amount_rub,
            ol.status,
            ol.details
        FROM operations_log ol
        JOIN branches b ON b.branch_id = ol.branch_id
        ORDER BY ol.op_time DESC;
    """)

    # Сводка операционного дня по филиалам
    cur.execute("""
        CREATE OR REPLACE VIEW v_daily_branch_report AS
        SELECT
            ol.op_date,
            b.city                                              AS branch_city,
            ol.branch_code,
            COUNT(*) FILTER (WHERE ol.op_type = 'booking')     AS new_bookings,
            COUNT(*) FILTER (WHERE ol.op_type = 'payment')     AS payments,
            COUNT(*) FILTER (WHERE ol.op_type = 'cancellation') AS cancellations,
            COUNT(*) FILTER (WHERE ol.op_type = 'refund')      AS refunds,
            COUNT(*) FILTER (WHERE ol.op_type = 'package_open') AS new_packages,
            COALESCE(SUM(ol.amount_rub) FILTER (WHERE ol.op_type = 'payment'), 0) AS day_revenue_rub,
            COALESCE(SUM(ol.amount_rub) FILTER (WHERE ol.op_type = 'refund'), 0)  AS day_refunds_rub
        FROM operations_log ol
        JOIN branches b ON b.branch_id = ol.branch_id
        GROUP BY ol.op_date, b.branch_id, b.city, ol.branch_code
        ORDER BY ol.op_date DESC, day_revenue_rub DESC;
    """)

    conn.commit()
    cur.close()
    print('✅ Tables and views created')


def insert_reference_data(conn):
    cur = conn.cursor()

    # branches
    print('\n🏢 Inserting branches...')
    branch_ids = []
    for i, (city, code, hub) in enumerate(BRANCH_CITIES):
        opened = date(2015, 1, 1) + timedelta(days=random.randint(0, 1000))
        cur.execute("""
            INSERT INTO branches (city, code, iata_hub, address, phone, manager, opened_at, is_active)
            VALUES (%s, %s, %s, %s, %s, %s, %s, %s)
            RETURNING branch_id
        """, (
            city, code, hub,
            f'ул. Туристическая, {random.randint(1, 200)}',
            f'+7 {random.randint(900,999)} {random.randint(100,999)}-{random.randint(10,99)}-{random.randint(10,99)}',
            f'Менеджер_{city[:4]}',
            opened,
            True  # все активны
        ))
        branch_ids.append(cur.fetchone()[0])

    # destinations
    print('🌍 Inserting destinations...')
    dest_ids = []
    for country, resort, iata, season in DESTINATIONS:
        cur.execute("""
            INSERT INTO destinations (country, resort, iata_dest, season)
            VALUES (%s, %s, %s, %s) RETURNING dest_id
        """, (country, resort, iata, season))
        dest_ids.append(cur.fetchone()[0])

    conn.commit()
    cur.close()
    return branch_ids, dest_ids


def insert_tours(conn, dest_ids):
    cur = conn.cursor()
    print('🏖  Inserting tours...')
    tour_ids = []
    meal_plans = ['AI', 'AI', 'HB', 'BB', 'RO']  # AI чаще

    for dest_id in dest_ids:
        # 2-4 тура на каждое направление
        for _ in range(random.randint(2, 4)):
            hotel_name, stars, h_type = random.choice(HOTELS)
            nights = random.choice([7, 10, 14, 7, 7])
            base_price = round(random.uniform(45_000, 280_000), -3)  # кратно 1000
            meal = random.choice(meal_plans)
            # ~15% туров неактивны (сезон закрыт / снят с продажи)
            active = random.random() > 0.15

            cur.execute("""
                INSERT INTO tours
                  (dest_id, title, hotel_name, hotel_stars, hotel_type,
                   duration_nights, meal_plan, description, base_price_rub, is_active)
                VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s)
                RETURNING tour_id
            """, (
                dest_id,
                f'{hotel_name} {nights}н',
                hotel_name,
                stars,
                h_type,
                nights,
                meal,
                f'{nights} ночей, питание {meal}, {stars}★',
                base_price,
                active
            ))
            tour_ids.append((cur.fetchone()[0], active))

    conn.commit()
    cur.close()
    print(f'   Tours: {len(tour_ids)} ({sum(1 for _,a in tour_ids if a)} active)')
    return tour_ids


def insert_packages_and_flights(conn, tour_ids, branch_ids):
    """Создаёт пакеты — сетка дат [сегодня-120 .. сегодня+180] с шагом 7-14 дней"""
    cur = conn.cursor()
    print('\n📦 Inserting tour packages + flights...')

    today = date.today()
    window_start = today - timedelta(days=120)
    window_end   = today + timedelta(days=180)

    package_ids = []

    for tour_id, tour_active in tour_ids:
        # Каждый тур продаётся из 2-3 случайных филиалов
        branch_sample = random.sample(branch_ids, k=random.randint(2, 3))

        for branch_id in branch_sample:
            # Сетка дат вылета
            d = window_start + timedelta(days=random.randint(0, 13))
            while d <= window_end:
                duration = random.choice([7, 7, 10, 14])
                seats = random.choice([10, 14, 18, 20, 25, 30])
                price_coef = random.uniform(0.9, 1.35)

                # Получаем base_price
                cur.execute('SELECT base_price_rub, duration_nights FROM tours WHERE tour_id = %s', (tour_id,))
                row = cur.fetchone()
                price = round(float(row[0]) * price_coef, -2)

                # Определяем статус пакета
                if not tour_active:
                    status = 'cancelled'
                elif d < today - timedelta(days=3):
                    status = random.choice(['closed', 'closed', 'sold_out'])
                elif d < today:
                    status = 'closed'
                else:
                    status = 'active'

                cur.execute("""
                    INSERT INTO tour_packages
                      (tour_id, branch_id, depart_date, return_date,
                       seats_total, price_rub, status)
                    VALUES (%s, %s, %s, %s, %s, %s, %s)
                    RETURNING package_id
                """, (
                    tour_id, branch_id,
                    d, d + timedelta(days=duration),
                    seats, price, status
                ))
                pkg_id = cur.fetchone()[0]
                package_ids.append((pkg_id, branch_id, d, status))

                # Получаем iata_hub и iata_dest
                cur.execute("""
                    SELECT br.iata_hub, ds.iata_dest
                    FROM branches br, tours t
                    JOIN destinations ds ON ds.dest_id = t.dest_id
                    WHERE br.branch_id = %s AND t.tour_id = %s
                """, (branch_id, tour_id))
                iata_row = cur.fetchone()
                if not iata_row:
                    d += timedelta(days=random.choice([7, 7, 14]))
                    continue
                hub, dest_iata = iata_row

                airline_name, airline_code = random.choice(AIRLINES)
                fnum_out = f'{airline_code}{random.randint(100,999)}'
                fnum_ret = f'{airline_code}{random.randint(100,999)}'

                # Вылет туда — ранним утром
                dep_out = datetime.combine(d, datetime.min.time()) + timedelta(hours=random.randint(4,9), minutes=random.choice([0,15,30,45]))
                arr_out = dep_out + timedelta(hours=random.randint(3, 9))

                # Вылет обратно
                ret_date = d + timedelta(days=duration)
                dep_ret = datetime.combine(ret_date, datetime.min.time()) + timedelta(hours=random.randint(8,22))
                arr_ret = dep_ret + timedelta(hours=random.randint(3, 9))

                cur.execute("""
                    INSERT INTO flights
                      (package_id, direction, airline_name, airline_code,
                       flight_number, origin_iata, dest_iata, depart_dt, arrive_dt, aircraft)
                    VALUES
                      (%s,'OUT',%s,%s,%s,%s,%s,%s,%s,%s),
                      (%s,'RET',%s,%s,%s,%s,%s,%s,%s,%s)
                """, (
                    pkg_id, airline_name, airline_code, fnum_out, hub, dest_iata, dep_out, arr_out,
                    random.choice(['Boeing 737', 'Airbus A320', 'Boeing 767', 'Airbus A321']),
                    pkg_id, airline_name, airline_code, fnum_ret, dest_iata, hub, dep_ret, arr_ret,
                    random.choice(['Boeing 737', 'Airbus A320', 'Boeing 767', 'Airbus A321']),
                ))

                d += timedelta(days=random.choice([7, 7, 14]))

    conn.commit()
    cur.close()
    print(f'   Packages: {len(package_ids)}')
    return package_ids


def insert_clients_and_bookings(conn, package_ids, branch_ids):
    cur = conn.cursor()
    booking_records = []   # собираем для insert_payments_and_refunds

    # Клиенты
    print('\n👥 Inserting clients...')
    first_names = ['Алексей','Дмитрий','Андрей','Сергей','Иван','Елена','Ольга','Наталья','Татьяна','Мария',
                   'Максим','Николай','Виктор','Юрий','Артём','Анна','Ирина','Светлана','Юлия','Екатерина']
    last_names  = ['Иванов','Петров','Сидоров','Козлов','Новиков','Морозов','Волков','Соловьёв','Попов','Лебедев',
                   'Иванова','Петрова','Сидорова','Козлова','Новикова','Морозова','Волкова','Соловьёва','Попова','Лебедева']

    client_ids = []
    for i in range(300):
        cid = str(uuid.uuid4())
        client_ids.append(cid)
        fn = random.choice(first_names)
        ln = random.choice(last_names)
        birth = date(1960, 1, 1) + timedelta(days=random.randint(0, 20000))
        cur.execute("""
            INSERT INTO clients
              (client_id, full_name, phone, email, passport_series, birth_date, preferred_branch)
            VALUES (%s, %s, %s, %s, %s, %s, %s)
        """, (
            cid,
            f'{ln} {fn}',
            f'+7{random.randint(9000000000, 9999999999)}',
            f'{fn.lower()}{i}@mail.ru',
            f'{random.randint(1000,9999)} {random.randint(100000,999999)}',
            birth,
            random.choice(branch_ids)
        ))

    conn.commit()

    # Бронирования — только для активных и closed пакетов
    print('🎫 Inserting bookings...')
    today = date.today()
    bookable = [(pid, bid, dep, st) for pid, bid, dep, st in package_ids
                if st in ('active', 'closed', 'sold_out')]

    booking_statuses = ['confirmed','confirmed','confirmed','pending','cancelled','refunded']
    booked = 0

    for pkg_id, branch_id, dep_date, pkg_status in bookable:
        # Сколько мест занять: для past — почти всё, future — 30-90%
        cur.execute('SELECT seats_total FROM tour_packages WHERE package_id = %s', (pkg_id,))
        total = cur.fetchone()[0]

        if dep_date < today:
            fill = random.uniform(0.6, 1.0)
        else:
            fill = random.uniform(0.2, 0.85)

        persons_to_book = int(total * fill)
        booked_so_far = 0

        while booked_so_far < persons_to_book:
            client_id = random.choice(client_ids)
            persons = random.choice([1, 1, 2, 2, 3])
            if booked_so_far + persons > total:
                persons = total - booked_so_far
            if persons <= 0:
                break

            total_rub = persons * float(
                [x for x in [(pkg_id,)]][0][0]  # placeholder — подтянем ниже
                if False else 0
            )
            # Получаем цену пакета
            cur.execute('SELECT price_rub FROM tour_packages WHERE package_id = %s', (pkg_id,))
            price = float(cur.fetchone()[0])
            total_rub = round(persons * price, 2)

            status = random.choice(booking_statuses)
            # Если пакет sold_out, все confirmed
            if pkg_status == 'sold_out':
                status = 'confirmed'

            booked_at = datetime.combine(dep_date - timedelta(days=random.randint(5, 90)),
                                         datetime.min.time()) + timedelta(hours=random.randint(8,20))
            paid_at = booked_at + timedelta(hours=random.randint(1, 48)) if status == 'confirmed' else None

            # PNR только для confirmed бронирований с авиаперелётом
            gds_pnr = None
            if status == 'confirmed':
                gds_pnr = _gen_pnr()

            cur.execute("""
                INSERT INTO bookings
                  (package_id, branch_id, client_id, persons, total_rub,
                   status, booked_at, paid_at, gds_pnr_code)
                VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s)
                RETURNING booking_id
            """, (pkg_id, branch_id, client_id, persons, total_rub,
                  status, booked_at, paid_at, gds_pnr))
            booking_id = cur.fetchone()[0]
            booking_records.append({
                'booking_id': str(booking_id),
                'branch_id':  branch_id,
                'total_rub':  total_rub,
                'status':     status,
                'booked_at':  booked_at,
                'paid_at':    paid_at,
            })

            booked_so_far += persons
            booked += 1

    conn.commit()
    cur.close()
    print(f'   Clients: {len(client_ids)}, Bookings: {booked}')
    return booking_records


def _gen_pnr():
    """Генерирует 6-символьный PNR-код (A-Z0-9)."""
    import string
    return ''.join(random.choices(string.ascii_uppercase + string.digits, k=6))


def insert_payments_and_refunds(conn, booking_records):
    """
    Для каждого бронирования создаёт оплаты.
    confirmed  → 1 или 2 платежа (депозит + остаток)
    pending    → 1 платёж со статусом pending
    cancelled  → попытка платежа, затем возврат
    refunded   → завершённый возврат
    """
    cur = conn.cursor()
    print('\n💳 Inserting payments & refunds...')

    methods  = ['card', 'card', 'card', 'cash', 'online', 'transfer', 'invoice']
    p_total  = 0
    r_total  = 0

    cancel_reasons = [
        'client_request', 'client_request', 'no_visa',
        'flight_cancelled', 'force_majeure', 'other',
    ]

    for bk in booking_records:
        bid      = bk['booking_id']
        br_id    = bk['branch_id']
        total    = float(bk['total_rub'])
        status   = bk['status']
        booked   = bk['booked_at']

        if status == 'confirmed':
            # 40% — сразу полная оплата, 60% — депозит + остаток
            if random.random() < 0.40:
                # Полная оплата
                paid_at = booked + timedelta(minutes=random.randint(10, 120))
                cur.execute("""
                    INSERT INTO payments
                      (booking_id, branch_id, amount_rub, method,
                       payment_type, status, paid_at, transaction_ref)
                    VALUES (%s,%s,%s,%s,'full','completed',%s,%s)
                """, (bid, br_id, total, random.choice(methods),
                      paid_at, f'TXN{random.randint(10**9, 10**10-1)}'))
            else:
                # Депозит 30–50%
                deposit_pct = random.uniform(0.30, 0.50)
                deposit     = round(total * deposit_pct, 2)
                balance     = round(total - deposit, 2)
                dep_at  = booked + timedelta(minutes=random.randint(10, 60))
                bal_at  = dep_at  + timedelta(days=random.randint(3, 30))
                for amt, ptype, ts in [(deposit, 'deposit', dep_at), (balance, 'balance', bal_at)]:
                    cur.execute("""
                        INSERT INTO payments
                          (booking_id, branch_id, amount_rub, method,
                           payment_type, status, paid_at, transaction_ref)
                        VALUES (%s,%s,%s,%s,%s,'completed',%s,%s)
                    """, (bid, br_id, amt, random.choice(methods),
                          ptype, ts, f'TXN{random.randint(10**9, 10**10-1)}'))
            p_total += 1

        elif status == 'pending':
            # Ожидает оплаты — deposit pending
            dep_at = booked + timedelta(hours=random.randint(1, 4))
            cur.execute("""
                INSERT INTO payments
                  (booking_id, branch_id, amount_rub, method,
                   payment_type, status, paid_at)
                VALUES (%s,%s,%s,%s,'deposit','pending',%s)
            """, (bid, br_id, round(total * 0.3, 2), random.choice(methods), dep_at))
            p_total += 1

        elif status in ('cancelled', 'refunded'):
            # Был платёж, потом отмена
            paid_at = booked + timedelta(minutes=random.randint(15, 180))
            cur.execute("""
                INSERT INTO payments
                  (booking_id, branch_id, amount_rub, method,
                   payment_type, status, paid_at, transaction_ref)
                VALUES (%s,%s,%s,%s,'full','reversed',%s,%s)
                RETURNING payment_id
            """, (bid, br_id, total, random.choice(methods),
                  paid_at, f'TXN{random.randint(10**9, 10**10-1)}'))
            payment_id = cur.fetchone()[0]
            p_total += 1

            # Штраф 0–30% от суммы (зависит от срочности)
            penalty_pct = random.choice([0, 0, 0.10, 0.15, 0.20, 0.30])
            penalty     = round(total * penalty_pct, 2)
            refund_amt  = round(total - penalty, 2)
            reason      = random.choice(cancel_reasons)
            req_at      = paid_at + timedelta(hours=random.randint(1, 72))
            proc_at     = req_at  + timedelta(hours=random.randint(4, 96)) if status == 'refunded' else None
            rf_status   = 'completed' if status == 'refunded' else 'pending'

            cur.execute("""
                INSERT INTO refunds
                  (booking_id, payment_id, branch_id, amount_rub,
                   penalty_rub, reason, status, requested_at, processed_at, processed_by)
                VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)
            """, (bid, payment_id, br_id, refund_amt, penalty,
                  reason, rf_status, req_at, proc_at,
                  f'manager_{random.randint(1,10)}' if proc_at else None))
            r_total += 1

    conn.commit()
    cur.close()
    print(f'   Payments: {p_total}, Refunds: {r_total}')


def build_operations_log(conn):
    """
    Заполняет operations_log на основе уже существующих данных.
    Каждая операция (бронирование, оплата, отмена, возврат) получает запись в лог.
    """
    cur = conn.cursor()
    print('\n📋 Building operations_log...')

    # Бронирования
    cur.execute("""
        SELECT bk.booking_id, bk.branch_id, br.code, bk.status,
               bk.booked_at, bk.total_rub, bk.persons
        FROM bookings bk
        JOIN branches br ON br.branch_id = bk.branch_id
    """)
    bookings = cur.fetchall()

    ops = []
    for booking_id, branch_id, branch_code, status, booked_at, total_rub, persons in bookings:
        ops.append((
            branch_id, branch_code, 'booking',
            'booking', str(booking_id),
            booked_at.date(), booked_at,
            f'agent_{random.randint(1,8)}',
            float(total_rub) if status not in ('cancelled',) else None,
            status if status != 'confirmed' else 'ok',
            json.dumps({'persons': persons, 'booking_status': status}),
        ))

    # Оплаты
    cur.execute("""
        SELECT p.payment_id, p.branch_id, br.code,
               p.amount_rub, p.payment_type, p.method, p.status, p.paid_at
        FROM payments p
        JOIN branches br ON br.branch_id = p.branch_id
        WHERE p.paid_at IS NOT NULL
    """)
    for pid, branch_id, branch_code, amount, ptype, method, status, paid_at in cur.fetchall():
        ops.append((
            branch_id, branch_code, 'payment',
            'payment', str(pid),
            paid_at.date(), paid_at,
            f'cashier_{random.randint(1,4)}',
            float(amount),
            status,
            json.dumps({'payment_type': ptype, 'method': method}),
        ))

    # Отмены и возвраты
    cur.execute("""
        SELECT r.refund_id, r.branch_id, br.code, r.amount_rub,
               r.penalty_rub, r.reason, r.status,
               r.requested_at, r.processed_at
        FROM refunds r
        JOIN branches br ON br.branch_id = r.branch_id
    """)
    for rid, branch_id, branch_code, amount, penalty, reason, status, req_at, proc_at in cur.fetchall():
        # Запись об отмене
        ops.append((
            branch_id, branch_code, 'cancellation',
            'refund', str(rid),
            req_at.date(), req_at,
            f'agent_{random.randint(1,8)}',
            None,
            'ok',
            json.dumps({'reason': reason}),
        ))
        # Запись о возврате (если обработан)
        if proc_at:
            ops.append((
                branch_id, branch_code, 'refund',
                'refund', str(rid),
                proc_at.date(), proc_at,
                f'manager_{random.randint(1,10)}',
                float(amount),
                status,
                json.dumps({'penalty_rub': float(penalty), 'reason': reason}),
            ))

    # Пакеты туров — открытие/закрытие
    cur.execute("""
        SELECT tp.package_id, tp.branch_id, br.code,
               tp.status, tp.created_at, tp.price_rub
        FROM tour_packages tp
        JOIN branches br ON br.branch_id = tp.branch_id
    """)
    for pkg_id, branch_id, branch_code, status, created_at, price in cur.fetchall():
        op_type = 'package_open' if status == 'active' else 'package_close'
        ops.append((
            branch_id, branch_code, op_type,
            'package', str(pkg_id),
            created_at.date() if hasattr(created_at, 'date') else created_at,
            created_at,
            'system',
            float(price),
            'ok',
            json.dumps({'package_status': status}),
        ))

    # Пакетная вставка
    cur.executemany("""
        INSERT INTO operations_log
          (branch_id, branch_code, op_type, entity_type, entity_id,
           op_date, op_time, operator, amount_rub, status, details)
        VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)
    """, ops)

    conn.commit()
    cur.close()
    print(f'   Operations logged: {len(ops)}')


def print_stats(conn):
    cur = conn.cursor()
    print('\n' + '='*65)
    print('📊  DATABASE STATISTICS')
    print('='*65)

    tables = ['branches','destinations','tours','tour_packages','flights',
              'clients','bookings','payments','refunds','operations_log']
    for t in tables:
        cur.execute(f'SELECT COUNT(*) FROM {t}')
        n = cur.fetchone()[0]
        cur.execute(f"SELECT pg_size_pretty(pg_total_relation_size('{t}'))")
        sz = cur.fetchone()[0]
        print(f'  {t:20} | rows: {n:6} | size: {sz}')

    print('\n📅  CALENDAR — next 30 days (sample):')
    print('-'*65)
    cur.execute("""
        SELECT depart_date, branch_city, country, resort, tour_title,
               seats_total, seats_sold, seats_free, price_rub, status
        FROM v_calendar_availability
        WHERE depart_date BETWEEN CURRENT_DATE AND CURRENT_DATE + 30
        ORDER BY depart_date, branch_city
        LIMIT 15
    """)
    rows = cur.fetchall()
    if rows:
        print(f"  {'Дата':<12} {'Филиал':<20} {'Курорт':<18} {'Всего':>6} {'Прод':>5} {'Своб':>5} {'Цена RUB':>10}")
        print('  ' + '-'*63)
        for r in rows:
            print(f"  {str(r[0]):<12} {r[1]:<20} {r[3]:<18} {r[5]:>6} {r[6]:>5} {r[7]:>5} {int(r[8]):>10,}")
    else:
        print('  (нет пакетов в ближайшие 30 дней)')

    print('\n🏢  SALES BY BRANCH:')
    print('-'*65)
    cur.execute('SELECT city, code, total_bookings, total_persons, revenue_rub, cancellations FROM v_branch_sales')
    for r in cur.fetchall():
        rev = f"{int(r[4]):,}" if r[4] else '0'
        print(f"  {r[0]:<22} [{r[1]}]  брон: {r[2]:>4}  чел: {r[3]:>5}  выручка: {rev:>14} руб  отмен: {r[5]:>3}")

    print('\n💳  PAYMENTS & REFUNDS:')
    print('-'*65)
    cur.execute("""
        SELECT
            COUNT(*) FILTER (WHERE status='completed')   AS paid,
            SUM(amount_rub) FILTER (WHERE status='completed') AS total_received,
            COUNT(*) FILTER (WHERE status='pending')     AS pending,
            COUNT(*) FILTER (WHERE status='reversed')    AS reversed
        FROM payments
    """)
    r = cur.fetchone()
    print(f'  Оплачено: {r[0]}  Сумма: {int(r[1] or 0):,} руб  Ожидают: {r[2]}  Возвращено: {r[3]}')

    cur.execute("""
        SELECT
            COUNT(*) FILTER (WHERE status='completed') AS done,
            COUNT(*) FILTER (WHERE status='pending')   AS pend,
            SUM(amount_rub)   FILTER (WHERE status='completed') AS refunded,
            SUM(penalty_rub)  AS total_penalty
        FROM refunds
    """)
    r = cur.fetchone()
    print(f'  Возвратов: {r[0]} завершено, {r[1]} в обработке  '
          f'Возвращено: {int(r[2] or 0):,} руб  Штрафы: {int(r[3] or 0):,} руб')

    print('\n📋  OPERATIONS LOG (top types):')
    print('-'*65)
    cur.execute("""
        SELECT op_type, COUNT(*) AS cnt, SUM(amount_rub) AS total
        FROM operations_log
        GROUP BY op_type ORDER BY cnt DESC
    """)
    for op_type, cnt, total in cur.fetchall():
        total_str = f'{int(total):,} руб' if total else '—'
        print(f'  {op_type:<20} {cnt:>6} записей  {total_str}')

    print('\n🌍  ACTIVE TOURS with free seats (next 60 days):')
    print('-'*65)
    cur.execute("""
        SELECT country, resort, tour_title, MIN(depart_date) AS nearest,
               SUM(seats_free) AS total_free, COUNT(*) AS packages
        FROM v_active_tours_availability
        WHERE depart_date <= CURRENT_DATE + 60
        GROUP BY country, resort, tour_title
        ORDER BY nearest
        LIMIT 12
    """)
    for r in cur.fetchall():
        print(f"  {r[0]:<12} {r[1]:<16} {r[2]:<30} ближ.вылет: {r[3]}  своб: {r[4]}  пакетов: {r[5]}")

    cur.close()


def main():
    print('='*65)
    print('✈️   Travel Agency — PostgreSQL test database creator')
    print('='*65)

    conn = connect()
    print('✅ Connected')

    try:
        drop_and_create_tables(conn)
        branch_ids, dest_ids = insert_reference_data(conn)
        tour_ids = insert_tours(conn, dest_ids)
        package_ids = insert_packages_and_flights(conn, tour_ids, branch_ids)
        booking_records = insert_clients_and_bookings(conn, package_ids, branch_ids)
        insert_payments_and_refunds(conn, booking_records)
        build_operations_log(conn)
        print_stats(conn)

        print('\n' + '='*65)
        print('✅  Done! Travel agency database is ready.')
        print('='*65)
        print("""
Полезные запросы:

  -- Календарный график с остатком мест
  SELECT depart_date, branch_city, country, resort,
         seats_total, seats_sold, seats_free, price_rub
  FROM v_calendar_availability
  WHERE depart_date BETWEEN '2025-06-01' AND '2025-08-31'
  ORDER BY depart_date;

  -- Только активные туры со свободными местами
  SELECT * FROM v_active_tours_availability LIMIT 20;

  -- Продажи по филиалам
  SELECT * FROM v_branch_sales;

  -- Горящие: < 3 свободных мест, вылет в течение 2 недель
  SELECT depart_date, branch_city, resort, tour_title,
         seats_free, price_rub
  FROM v_active_tours_availability
  WHERE seats_free < 3
    AND depart_date <= CURRENT_DATE + 14;
""")

    except Exception as e:
        print(f'\n❌ Error: {e}')
        import traceback
        traceback.print_exc()
        conn.rollback()
    finally:
        conn.close()


if __name__ == '__main__':
    main()
