#!/usr/bin/env python3
"""
Airline GDS (Global Distribution System) — тестовая база данных PostgreSQL.

Имитирует сторонний сервис бронирования авиабилетов, к которому обращаются
туристические агентства при выписке туров. Аналог Sabre / Amadeus / Galileo.

БД: tdtp_airline (отдельная от основной tdtp_test)

Схема:
  gds_airlines          — авиакомпании
  gds_airports          — аэропорты
  gds_routes            — маршруты (регулярные рейсы)
  gds_flights           — конкретные рейсы (дата + статус)
  gds_inventory         — инвентарь мест по классам тарифов
  gds_pnr               — Passenger Name Records (бронирования)
  gds_pnr_passengers    — пассажиры в каждом PNR
  gds_pnr_segments      — сегменты перелёта в PNR
  gds_tickets           — электронные билеты
  gds_changes_log       — лог изменений PNR (аудит)

Связь с travel_agency:
  bookings.gds_pnr_code → gds_pnr.pnr_code  (cross-DB reference по коду)
  bookings.branch_id    — agency_iata в PNR соответствует коду филиала

Запуск:
  python create_airline_gds_db.py

Требования:
  pip install psycopg2-binary
"""

import psycopg2
import random
import string
import uuid
import json
from datetime import datetime, timedelta, date, time as dtime

# ─── конфиг подключения ────────────────────────────────────────────────────────

ADMIN_CONFIG = {
    'host':     'localhost',
    'port':     5432,
    'user':     'tdtp_user',
    'password': 'tdtp_dev_pass_2025',
    'database': 'tdtp_test',   # подключаемся к основной для создания новой БД
}

GDS_CONFIG = {
    'host':     'localhost',
    'port':     5432,
    'user':     'tdtp_user',
    'password': 'tdtp_dev_pass_2025',
    'database': 'tdtp_airline',
}

# ─── справочники ───────────────────────────────────────────────────────────────

AIRLINES = [
    # (iata, icao, name_ru, name_en, country, hub)
    ('SU', 'AFL', 'Аэрофлот',            'Aeroflot',           'RU', 'SVO'),
    ('DP', 'PBD', 'Победа',              'Pobeda',             'RU', 'VKO'),
    ('S7', 'SBI', 'S7 Airlines',         'S7 Airlines',        'RU', 'DME'),
    ('U6', 'SVR', 'Уральские авиалинии', 'Ural Airlines',      'RU', 'SVX'),
    ('N4', 'NWS', 'Nordwind',            'Nordwind Airlines',  'RU', 'SVO'),
    ('ZF', 'AZV', 'Azur Air',            'Azur Air',           'RU', 'VKO'),
    ('X3', 'TUI', 'TUI fly',             'TUI fly',            'DE', 'HAM'),
    ('FZ', 'FDB', 'FlyDubai',            'flydubai',           'AE', 'DXB'),
    ('TK', 'THY', 'Turkish Airlines',    'Turkish Airlines',   'TR', 'IST'),
    ('EK', 'UAE', 'Emirates',            'Emirates',           'AE', 'DXB'),
]

AIRPORTS = [
    # (iata, icao, city_ru, city_en, country_ru, country_en, utc_offset_hours)
    ('SVO', 'UUEE', 'Москва',          'Moscow',        'Россия',  'Russia',  3),
    ('VKO', 'UUWW', 'Москва',          'Moscow',        'Россия',  'Russia',  3),
    ('DME', 'UUDD', 'Москва',          'Moscow',        'Россия',  'Russia',  3),
    ('LED', 'ULLI', 'Санкт-Петербург', 'Saint Petersburg','Россия','Russia',  3),
    ('KZN', 'UWKD', 'Казань',          'Kazan',         'Россия',  'Russia',  3),
    ('SVX', 'USSS', 'Екатеринбург',    'Yekaterinburg', 'Россия',  'Russia',  5),
    ('OVB', 'UNNT', 'Новосибирск',     'Novosibirsk',   'Россия',  'Russia',  7),
    ('KRR', 'URKK', 'Краснодар',       'Krasnodar',     'Россия',  'Russia',  3),
    ('ROV', 'URRR', 'Ростов-на-Дону',  'Rostov-on-Don', 'Россия',  'Russia',  3),
    ('UFA', 'UWUU', 'Уфа',             'Ufa',           'Россия',  'Russia',  5),
    ('AYT', 'LTAI', 'Анталья',         'Antalya',       'Турция',  'Turkey',  3),
    ('HRG', 'HEGN', 'Хургада',         'Hurghada',      'Египет',  'Egypt',   2),
    ('SSH', 'HESH', 'Шарм-эль-Шейх',   'Sharm el-Sheikh','Египет', 'Egypt',   2),
    ('HKT', 'VTSP', 'Пхукет',          'Phuket',        'Таиланд', 'Thailand',7),
    ('DXB', 'OMDB', 'Дубай',           'Dubai',         'ОАЭ',     'UAE',     4),
    ('BCN', 'LEBL', 'Барселона',        'Barcelona',     'Испания', 'Spain',   2),
    ('HER', 'LGIR', 'Ираклион (Крит)', 'Heraklion',     'Греция',  'Greece',  3),
    ('MLE', 'VRMM', 'Мале',            'Male',          'Мальдивы','Maldives',5),
    ('VRA', 'MUVR', 'Варадеро',        'Varadero',      'Куба',    'Cuba',   -4),
    ('TUN', 'DTTA', 'Тунис',           'Tunis',         'Тунис',   'Tunisia', 1),
    ('CXR', 'VVCR', 'Нячанг',          'Nha Trang',     'Вьетнам', 'Vietnam', 7),
    ('LCA', 'LCLK', 'Ларнака',         'Larnaca',       'Кипр',    'Cyprus',  3),
]

