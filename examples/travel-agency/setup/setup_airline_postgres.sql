-- Airline Database — PostgreSQL
-- Node: postgres-airline (localhost:5434, db=tdtp_airline)
-- Publishes:  airline.flights.updated, airline.reservations.updated → RabbitMQ

DROP TABLE IF EXISTS flight_reservations CASCADE;
DROP TABLE IF EXISTS flights CASCADE;
DROP TABLE IF EXISTS aircraft CASCADE;
DROP TABLE IF EXISTS airports CASCADE;

-- ── 1. Airports ──────────────────────────────────────────────────────────────
CREATE TABLE airports (
    airport_id   SERIAL PRIMARY KEY,
    iata_code    CHAR(3)       NOT NULL UNIQUE,
    name         VARCHAR(100)  NOT NULL,
    city         VARCHAR(100)  NOT NULL,
    country_code CHAR(2)       NOT NULL,
    timezone     VARCHAR(50)   NOT NULL,
    latitude     DECIMAL(8,6),
    longitude    DECIMAL(9,6),
    is_hub       BOOLEAN       DEFAULT FALSE
);

CREATE INDEX idx_airports_iata ON airports(iata_code);

-- ── 2. Aircraft fleet ────────────────────────────────────────────────────────
CREATE TABLE aircraft (
    aircraft_id       SERIAL PRIMARY KEY,
    registration      VARCHAR(10)   NOT NULL UNIQUE,
    model             VARCHAR(50)   NOT NULL,
    manufacturer      VARCHAR(50)   NOT NULL,
    economy_seats     SMALLINT      NOT NULL,
    business_seats    SMALLINT      NOT NULL DEFAULT 0,
    range_km          INTEGER       NOT NULL,
    manufactured_year SMALLINT,
    is_active         BOOLEAN       DEFAULT TRUE
);

CREATE INDEX idx_aircraft_active ON aircraft(is_active) WHERE is_active = TRUE;

-- ── 3. Flights ───────────────────────────────────────────────────────────────
CREATE TABLE flights (
    flight_id              SERIAL PRIMARY KEY,
    flight_number          VARCHAR(10)   NOT NULL,
    origin_airport_id      INTEGER       NOT NULL REFERENCES airports(airport_id),
    destination_airport_id INTEGER       NOT NULL REFERENCES airports(airport_id),
    departure_time         TIMESTAMP     NOT NULL,
    arrival_time           TIMESTAMP     NOT NULL,
    duration_minutes       SMALLINT      NOT NULL,
    aircraft_id            INTEGER       NOT NULL REFERENCES aircraft(aircraft_id),
    economy_price          DECIMAL(10,2) NOT NULL,
    business_price         DECIMAL(10,2),
    available_economy      SMALLINT      NOT NULL,
    available_business     SMALLINT      DEFAULT 0,
    status                 VARCHAR(20)   CHECK (status IN (
                               'Scheduled','Boarding','Departed',
                               'Arrived','Cancelled','Delayed'))
                           DEFAULT 'Scheduled',
    delay_minutes          SMALLINT      DEFAULT 0,
    notes                  TEXT,
    created_at             TIMESTAMP     DEFAULT NOW(),
    last_updated           TIMESTAMP     DEFAULT NOW()
);

CREATE INDEX idx_flights_departure  ON flights(departure_time);
CREATE INDEX idx_flights_status     ON flights(status);
CREATE INDEX idx_flights_origin     ON flights(origin_airport_id);
CREATE INDEX idx_flights_updated    ON flights(last_updated);

-- ── 4. Flight reservations ───────────────────────────────────────────────────
CREATE TABLE flight_reservations (
    reservation_id      SERIAL PRIMARY KEY,
    flight_id           INTEGER       NOT NULL REFERENCES flights(flight_id),
    booking_ref_external VARCHAR(30)  NOT NULL,   -- tour booking ref from travel agency
    passenger_name      VARCHAR(100)  NOT NULL,
    seat_class          VARCHAR(20)   CHECK (seat_class IN ('Economy','Business'))
                        DEFAULT 'Economy',
    seat_number         VARCHAR(5),
    price_paid          DECIMAL(10,2) NOT NULL,
    status              VARCHAR(20)   CHECK (status IN (
                            'Confirmed','Cancelled','Checked-in','Boarded','No-show'))
                        DEFAULT 'Confirmed',
    agency_id           VARCHAR(50)   DEFAULT 'TRAVEL_CENTRAL',
    special_requests    TEXT,
    booked_at           TIMESTAMP     DEFAULT NOW(),
    last_updated        TIMESTAMP     DEFAULT NOW()
);

