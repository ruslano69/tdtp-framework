-- Central Office — staging tables + merge procedures
-- Staging tables receive raw data from consumer.py (tdtpcli --import-broker),
-- merge procedures do ON CONFLICT upsert into working tables.
--
-- Streams:
--   airline → flights_staging            → flights
--   airline → flight_reservations_staging → flight_reservations (FK: flights)
--   branch  → branch_customers_inbox_staging → branch_customers_inbox
--   branch  → branch_sales_inbox_staging     → branch_sales_inbox

-- ── Staging tables ─────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS flights_staging (
    flight_id          INTEGER      PRIMARY KEY,
    flight_number      VARCHAR(10),
    origin_iata        VARCHAR(3),
    origin_city        VARCHAR(100),
    destination_iata   VARCHAR(3),
    destination_city   VARCHAR(100),
    departure_time     TIMESTAMP,
    arrival_time       TIMESTAMP,
    duration_minutes   INTEGER,
    economy_price      FLOAT,
    business_price     FLOAT,
    available_economy  INTEGER,
    available_business INTEGER,
    status             VARCHAR(20),
    delay_minutes      INTEGER,
    last_updated       TIMESTAMP
);

CREATE TABLE IF NOT EXISTS flight_reservations_staging (
    reservation_id       INTEGER      PRIMARY KEY,
    flight_id            INTEGER,
    booking_ref_external VARCHAR(30),
    passenger_name       VARCHAR(100),
    seat_class           VARCHAR(20),
    price_paid           FLOAT,
    status               VARCHAR(20),
    agency_id            VARCHAR(50),
    last_updated         TIMESTAMP
);

CREATE TABLE IF NOT EXISTS branch_customers_inbox_staging (
    source_local_id INTEGER,
    customer_uuid   TEXT         PRIMARY KEY,  -- UUID as text, cast to UUID in merge
    first_name      VARCHAR(50),
    last_name       VARCHAR(50),
    email           VARCHAR(100),
    phone           VARCHAR(20),
    date_of_birth   DATE,
    city            VARCHAR(50),
    preferences     JSONB,
    registered_by   VARCHAR(50),
    created_at      TIMESTAMP,
    last_updated    TIMESTAMP
);

CREATE TABLE IF NOT EXISTS branch_sales_inbox_staging (
    source_local_id      INTEGER,
    booking_reference    VARCHAR(20)  PRIMARY KEY,
    schedule_id          INTEGER,
    customer_uuid        TEXT,              -- UUID as text, cast to UUID in merge via NULLIF
    number_of_travelers  SMALLINT,
    total_amount         NUMERIC(12,2),
    deposit_paid         NUMERIC(12,2),
    balance_due          NUMERIC(12,2),     -- COALESCE(x,0) in v_local_sales view
    payment_status       VARCHAR(20),
    payment_method       VARCHAR(30),
    sales_agent          VARCHAR(50),
    special_requests     TEXT,
    insurance_purchased  BOOLEAN,           -- COALESCE(x,false) in v_local_sales view
    cancellation_date    TIMESTAMP NULL,
    cancellation_reason  TEXT,
    refund_amount        NUMERIC(12,2),     -- COALESCE(x,0) in v_local_sales view
    created_at           TIMESTAMP,
    last_updated         TIMESTAMP
);

-- ── Merge procedures ──────────────────────────────────────────────────────────

CREATE OR REPLACE PROCEDURE merge_flights()
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO flights
        (flight_id, flight_number, origin_iata, origin_city, destination_iata,
         destination_city, departure_time, arrival_time, duration_minutes,
         economy_price, business_price, available_economy, available_business,
         status, delay_minutes, last_updated, synced_at)
    SELECT
        flight_id, flight_number, origin_iata, origin_city, destination_iata,
        destination_city,
        departure_time,
        arrival_time,
        duration_minutes,
        economy_price::DECIMAL(10,2),
        business_price::DECIMAL(10,2),
        available_economy,
        available_business,
        status,
        delay_minutes,
        last_updated,
        NOW()
    FROM flights_staging
    ON CONFLICT (flight_id) DO UPDATE SET
        flight_number      = EXCLUDED.flight_number,
        origin_iata        = EXCLUDED.origin_iata,
        origin_city        = EXCLUDED.origin_city,
        destination_iata   = EXCLUDED.destination_iata,
        destination_city   = EXCLUDED.destination_city,
        departure_time     = EXCLUDED.departure_time,
        arrival_time       = EXCLUDED.arrival_time,
        duration_minutes   = EXCLUDED.duration_minutes,
        economy_price      = EXCLUDED.economy_price,
        business_price     = EXCLUDED.business_price,
        available_economy  = EXCLUDED.available_economy,
        available_business = EXCLUDED.available_business,
        status             = EXCLUDED.status,
        delay_minutes      = EXCLUDED.delay_minutes,
        last_updated       = EXCLUDED.last_updated,
        synced_at          = NOW();

    TRUNCATE flights_staging;
    RAISE NOTICE 'merge_flights: done';