# Маршруты: (airline_iata, origin, destination, duration_min, days_of_week, aircraft)
# days_of_week: строка из 7 символов (1=работает, 0=нет), Mon-Sun
ROUTES = [
    # Москва → курорты
    ('SU', 'SVO', 'AYT', 210, '1010101', 'Airbus A321'),
    ('DP', 'VKO', 'AYT', 205, '0101010', 'Boeing 737'),
    ('SU', 'SVO', 'HRG', 285, '1100110', 'Boeing 767'),
    ('S7', 'DME', 'HRG', 290, '0011001', 'Airbus A320'),
    ('SU', 'SVO', 'SSH', 295, '1010100', 'Boeing 767'),
    ('N4', 'SVO', 'HKT', 570, '1000100', 'Boeing 777'),
    ('SU', 'SVO', 'DXB', 360, '1111111', 'Airbus A330'),
    ('ZF', 'VKO', 'BCN', 315, '0101010', 'Boeing 767'),
    ('S7', 'DME', 'HER', 255, '1010101', 'Airbus A320'),
    ('N4', 'SVO', 'MLE', 510, '0100010', 'Boeing 777'),
    ('ZF', 'VKO', 'VRA', 1080,'1000000', 'Boeing 767'),
    ('SU', 'SVO', 'TUN', 285, '0101010', 'Airbus A321'),
    ('N4', 'SVO', 'CXR', 540, '0010010', 'Boeing 767'),
    ('SU', 'SVO', 'LCA', 240, '1010101', 'Airbus A321'),
    # СПб → курорты
    ('S7', 'LED', 'AYT', 225, '1010010', 'Airbus A320'),
    ('U6', 'LED', 'HRG', 305, '0101010', 'Airbus A321'),
    # Регионы → Анталья
    ('U6', 'SVX', 'AYT', 255, '0110110', 'Airbus A321'),
    ('U6', 'OVB', 'AYT', 345, '0100010', 'Airbus A320'),
    ('U6', 'KRR', 'AYT', 165, '1010101', 'Boeing 737'),
    # Чартеры Nordwind
    ('N4', 'SVO', 'AYT', 210, '0010100', 'Boeing 737'),
    ('N4', 'LED', 'AYT', 225, '0001001', 'Boeing 737'),
]

FARE_CLASSES = [
    # (class_code, cabin, base_multiplier, baggage_kg, refundable)
    ('Y', 'economy',  1.00, 20, True),
    ('B', 'economy',  0.85, 20, True),
    ('M', 'economy',  0.72, 20, True),
    ('H', 'economy',  0.60, 10, False),
    ('K', 'economy',  0.50, 10, False),
    ('E', 'economy',  0.38, 0,  False),
    ('J', 'business', 3.20, 32, True),
    ('C', 'business', 2.80, 32, True),
    ('D', 'business', 2.50, 32, False),
]

# Агентства (iata-коды туристических агентств)
AGENCY_CODES = ['МСК', 'СПБ', 'КЗН', 'ЕКБ', 'НСК', 'КДР', 'РНД', 'УФА']


# ─── утилиты ───────────────────────────────────────────────────────────────────

def gen_pnr():
    return ''.join(random.choices(string.ascii_uppercase + string.digits, k=6))

def gen_ticket_number(airline_iata: str) -> str:
    """13-значный номер электронного билета: 3-значный код авиалинии + 10 цифр."""
    airline_codes = {
        'SU': '555', 'DP': '636', 'S7': '421', 'U6': '096',
        'N4': '067', 'ZF': '064', 'X3': '617', 'FZ': '141',
        'TK': '235', 'EK': '176',
    }
    prefix = airline_codes.get(airline_iata, '000')
    return prefix + ''.join(random.choices(string.digits, k=10))

def random_seat(cabin: str) -> str:
    row = random.randint(1, 8) if cabin == 'business' else random.randint(10, 36)
    seat = random.choice('ABCDEF')
    return f'{row}{seat}'


# ─── создание БД ───────────────────────────────────────────────────────────────

def create_database():
    conn = psycopg2.connect(**ADMIN_CONFIG)
    conn.autocommit = True
    cur = conn.cursor()
    cur.execute("SELECT 1 FROM pg_database WHERE datname = 'tdtp_airline'")
    if not cur.fetchone():
        cur.execute("CREATE DATABASE tdtp_airline OWNER tdtp_user ENCODING 'UTF8'")
        print('✅ Database tdtp_airline created')
    else:
        print('ℹ️  Database tdtp_airline already exists')
    cur.close()
    conn.close()


def connect_gds():
    return psycopg2.connect(**GDS_CONFIG)


# ─── DDL ───────────────────────────────────────────────────────────────────────

