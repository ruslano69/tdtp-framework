-- Central Office — additional tables for multi-node architecture
-- Run AFTER setup_database_postgres.sql
--
-- New tables:
--   flights               — synced FROM airline node
--   flight_reservations   — synced FROM airline node
--   branch_customers_inbox — received FROM branch node (pending reconciliation)
--   branch_sales_inbox    — received FROM branch node (pending reconciliation)

-- ── Flights (mirror of airline.flights, enriched with airport names) ──────────
CREATE TABLE IF NOT EXISTS flights (
    flight_id          INTEGER       PRIMARY KEY,  -- same ID as airline DB
    flight_number      VARCHAR(10)   NOT NULL,
    origin_iata        CHAR(3),
    origin_city        VARCHAR(100),
    destination_iata   CHAR(3),
    destination_city   VARCHAR(100),
    departure_time     TIMESTAMP,
    arrival_time       TIMESTAMP,
    duration_minutes   SMALLINT,
    economy_price      DECIMAL(10,2),
    business_price     DECIMAL(10,2),
    available_economy  SMALLINT,
    available_business SMALLINT DEFAULT 0,
    status             VARCHAR(20)   DEFAULT 'Scheduled',
    delay_minutes      SMALLINT      DEFAULT 0,
    synced_at          TIMESTAMP     DEFAULT NOW(),
    last_updated       TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_flights_central_dep    ON flights(departure_time);
CREATE INDEX IF NOT EXISTS idx_flights_central_status ON flights(status);
CREATE INDEX IF NOT EXISTS idx_flights_central_route  ON flights(origin_iata, destination_iata);

-- ── Flight reservations (mirror of airline.flight_reservations) ───────────────
CREATE TABLE IF NOT EXISTS flight_reservations (
    reservation_id       INTEGER       PRIMARY KEY,  -- same ID as airline DB
    flight_id            INTEGER       REFERENCES flights(flight_id),
    booking_ref_external VARCHAR(30),
    passenger_name       VARCHAR(100),
    seat_class           VARCHAR(20),
    price_paid           DECIMAL(10,2),
    status               VARCHAR(20),
    agency_id            VARCHAR(50),
    synced_at            TIMESTAMP     DEFAULT NOW(),
    last_updated         TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_flres_flight   ON flight_reservations(flight_id);
CREATE INDEX IF NOT EXISTS idx_flres_bookref  ON flight_reservations(booking_ref_external);

-- ── Branch customers inbox ────────────────────────────────────────────────────
-- Branch registers new customers locally; coordinator syncs them here.
-- Reconciliation: match by email to existing customers or INSERT new.
CREATE TABLE IF NOT EXISTS branch_customers_inbox (
    id                     SERIAL PRIMARY KEY,
    source_local_id        INTEGER       NOT NULL,
    customer_uuid          UUID          NOT NULL UNIQUE,
    first_name             VARCHAR(50),
    last_name              VARCHAR(50),
    email                  VARCHAR(100)  NOT NULL UNIQUE,
    phone                  VARCHAR(20),
    date_of_birth          DATE,
    nationality_country_id INTEGER,
    city                   VARCHAR(50),
    preferences            JSONB,
    registered_by          VARCHAR(50),
    created_at             TIMESTAMP,
    last_updated           TIMESTAMP,
    received_at            TIMESTAMP     DEFAULT NOW(),
    reconciled_at          TIMESTAMP     NULL,        -- set after merge into customers table
    reconciled_customer_id INTEGER       NULL REFERENCES customers(customer_id)
);

CREATE INDEX IF NOT EXISTS idx_bci_email      ON branch_customers_inbox(email);
CREATE INDEX IF NOT EXISTS idx_bci_uuid       ON branch_customers_inbox(customer_uuid);
CREATE INDEX IF NOT EXISTS idx_bci_unreconcil ON branch_customers_inbox(reconciled_at)
    WHERE reconciled_at IS NULL;

-- ── Branch sales inbox ────────────────────────────────────────────────────────
-- Branch creates bookings locally; coordinator syncs them here.
-- Reconciliation: link customer_uuid → customer_id, then insert into tour_sales.
CREATE TABLE IF NOT EXISTS branch_sales_inbox (
    id                     SERIAL PRIMARY KEY,
    source_local_id        INTEGER       NOT NULL,
    booking_reference      VARCHAR(20)   NOT NULL UNIQUE,
    schedule_id            INTEGER,
    customer_uuid          UUID,
    number_of_travelers    SMALLINT,
    total_amount           DECIMAL(12,2),
    deposit_paid           DECIMAL(12,2),
    balance_due            DECIMAL(12,2),
    payment_status         VARCHAR(20),
    payment_method         VARCHAR(30),
    sales_agent            VARCHAR(50),
    insurance_purchased    BOOLEAN       DEFAULT FALSE,
    cancellation_date      TIMESTAMP     NULL,
    cancellation_reason    TEXT          NULL,
    refund_amount          DECIMAL(12,2) NULL,
    created_at             TIMESTAMP,
    last_updated           TIMESTAMP,
    received_at            TIMESTAMP     DEFAULT NOW(),
    reconciled_at          TIMESTAMP     NULL,        -- set after merge into tour_sales
    reconciled_sale_id     INTEGER       NULL REFERENCES tour_sales(sale_id)
);

CREATE INDEX IF NOT EXISTS idx_bsi_booking    ON branch_sales_inbox(booking_reference);
CREATE INDEX IF NOT EXISTS idx_bsi_customer   ON branch_sales_inbox(customer_uuid);
CREATE INDEX IF NOT EXISTS idx_bsi_unreconcil ON branch_sales_inbox(reconciled_at)
    WHERE reconciled_at IS NULL;

-- ── Add last_updated to tour_schedule if missing ──────────────────────────────
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'tour_schedule' AND column_name = 'last_updated'
    ) THEN
        ALTER TABLE tour_schedule ADD COLUMN last_updated TIMESTAMP DEFAULT NOW();
        RAISE NOTICE 'Added last_updated to tour_schedule';
    END IF;
END $$;

-- ── Add last_updated to guides if missing ─────────────────────────────────────
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'guides' AND column_name = 'last_updated'
    ) THEN
        ALTER TABLE guides ADD COLUMN last_updated TIMESTAMP DEFAULT NOW();
        RAISE NOTICE 'Added last_updated to guides';
    END IF;
END $$;

DO $$
BEGIN
    RAISE NOTICE 'Central additions applied: flights, flight_reservations, branch_customers_inbox, branch_sales_inbox';
END $$;