END;
$$;

CREATE OR REPLACE PROCEDURE merge_flight_reservations()
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO flight_reservations
        (reservation_id, flight_id, booking_ref_external, passenger_name,
         seat_class, price_paid, status, agency_id, last_updated, synced_at)
    SELECT
        reservation_id, flight_id, booking_ref_external, passenger_name,
        seat_class,
        price_paid::DECIMAL(10,2),
        status, agency_id,
        last_updated,
        NOW()
    FROM flight_reservations_staging
    -- skip reservations whose flight hasn't been synced yet
    WHERE flight_id IN (SELECT flight_id FROM flights)
    ON CONFLICT (reservation_id) DO UPDATE SET
        flight_id            = EXCLUDED.flight_id,
        booking_ref_external = EXCLUDED.booking_ref_external,
        passenger_name       = EXCLUDED.passenger_name,
        seat_class           = EXCLUDED.seat_class,
        price_paid           = EXCLUDED.price_paid,
        status               = EXCLUDED.status,
        agency_id            = EXCLUDED.agency_id,
        last_updated         = EXCLUDED.last_updated,
        synced_at            = NOW();

    TRUNCATE flight_reservations_staging;
    RAISE NOTICE 'merge_flight_reservations: done';
END;
$$;

CREATE OR REPLACE PROCEDURE merge_branch_customers_inbox()
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO branch_customers_inbox
        (source_local_id, customer_uuid, first_name, last_name, email,
         phone, date_of_birth, city, preferences, registered_by,
         created_at, last_updated, received_at)
    SELECT
        source_local_id,
        customer_uuid::UUID,
        first_name, last_name, email, phone,
        date_of_birth,
        city,
        preferences,
        registered_by,
        created_at,
        last_updated,
        NOW()
    FROM branch_customers_inbox_staging
    ON CONFLICT (customer_uuid) DO UPDATE SET
        first_name    = EXCLUDED.first_name,
        last_name     = EXCLUDED.last_name,
        email         = EXCLUDED.email,
        phone         = EXCLUDED.phone,
        date_of_birth = EXCLUDED.date_of_birth,
        city          = EXCLUDED.city,
        preferences   = EXCLUDED.preferences,
        last_updated  = EXCLUDED.last_updated;

    TRUNCATE branch_customers_inbox_staging;
    RAISE NOTICE 'merge_branch_customers_inbox: done';
END;
$$;

CREATE OR REPLACE PROCEDURE merge_branch_sales_inbox()
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO branch_sales_inbox
        (source_local_id, booking_reference, schedule_id, customer_uuid,
         number_of_travelers, total_amount, deposit_paid, balance_due,
         payment_status, payment_method, sales_agent,
         insurance_purchased, cancellation_date, cancellation_reason,
         refund_amount, created_at, last_updated, received_at)
    SELECT
        source_local_id, booking_reference, schedule_id,
        NULLIF(customer_uuid, '')::UUID,
        number_of_travelers,
        total_amount::DECIMAL(12,2),
        deposit_paid::DECIMAL(12,2),
        balance_due::DECIMAL(12,2),
        payment_status, payment_method, sales_agent,
        insurance_purchased,
        cancellation_date,
        NULLIF(cancellation_reason, ''),
        refund_amount::DECIMAL(12,2),
        created_at,
        last_updated,
        NOW()
    FROM branch_sales_inbox_staging
    ON CONFLICT (booking_reference) DO UPDATE SET
        schedule_id         = EXCLUDED.schedule_id,
        number_of_travelers = EXCLUDED.number_of_travelers,
        total_amount        = EXCLUDED.total_amount,
        deposit_paid        = EXCLUDED.deposit_paid,
        balance_due         = EXCLUDED.balance_due,
        payment_status      = EXCLUDED.payment_status,
        payment_method      = EXCLUDED.payment_method,
        insurance_purchased = EXCLUDED.insurance_purchased,
        cancellation_date   = EXCLUDED.cancellation_date,
        cancellation_reason = EXCLUDED.cancellation_reason,
        refund_amount       = EXCLUDED.refund_amount,
        last_updated        = EXCLUDED.last_updated;

    TRUNCATE branch_sales_inbox_staging;
    RAISE NOTICE 'merge_branch_sales_inbox: done';
END;
$$;

DO $$ BEGIN RAISE NOTICE 'Central staging tables and merge procedures ready'; END $$;