def drop_and_create_tables(conn):
    cur = conn.cursor()
    print('🗑  Dropping old GDS tables...')
    cur.execute("""
        DROP TABLE IF EXISTS gds_changes_log      CASCADE;
        DROP TABLE IF EXISTS gds_tickets          CASCADE;
        DROP TABLE IF EXISTS gds_pnr_segments     CASCADE;
        DROP TABLE IF EXISTS gds_pnr_passengers   CASCADE;
        DROP TABLE IF EXISTS gds_pnr              CASCADE;
        DROP TABLE IF EXISTS gds_inventory        CASCADE;
        DROP TABLE IF EXISTS gds_flights          CASCADE;
        DROP TABLE IF EXISTS gds_routes           CASCADE;
        DROP TABLE IF EXISTS gds_airports         CASCADE;
        DROP TABLE IF EXISTS gds_airlines         CASCADE;
    """)

    print('📋 Creating GDS tables...')
    cur.execute("""
        CREATE TABLE gds_airlines (
            airline_id   SERIAL PRIMARY KEY,
            iata_code    CHAR(2) NOT NULL UNIQUE,
            icao_code    CHAR(3) NOT NULL,
            name_ru      VARCHAR(100) NOT NULL,
            name_en      VARCHAR(100) NOT NULL,
            country      VARCHAR(50)  NOT NULL,
            hub_airport  CHAR(3)      NOT NULL,
            is_active    BOOLEAN DEFAULT true
        );

        CREATE TABLE gds_airports (
            airport_id   SERIAL PRIMARY KEY,
            iata_code    CHAR(3) NOT NULL UNIQUE,
            icao_code    CHAR(4) NOT NULL,
            city_ru      VARCHAR(100) NOT NULL,
            city_en      VARCHAR(100) NOT NULL,
            country_ru   VARCHAR(100) NOT NULL,
            country_en   VARCHAR(100) NOT NULL,
            utc_offset   SMALLINT NOT NULL DEFAULT 0
        );

        CREATE TABLE gds_routes (
            route_id      SERIAL PRIMARY KEY,
            airline_id    INTEGER NOT NULL REFERENCES gds_airlines(airline_id),
            flight_number VARCHAR(8) NOT NULL,      -- SU1234
            origin_iata   CHAR(3) NOT NULL REFERENCES gds_airports(iata_code),
            dest_iata     CHAR(3) NOT NULL REFERENCES gds_airports(iata_code),
            duration_min  SMALLINT NOT NULL,
            aircraft_type VARCHAR(30) NOT NULL,
            days_of_week  CHAR(7) NOT NULL,         -- 1010101 = Пн,Ср,Пт,Вс
            is_active     BOOLEAN DEFAULT true,
            UNIQUE (airline_id, flight_number)
        );

        CREATE TABLE gds_flights (
            flight_id    BIGSERIAL PRIMARY KEY,
            route_id     INTEGER NOT NULL REFERENCES gds_routes(route_id),
            flight_number VARCHAR(8) NOT NULL,
            flight_date  DATE NOT NULL,
            depart_utc   TIMESTAMP NOT NULL,
            arrive_utc   TIMESTAMP NOT NULL,
            status       VARCHAR(20) NOT NULL DEFAULT 'scheduled',
                         -- scheduled / boarding / departed / landed / cancelled / delayed
            delay_min    SMALLINT DEFAULT 0,
            tail_number  VARCHAR(10),
            UNIQUE (flight_number, flight_date)
        );
        CREATE INDEX gds_flights_date_idx ON gds_flights (flight_date);

        CREATE TABLE gds_inventory (
            inv_id        BIGSERIAL PRIMARY KEY,
            flight_id     BIGINT NOT NULL REFERENCES gds_flights(flight_id),
            fare_class    CHAR(1) NOT NULL,          -- Y B M H K E J C D
            cabin_class   VARCHAR(10) NOT NULL,      -- economy / business
            seats_total   SMALLINT NOT NULL,
            seats_sold    SMALLINT NOT NULL DEFAULT 0,
            seats_blocked SMALLINT NOT NULL DEFAULT 0,
            price_rub     NUMERIC(12,2) NOT NULL,
            price_usd     NUMERIC(10,2) NOT NULL,
            fare_basis    VARCHAR(20) NOT NULL,
            baggage_kg    SMALLINT NOT NULL DEFAULT 20,
            is_refundable BOOLEAN NOT NULL DEFAULT true,
            is_available  BOOLEAN NOT NULL DEFAULT true,
            last_updated  TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
            UNIQUE (flight_id, fare_class)
        );
        CREATE INDEX gds_inv_flight_idx     ON gds_inventory (flight_id);
        CREATE INDEX gds_inv_available_idx  ON gds_inventory (is_available, fare_class);

        CREATE TABLE gds_pnr (
            pnr_code         CHAR(6) PRIMARY KEY,
            created_at       TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
            agency_code      CHAR(3) NOT NULL,       -- код агентства (= branch_code в travel_agency)
            agent_id         VARCHAR(30),
            status           VARCHAR(20) NOT NULL DEFAULT 'active',
                             -- active / ticketed / cancelled / expired / no_show
            ticket_time_limit TIMESTAMP WITH TIME ZONE,
            total_amount_rub  NUMERIC(14,2),
            currency          CHAR(3) DEFAULT 'RUB',
            notes            TEXT
        );
        CREATE INDEX gds_pnr_agency_idx  ON gds_pnr (agency_code);
        CREATE INDEX gds_pnr_status_idx  ON gds_pnr (status);

        CREATE TABLE gds_pnr_passengers (
            passenger_id  SERIAL PRIMARY KEY,
            pnr_code      CHAR(6) NOT NULL REFERENCES gds_pnr(pnr_code),
            last_name     VARCHAR(60) NOT NULL,
            first_name    VARCHAR(60) NOT NULL,
            middle_name   VARCHAR(60),
            birth_date    DATE,
            gender        CHAR(1) NOT NULL DEFAULT 'M',   -- M / F
            doc_type      VARCHAR(10) NOT NULL DEFAULT 'passport',
            doc_number    VARCHAR(20) NOT NULL,
            doc_expires   DATE,
            nationality   CHAR(2) NOT NULL DEFAULT 'RU',
            ff_number     VARCHAR(20)                     -- frequent flyer
        );
        CREATE INDEX gds_pax_pnr_idx ON gds_pnr_passengers (pnr_code);

        CREATE TABLE gds_pnr_segments (
            segment_id    BIGSERIAL PRIMARY KEY,
            pnr_code      CHAR(6) NOT NULL REFERENCES gds_pnr(pnr_code),
            flight_id     BIGINT NOT NULL REFERENCES gds_flights(flight_id),
            fare_class    CHAR(1) NOT NULL,
            seat_number   VARCHAR(4),
            meal_code     CHAR(4) DEFAULT 'KSML',   -- KSML/VGML/NSML/BBML
            baggage_kg    SMALLINT DEFAULT 20,
            status        VARCHAR(20) NOT NULL DEFAULT 'confirmed',
                          -- confirmed / cancelled / checked_in / boarded / no_show
            checked_in_at TIMESTAMP WITH TIME ZONE,
            boarding_pass VARCHAR(30)
        );
        CREATE INDEX gds_seg_pnr_idx    ON gds_pnr_segments (pnr_code);
        CREATE INDEX gds_seg_flight_idx ON gds_pnr_segments (flight_id);

        CREATE TABLE gds_tickets (
            ticket_number   VARCHAR(14) PRIMARY KEY,  -- 13-значный e-ticket
            pnr_code        CHAR(6) NOT NULL REFERENCES gds_pnr(pnr_code),
            passenger_id    INTEGER NOT NULL REFERENCES gds_pnr_passengers(passenger_id),
            segment_id      BIGINT NOT NULL REFERENCES gds_pnr_segments(segment_id),
            fare_basis      VARCHAR(20) NOT NULL,
            fare_amount_rub NUMERIC(12,2) NOT NULL,
            taxes_rub       NUMERIC(10,2) NOT NULL DEFAULT 0,
            total_rub       NUMERIC(12,2) NOT NULL,
            issued_at       TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
            issued_by       VARCHAR(30),
            status          VARCHAR(20) NOT NULL DEFAULT 'open',
                            -- open / used / voided / refunded / exchanged
            void_at         TIMESTAMP WITH TIME ZONE,
            refund_at       TIMESTAMP WITH TIME ZONE,
            refund_amount_rub NUMERIC(12,2),
            coupon_status   VARCHAR(10) DEFAULT 'open'  -- open / used / void / refund
        );
        CREATE INDEX gds_tkt_pnr_idx ON gds_tickets (pnr_code);
        CREATE INDEX gds_tkt_pax_idx ON gds_tickets (passenger_id);

        CREATE TABLE gds_changes_log (
            change_id    BIGSERIAL PRIMARY KEY,
            pnr_code     CHAR(6) NOT NULL REFERENCES gds_pnr(pnr_code),
            changed_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
            change_type  VARCHAR(30) NOT NULL,
                         -- pnr_created / segment_added / ticket_issued / ticket_voided
                         -- pnr_cancelled / passenger_added / seat_assigned
                         -- status_change / price_change / no_show_marked
            changed_by   VARCHAR(30),
            old_value    TEXT,
            new_value    TEXT,
            amount_rub   NUMERIC(12,2),
            details      JSONB
        );
        CREATE INDEX gds_chg_pnr_idx  ON gds_changes_log (pnr_code);
        CREATE INDEX gds_chg_type_idx ON gds_changes_log (change_type);
    """)

    # Views
    cur.execute("""
        CREATE OR REPLACE VIEW v_gds_availability AS
        SELECT
            f.flight_id,
            f.flight_number,
            f.flight_date,
            f.depart_utc,
            f.arrive_utc,
            f.status            AS flight_status,
            a_org.city_ru       AS origin_city,
            f_rt.origin_iata,
            a_dst.city_ru       AS dest_city,
            f_rt.dest_iata,
            al.iata_code        AS airline,
            al.name_ru          AS airline_name,
            i.fare_class,
            i.cabin_class,
            i.seats_total,
            i.seats_sold,
            i.seats_total - i.seats_sold - i.seats_blocked AS seats_free,
            i.price_rub,
            i.fare_basis,
            i.baggage_kg,
            i.is_refundable,
            i.is_available
        FROM gds_flights f
        JOIN gds_routes   f_rt ON f_rt.route_id  = f.route_id
        JOIN gds_airlines al   ON al.airline_id  = f_rt.airline_id
        JOIN gds_airports a_org ON a_org.iata_code = f_rt.origin_iata
        JOIN gds_airports a_dst ON a_dst.iata_code = f_rt.dest_iata
        JOIN gds_inventory i   ON i.flight_id    = f.flight_id
        WHERE f.status NOT IN ('cancelled')
          AND i.is_available = true
        ORDER BY f.flight_date, f.depart_utc, i.price_rub;
    """)

    cur.execute("""
        CREATE OR REPLACE VIEW v_gds_pnr_detail AS
        SELECT
            p.pnr_code,
            p.agency_code,
            p.status             AS pnr_status,
            p.created_at,
            p.total_amount_rub,
            COUNT(DISTINCT pp.passenger_id)  AS passengers,
            COUNT(DISTINCT ps.segment_id)    AS segments,
            COUNT(DISTINCT t.ticket_number)  AS tickets,
            MIN(f.flight_date)               AS first_flight_date,
            MAX(f.flight_date)               AS last_flight_date
        FROM gds_pnr p
        LEFT JOIN gds_pnr_passengers pp ON pp.pnr_code   = p.pnr_code
        LEFT JOIN gds_pnr_segments   ps ON ps.pnr_code   = p.pnr_code
        LEFT JOIN gds_flights        f  ON f.flight_id   = ps.flight_id
        LEFT JOIN gds_tickets        t  ON t.pnr_code    = p.pnr_code
        GROUP BY p.pnr_code, p.agency_code, p.status, p.created_at, p.total_amount_rub
        ORDER BY p.created_at DESC;
    """)

    conn.commit()
    cur.close()
    print('✅ GDS tables and views created')


