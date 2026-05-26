-- Branch Office Database — PostgreSQL
-- Node: postgres-branch (localhost:5433, db=tdtp_branch)
--
-- Read-only caches (synced FROM central via tdtpcli + S3):
--   countries_cache, tours_cache, guides_cache, schedule_cache
--
-- Local write tables (synced TO central via tdtpcli + S3):
--   local_customers  →  central: branch_customers_inbox
--   local_sales      →  central: branch_sales_inbox

DROP TABLE IF EXISTS local_sales CASCADE;
DROP TABLE IF EXISTS local_customers CASCADE;
DROP TABLE IF EXISTS schedule_cache CASCADE;
DROP TABLE IF EXISTS guides_cache CASCADE;
DROP TABLE IF EXISTS tours_cache CASCADE;
DROP TABLE IF EXISTS countries_cache CASCADE;

-- ── Cache tables (coordinator writes these, branch reads) ─────────────────────

CREATE TABLE countries_cache (
    country_id       INTEGER PRIMARY KEY,   -- mirrors central.countries.country_id
    country_code     VARCHAR(3),
    country_name     VARCHAR(100),
    continent        VARCHAR(50),
    currency_code    VARCHAR(3),
    is_visa_required BOOLEAN DEFAULT FALSE,
    synced_at        TIMESTAMP DEFAULT NOW()
);

CREATE TABLE tours_cache (
    tour_id                 INTEGER PRIMARY KEY,  -- mirrors central.tours.tour_id
    tour_code               VARCHAR(20),
    tour_name               VARCHAR(150),
    description             TEXT,
    destination_country_id  INTEGER REFERENCES countries_cache(country_id),
    duration_days           SMALLINT,
    difficulty_level        VARCHAR(20),
    max_group_size          SMALLINT,
    base_price              DECIMAL(10,2),
    is_active               BOOLEAN DEFAULT TRUE,
    synced_at               TIMESTAMP DEFAULT NOW()
);

CREATE TABLE guides_cache (
    guide_id        INTEGER PRIMARY KEY,  -- mirrors central.guides.guide_id
    first_name      VARCHAR(50),
    last_name       VARCHAR(50),
    specialization  VARCHAR(100),
    rating          DECIMAL(3,2),
    languages       JSONB,
    is_active       BOOLEAN DEFAULT TRUE,
    synced_at       TIMESTAMP DEFAULT NOW()
);

CREATE TABLE schedule_cache (
    schedule_id    INTEGER PRIMARY KEY,  -- mirrors central.tour_schedule.schedule_id
    tour_id        INTEGER REFERENCES tours_cache(tour_id),
    guide_id       INTEGER REFERENCES guides_cache(guide_id),
    start_date     DATE,
    end_date       DATE,
    available_slots  SMALLINT,
    booked_slots     SMALLINT DEFAULT 0,
    price_modifier   DECIMAL(5,2) DEFAULT 1.00,
    status           VARCHAR(20) DEFAULT 'Scheduled',
    synced_at        TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_schedule_cache_dates  ON schedule_cache(start_date);
CREATE INDEX idx_schedule_cache_status ON schedule_cache(status);
CREATE INDEX idx_schedule_cache_tour   ON schedule_cache(tour_id);

-- ── Local write tables (branch writes, coordinator syncs to central) ──────────

CREATE TABLE local_customers (
    local_id               SERIAL PRIMARY KEY,
    customer_uuid          UUID        DEFAULT gen_random_uuid() NOT NULL UNIQUE,
    first_name             VARCHAR(50) NOT NULL,
    last_name              VARCHAR(50) NOT NULL,
    email                  VARCHAR(100) NOT NULL UNIQUE,
    phone                  VARCHAR(20),
    date_of_birth          DATE,
    nationality_country_id INTEGER,
    city                   VARCHAR(50),
    preferences            JSONB,
    registered_by          VARCHAR(50) DEFAULT 'branch_agent',
    created_at             TIMESTAMP   DEFAULT NOW(),
    synced_to_central_at   TIMESTAMP   NULL,  -- NULL = not yet synced
    last_updated           TIMESTAMP   DEFAULT NOW()
);

CREATE INDEX idx_lc_email    ON local_customers(email);
CREATE INDEX idx_lc_unsynced ON local_customers(synced_to_central_at)
    WHERE synced_to_central_at IS NULL;

CREATE TABLE local_sales (
    local_sale_id          SERIAL PRIMARY KEY,
    booking_reference      VARCHAR(20)   NOT NULL UNIQUE,
    schedule_id            INTEGER       NOT NULL,   -- from schedule_cache
    customer_uuid          UUID          NOT NULL,   -- from local_customers or known central UUID
    number_of_travelers    SMALLINT      NOT NULL,
    total_amount           DECIMAL(12,2) NOT NULL,
    deposit_paid           DECIMAL(12,2) DEFAULT 0,
    balance_due            DECIMAL(12,2),
    payment_status         VARCHAR(20)   DEFAULT 'Pending',
    payment_method         VARCHAR(30),
    sales_agent            VARCHAR(50),
    special_requests       TEXT,
    insurance_purchased    BOOLEAN       DEFAULT FALSE,
    cancellation_date      TIMESTAMP     NULL,
    cancellation_reason    TEXT          NULL,
    refund_amount          DECIMAL(12,2) NULL,
    synced_to_central_at   TIMESTAMP     NULL,  -- NULL = not yet synced
    created_at             TIMESTAMP     DEFAULT NOW(),
    last_updated           TIMESTAMP     DEFAULT NOW()
);

CREATE INDEX idx_ls_schedule  ON local_sales(schedule_id);
CREATE INDEX idx_ls_customer  ON local_sales(customer_uuid);
CREATE INDEX idx_ls_unsynced  ON local_sales(synced_to_central_at)
    WHERE synced_to_central_at IS NULL;
CREATE INDEX idx_ls_updated   ON local_sales(last_updated);

-- ── Triggers ─────────────────────────────────────────────────────────────────
CREATE OR REPLACE FUNCTION update_last_updated()
RETURNS TRIGGER AS $$
BEGIN NEW.last_updated = NOW(); RETURN NEW; END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_local_customers_lu
    BEFORE UPDATE ON local_customers
    FOR EACH ROW EXECUTE FUNCTION update_last_updated();

CREATE TRIGGER trg_local_sales_lu
    BEFORE UPDATE ON local_sales
    FOR EACH ROW EXECUTE FUNCTION update_last_updated();

DO $$
BEGIN
    RAISE NOTICE 'Branch database schema created successfully.';
    RAISE NOTICE '  Cache tables:       countries_cache, tours_cache, guides_cache, schedule_cache';
    RAISE NOTICE '  Local write tables: local_customers, local_sales';
END $$;