CREATE INDEX idx_reservations_flight   ON flight_reservations(flight_id);
CREATE INDEX idx_reservations_bookref  ON flight_reservations(booking_ref_external);
CREATE INDEX idx_reservations_updated  ON flight_reservations(last_updated);

-- ── Triggers ─────────────────────────────────────────────────────────────────
CREATE OR REPLACE FUNCTION update_last_updated()
RETURNS TRIGGER AS $$
BEGIN NEW.last_updated = NOW(); RETURN NEW; END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_flights_lu
    BEFORE UPDATE ON flights
    FOR EACH ROW EXECUTE FUNCTION update_last_updated();

CREATE TRIGGER trg_reservations_lu
    BEFORE UPDATE ON flight_reservations
    FOR EACH ROW EXECUTE FUNCTION update_last_updated();

-- ── Seed: airports ───────────────────────────────────────────────────────────
INSERT INTO airports (iata_code, name, city, country_code, timezone, latitude, longitude, is_hub) VALUES
    ('KBP', 'Boryspil International',   'Kyiv',      'UA', 'Europe/Kyiv',     50.3450, 30.8947, TRUE),
    ('LWO', 'Lviv Danylo Halytskyi',    'Lviv',      'UA', 'Europe/Kyiv',     49.8125, 23.9561, FALSE),
    ('ODS', 'Odesa International',      'Odesa',     'UA', 'Europe/Kyiv',     46.4268, 30.6765, FALSE),
    ('HRK', 'Kharkiv International',    'Kharkiv',   'UA', 'Europe/Kyiv',     49.9248, 36.2900, FALSE),
    ('WAW', 'Chopin Airport',           'Warsaw',    'PL', 'Europe/Warsaw',   52.1657, 20.9671, FALSE),
    ('BER', 'Brandenburg Airport',      'Berlin',    'DE', 'Europe/Berlin',   52.3667, 13.5033, FALSE),
    ('CDG', 'Charles de Gaulle',        'Paris',     'FR', 'Europe/Paris',    49.0097, 2.5479,  FALSE),
    ('LHR', 'Heathrow',                 'London',    'GB', 'Europe/London',   51.4700, -0.4543, FALSE),
    ('VIE', 'Vienna International',     'Vienna',    'AT', 'Europe/Vienna',   48.1103, 16.5697, FALSE),
    ('PRG', 'Václav Havel Airport',     'Prague',    'CZ', 'Europe/Prague',   50.1008, 14.2600, FALSE),
    ('FCO', 'Leonardo da Vinci',        'Rome',      'IT', 'Europe/Rome',     41.8003, 12.2389, FALSE),
    ('BCN', 'El Prat',                  'Barcelona', 'ES', 'Europe/Madrid',   41.2971, 2.0785,  FALSE),
    ('AYT', 'Antalya Airport',          'Antalya',   'TR', 'Europe/Istanbul', 36.8987, 30.7992, FALSE),
    ('HRG', 'Hurghada International',   'Hurghada',  'EG', 'Africa/Cairo',    27.1783, 33.7994, FALSE),
    ('DXB', 'Dubai International',      'Dubai',     'AE', 'Asia/Dubai',      25.2532, 55.3657, FALSE);

-- ── Seed: aircraft fleet ─────────────────────────────────────────────────────
INSERT INTO aircraft (registration, model, manufacturer, economy_seats, business_seats, range_km, manufactured_year) VALUES
    ('UR-PSA', 'Boeing 737-800',  'Boeing',  162, 12, 5765, 2018),
    ('UR-PSB', 'Boeing 737-800',  'Boeing',  162, 12, 5765, 2019),
    ('UR-AXA', 'Airbus A320neo',  'Airbus',  165, 12, 6300, 2020),
    ('UR-AXB', 'Airbus A320neo',  'Airbus',  165, 12, 6300, 2021),
    ('UR-WDC', 'Airbus A321XLR',  'Airbus',  220, 16, 8700, 2022),
    ('UR-EMB', 'Embraer E195',    'Embraer', 120,  8, 4100, 2017),
    ('UR-ATR', 'ATR 72-600',      'ATR',      70,  0, 1528, 2016);

DO $$
BEGIN
    RAISE NOTICE 'Airline database schema created: airports=%, aircraft=%',
        (SELECT count(*) FROM airports),
        (SELECT count(*) FROM aircraft);
END $$;