# ─── справочники ───────────────────────────────────────────────────────────────

def insert_reference_data(conn):
    cur = conn.cursor()

    print('✈️  Inserting airlines...')
    airline_ids = {}
    for iata, icao, name_ru, name_en, country, hub in AIRLINES:
        cur.execute("""
            INSERT INTO gds_airlines (iata_code, icao_code, name_ru, name_en, country, hub_airport)
            VALUES (%s,%s,%s,%s,%s,%s) RETURNING airline_id
        """, (iata, icao, name_ru, name_en, country, hub))
        airline_ids[iata] = cur.fetchone()[0]

    print('🏠 Inserting airports...')
    for row in AIRPORTS:
        iata, icao, city_ru, city_en, country_ru, country_en, utc = row
        cur.execute("""
            INSERT INTO gds_airports
              (iata_code, icao_code, city_ru, city_en, country_ru, country_en, utc_offset)
            VALUES (%s,%s,%s,%s,%s,%s,%s)
        """, (iata, icao, city_ru, city_en, country_ru, country_en, utc))

    print('🛣  Inserting routes...')
    route_ids = {}
    for i, (airline_iata, origin, dest, dur_min, days, aircraft) in enumerate(ROUTES):
        flight_num = f"{airline_iata}{random.randint(100, 999)}"
        cur.execute("""
            INSERT INTO gds_routes
              (airline_id, flight_number, origin_iata, dest_iata,
               duration_min, aircraft_type, days_of_week)
            VALUES (%s,%s,%s,%s,%s,%s,%s) RETURNING route_id
        """, (airline_ids[airline_iata], flight_num, origin, dest, dur_min, aircraft, days))
        route_ids[i] = (cur.fetchone()[0], airline_iata, origin, dest, dur_min, aircraft, days, flight_num)

    conn.commit()
    cur.close()
    print(f'   Airlines: {len(airline_ids)}, Airports: {len(AIRPORTS)}, Routes: {len(route_ids)}')
    return route_ids


# ─── рейсы и инвентарь ─────────────────────────────────────────────────────────

def insert_flights_and_inventory(conn, route_ids):
    """
    Генерирует рейсы в окне [-60 .. +120 дней] для каждого маршрута
    в соответствии с расписанием days_of_week.
    """
    cur = conn.cursor()
    print('\n🛫 Inserting flights & inventory...')

    today = date.today()
    window_start = today - timedelta(days=60)
    window_end   = today + timedelta(days=120)

    flight_ids = []

    day_map = {0: 'Mon', 1: 'Tue', 2: 'Wed', 3: 'Thu', 4: 'Fri', 5: 'Sat', 6: 'Sun'}
    usd_rate = 92.5  # условный курс для расчёта price_usd

    for _, (route_id, airline_iata, origin, dest, dur_min, aircraft, days, fnum) in route_ids.items():
        # Базовая цена маршрута
        base_price = {
            'AYT': 12000, 'HRG': 15000, 'SSH': 16000, 'HKT': 28000,
            'DXB': 20000, 'BCN': 22000, 'HER': 18000, 'MLE': 35000,
            'VRA': 55000, 'TUN': 14000, 'CXR': 32000, 'LCA': 17000,
        }.get(dest, 10000)

        # Перебираем дни в окне
        d = window_start
        while d <= window_end:
            weekday = d.weekday()  # 0=Mon .. 6=Sun
            if days[weekday] == '1':
                # Время вылета
                dep_hour = random.randint(4, 23)
                dep_min  = random.choice([0, 15, 30, 45])
                dep_utc  = datetime.combine(d, dtime(dep_hour, dep_min)) - timedelta(hours=3)  # MSK→UTC
                arr_utc  = dep_utc + timedelta(minutes=dur_min)

                # Статус рейса
                if d < today - timedelta(days=1):
                    status = random.choices(
                        ['landed', 'landed', 'landed', 'delayed'],
                        weights=[8, 8, 8, 1]
                    )[0]
                elif d == today:
                    status = random.choice(['scheduled', 'boarding', 'departed'])
                else:
                    status = 'scheduled'

                # ~3% рейсов отменено
                if random.random() < 0.03:
                    status = 'cancelled'

                delay_min = 0
                if status == 'delayed':
                    delay_min = random.choice([15, 30, 45, 60, 90, 120])

                cur.execute("""
                    INSERT INTO gds_flights
                      (route_id, flight_number, flight_date,
                       depart_utc, arrive_utc, status, delay_min, tail_number)
                    VALUES (%s,%s,%s,%s,%s,%s,%s,%s)
                    ON CONFLICT (flight_number, flight_date) DO NOTHING
                    RETURNING flight_id
                """, (route_id, fnum, d, dep_utc, arr_utc, status, delay_min,
                      f'{airline_iata}-{random.randint(100,999)}'))
                row = cur.fetchone()
                if not row:
                    d += timedelta(days=1)
                    continue
                flight_id = row[0]
                flight_ids.append((flight_id, airline_iata, d, base_price, status))

                # Инвентарь по классам тарифов
                total_seats = {'Boeing 737': 189, 'Airbus A320': 180,
                               'Airbus A321': 220, 'Boeing 767': 250,
                               'Boeing 777': 350, 'Airbus A330': 300}.get(aircraft, 180)

                for class_code, cabin, base_mult, baggage, refundable in FARE_CLASSES:
                    if cabin == 'business':
                        seats = random.randint(8, 24)
                    else:
                        seats = int(total_seats * random.uniform(0.10, 0.20))

                    # Продано: для прошлых рейсов — много, для будущих — меньше
                    if d < today:
                        sold_pct = random.uniform(0.70, 1.00)
                    else:
                        sold_pct = random.uniform(0.10, 0.70)
                    seats_sold = min(int(seats * sold_pct), seats)

                    price_rub = round(base_price * base_mult * random.uniform(0.9, 1.15), -2)
                    price_usd = round(price_rub / usd_rate, 2)
                    fare_basis = f"{class_code}{'R' if refundable else 'N'}{random.randint(1,9)}{'RT' if random.random()>0.5 else 'OW'}"
                    is_avail = seats_sold < seats and status != 'cancelled'

                    cur.execute("""
                        INSERT INTO gds_inventory
                          (flight_id, fare_class, cabin_class, seats_total,
                           seats_sold, price_rub, price_usd, fare_basis,
                           baggage_kg, is_refundable, is_available)
                        VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)
                    """, (flight_id, class_code, cabin, seats, seats_sold,
                          price_rub, price_usd, fare_basis,
                          baggage, refundable, is_avail))

            d += timedelta(days=1)

    conn.commit()
    cur.close()
    print(f'   Flights: {len(flight_ids)} (routes: {len(route_ids)}, window: {window_start}–{window_end})')
    return flight_ids


# ─── PNR, пассажиры, сегменты, билеты ─────────────────────────────────────────

def insert_pnr_data(conn, flight_ids):
    """
    Создаёт ~1800 PNR с пассажирами, сегментами и билетами.
    PNR-коды сохраняются — они же будут вставлены в bookings.gds_pnr_code
    travel_agency DB.
    """
    cur = conn.cursor()
    print('\n🎫 Inserting PNR / passengers / tickets...')

    # Данные для генерации пассажиров
    last_names  = ['IVANOV','PETROV','SIDOROV','KOZLOV','NOVIKOV',
                   'MOROZOV','VOLKOV','SOLOVEV','POPOV','LEBEDEV',
                   'SMIRNOV','KUZNETSOV','FEDOROV','ALEKSEEV','ANDREEV']
    first_names = ['ALEKSEI','DMITRY','ANDREI','SERGEI','IVAN',
                   'ELENA','OLGA','NATALIA','TATIANA','MARIA',
                   'MAXIM','NIKOLAI','VICTOR','YURI','ARTEM']

    # Выбираем рейсы только из будущего и ближайшего прошлого (-30 дней)
    today = date.today()
    bookable_flights = [(fid, al, fd, bp, fs) for fid, al, fd, bp, fs in flight_ids
                        if fd >= today - timedelta(days=30) and fs != 'cancelled']

    if not bookable_flights:
        print('  ⚠️  No bookable flights found')
        return []

    pnr_list = []
    total_tickets = 0
    target_pnr = 1800

    for _ in range(target_pnr):
        pnr_code = gen_pnr()
        agency   = random.choice(AGENCY_CODES)
        agent_id = f'AGT{random.randint(100,999)}'

        # Выбираем рейс
        flight_id, airline_iata, flight_date, base_price, flight_status = random.choice(bookable_flights)

        # Статус PNR
        if flight_date < today:
            pnr_status = random.choices(
                ['ticketed', 'ticketed', 'ticketed', 'cancelled', 'no_show'],
                weights=[7, 7, 7, 2, 1]
            )[0]
        else:
            pnr_status = random.choices(
                ['active', 'ticketed', 'ticketed', 'cancelled'],
                weights=[2, 5, 5, 1]
            )[0]

        # Время создания PNR
        days_before = random.randint(5, 90)
        created_at  = datetime.combine(flight_date - timedelta(days=days_before),
                                       dtime(random.randint(8,20), random.randint(0,59)))
        ttl = created_at + timedelta(hours=random.randint(12, 72))

        # Выбираем класс тарифа
        cur.execute("""
            SELECT fare_class, cabin_class, price_rub, fare_basis, baggage_kg
            FROM gds_inventory
            WHERE flight_id = %s AND is_available = true
            ORDER BY price_rub
            LIMIT 10
        """, (flight_id,))
        inv_rows = cur.fetchall()
        if not inv_rows:
            continue
        fare_class, cabin, price_rub, fare_basis, baggage = random.choice(inv_rows)

        # Кол-во пассажиров 1-3
        n_pax = random.choices([1, 2, 3], weights=[5, 3, 1])[0]
        total_amount = round(float(price_rub) * n_pax, 2)

        cur.execute("""
            INSERT INTO gds_pnr
              (pnr_code, created_at, agency_code, agent_id, status,
               ticket_time_limit, total_amount_rub)
            VALUES (%s,%s,%s,%s,%s,%s,%s)
        """, (pnr_code, created_at, agency, agent_id, pnr_status, ttl, total_amount))

        # Пассажиры
        passenger_ids = []
        for _ in range(n_pax):
            ln = random.choice(last_names)
            fn = random.choice(first_names)
            birth = date(1960, 1, 1) + timedelta(days=random.randint(0, 20000))
            gender = random.choice('MF')
            doc_num = f'{random.randint(1000,9999)} {random.randint(100000,999999)}'
            doc_exp = date.today() + timedelta(days=random.randint(180, 1800))

            cur.execute("""
                INSERT INTO gds_pnr_passengers
                  (pnr_code, last_name, first_name, birth_date, gender,
                   doc_type, doc_number, doc_expires, nationality)
                VALUES (%s,%s,%s,%s,%s,'passport',%s,%s,'RU')
                RETURNING passenger_id
            """, (pnr_code, ln, fn, birth, gender, doc_num, doc_exp))
            passenger_ids.append(cur.fetchone()[0])

        # Сегменты (OUT + RET для тура)
        # OUT рейс уже выбран, RET — опционально (для тура)
        is_round_trip = random.random() > 0.3

        segment_ids = []
        for pax_id in passenger_ids:
            seat = random_seat(cabin)
            meal = random.choice(['KSML', 'VGML', 'NSML', 'MOML'])
            seg_status = 'confirmed'
            if pnr_status == 'cancelled':
                seg_status = 'cancelled'
            elif pnr_status == 'no_show':
                seg_status = 'no_show'
            elif flight_status in ('landed', 'departed'):
                seg_status = random.choices(
                    ['boarded', 'no_show', 'checked_in'],
                    weights=[8, 1, 1]
                )[0]

            checked_in_at = None
            boarding_pass = None
            if seg_status in ('checked_in', 'boarded'):
                checked_in_at = datetime.combine(flight_date, dtime(random.randint(3,5), random.randint(0,59)))
                boarding_pass = f'BP{pnr_code}{random.randint(100,999)}'

            cur.execute("""
                INSERT INTO gds_pnr_segments
                  (pnr_code, flight_id, fare_class, seat_number,
                   meal_code, baggage_kg, status, checked_in_at, boarding_pass)
                VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s)
                RETURNING segment_id
            """, (pnr_code, flight_id, fare_class, seat,
                  meal, baggage, seg_status, checked_in_at, boarding_pass))
            segment_ids.append((cur.fetchone()[0], pax_id))

        # Инкрементируем seats_sold
        cur.execute("""
            UPDATE gds_inventory
            SET seats_sold = seats_sold + %s
            WHERE flight_id = %s AND fare_class = %s
        """, (n_pax, flight_id, fare_class))

        # Билеты (только для ticketed PNR)
        ticket_status = 'open'
        if pnr_status == 'ticketed':
            if flight_date < today:
                ticket_status = random.choices(['used','used','voided','refunded'],
                                               weights=[7,7,1,1])[0]

            issued_at = created_at + timedelta(hours=random.randint(1, 48))

            for seg_id, pax_id in segment_ids:
                tkt_num   = gen_ticket_number(airline_iata)
                taxes     = round(float(price_rub) * 0.12, 2)
                total_tkt = round(float(price_rub) + taxes, 2)

                void_at = refund_at = refund_amount = None
                coupon  = 'open'
                if ticket_status == 'voided':
                    void_at = issued_at + timedelta(hours=random.randint(1, 24))
                    coupon  = 'void'
                elif ticket_status == 'refunded':
                    refund_at = issued_at + timedelta(hours=random.randint(24, 168))
                    refund_amount = round(total_tkt * random.uniform(0.60, 0.95), 2)
                    coupon = 'refund'
                elif ticket_status == 'used':
                    coupon = 'used'

                cur.execute("""
                    INSERT INTO gds_tickets
                      (ticket_number, pnr_code, passenger_id, segment_id,
                       fare_basis, fare_amount_rub, taxes_rub, total_rub,
                       issued_at, issued_by, status,
                       void_at, refund_at, refund_amount_rub, coupon_status)
                    VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)
                """, (tkt_num, pnr_code, pax_id, seg_id,
                      fare_basis, float(price_rub), taxes, total_tkt,
                      issued_at, f'AGT{random.randint(100,999)}', ticket_status,
                      void_at, refund_at, refund_amount, coupon))
                total_tickets += 1

        # Лог изменений PNR
        # Создание PNR
        cur.execute("""
            INSERT INTO gds_changes_log
              (pnr_code, changed_at, change_type, changed_by, new_value, details)
            VALUES (%s,%s,'pnr_created',%s,%s,%s)
        """, (pnr_code, created_at, agent_id, pnr_status,
              json.dumps({'agency': agency, 'passengers': n_pax})))

        if pnr_status == 'ticketed':
            cur.execute("""
                INSERT INTO gds_changes_log
                  (pnr_code, changed_at, change_type, changed_by, amount_rub, details)
                VALUES (%s,%s,'ticket_issued',%s,%s,%s)
            """, (pnr_code, created_at + timedelta(hours=24),
                  agent_id, total_amount,
                  json.dumps({'tickets': len(segment_ids), 'status': ticket_status})))

        if pnr_status == 'cancelled':
            cancel_at = created_at + timedelta(hours=random.randint(2, 120))
            cur.execute("""
                INSERT INTO gds_changes_log
                  (pnr_code, changed_at, change_type, changed_by,
                   old_value, new_value, details)
                VALUES (%s,%s,'pnr_cancelled',%s,'active','cancelled',%s)
            """, (pnr_code, cancel_at, agent_id,
                  json.dumps({'reason': random.choice(['client_request','no_payment','schedule_change'])})))

        pnr_list.append(pnr_code)

        if len(pnr_list) % 200 == 0:
            conn.commit()
            print(f'   ... {len(pnr_list)} PNRs created')

    conn.commit()
    cur.close()
    print(f'   PNRs: {len(pnr_list)}, Tickets: {total_tickets}')
    return pnr_list


# ─── статистика ────────────────────────────────────────────────────────────────

def print_stats(conn):
    cur = conn.cursor()
    print('\n' + '='*65)
    print('📊  GDS DATABASE STATISTICS')
    print('='*65)

    tables = ['gds_airlines','gds_airports','gds_routes','gds_flights',
              'gds_inventory','gds_pnr','gds_pnr_passengers',
              'gds_pnr_segments','gds_tickets','gds_changes_log']
    for t in tables:
        cur.execute(f'SELECT COUNT(*) FROM {t}')
        n = cur.fetchone()[0]
        cur.execute(f"SELECT pg_size_pretty(pg_total_relation_size('{t}'))")
        sz = cur.fetchone()[0]
        print(f'  {t:25} | rows: {n:6} | size: {sz}')

    print('\n✈️   FLIGHT STATUS BREAKDOWN:')
    print('-'*65)
    cur.execute("""
        SELECT status, COUNT(*) AS flights,
               SUM(CASE WHEN status != 'cancelled' THEN 1 ELSE 0 END) AS active
        FROM gds_flights
        GROUP BY status ORDER BY flights DESC
    """)
    for row in cur.fetchall():
        print(f'  {row[0]:<15} {row[1]:>6} рейсов')

    print('\n🎫  PNR STATUS BREAKDOWN:')
    print('-'*65)
    cur.execute("""
        SELECT status, COUNT(*) FROM gds_pnr GROUP BY status ORDER BY count DESC
    """)
    for row in cur.fetchall():
        print(f'  {row[0]:<15} {row[1]:>6} PNR')

    print('\n💰  INVENTORY (top 10 cheapest available):')
    print('-'*65)
    cur.execute("""
        SELECT flight_number, flight_date, origin_city, dest_city,
               fare_class, seats_free, price_rub
        FROM v_gds_availability
        WHERE flight_date >= CURRENT_DATE
        ORDER BY price_rub ASC
        LIMIT 10
    """)
    for r in cur.fetchall():
        print(f'  {r[0]:<8} {str(r[1]):<12} {r[2]:<20} → {r[3]:<18} '
              f'[{r[4]}] своб:{r[5]:>3} {int(r[6]):>8,} руб')

    print('\n')
    cur.close()


# ─── main ──────────────────────────────────────────────────────────────────────

def main():
    print('='*65)
    print('✈️   Airline GDS — PostgreSQL test database creator')
    print(f'    DB: tdtp_airline  (host: {GDS_CONFIG["host"]}:{GDS_CONFIG["port"]})')
    print('='*65)

    create_database()

    conn = connect_gds()
    print('✅ Connected to tdtp_airline')

    try:
        drop_and_create_tables(conn)
        route_ids  = insert_reference_data(conn)
        flight_ids = insert_flights_and_inventory(conn, route_ids)
        pnr_list   = insert_pnr_data(conn, flight_ids)
        print_stats(conn)

        print('='*65)
        print('✅  Done! Airline GDS database is ready.')
        print('='*65)
        print(f"""
Полезные запросы:

  -- Доступные места (следующие 30 дней)
  SELECT flight_number, flight_date, origin_city, dest_city,
         fare_class, seats_free, price_rub
  FROM v_gds_availability
  WHERE flight_date BETWEEN CURRENT_DATE AND CURRENT_DATE + 30
  ORDER BY flight_date, price_rub;

  -- Детали PNR
  SELECT * FROM v_gds_pnr_detail WHERE pnr_status = 'ticketed' LIMIT 10;

  -- Загрузка рейса
  SELECT fare_class, seats_total, seats_sold,
         seats_total - seats_sold AS seats_free, price_rub
  FROM gds_inventory
  WHERE flight_id = 1 ORDER BY price_rub;

  -- PNR агентства КЗН за последние 7 дней
  SELECT pnr_code, status, total_amount_rub, created_at
  FROM gds_pnr
  WHERE agency_code = 'КЗН'
    AND created_at >= NOW() - INTERVAL '7 days';
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
