-- Branch Office — staging tables + merge procedures
-- Staging tables receive raw data from consumer.py (tdtpcli --import-broker),
-- merge procedures do ON CONFLICT upsert into working cache tables.
--
-- Import order (FK deps in cache tables):
--   1. countries → 2. guides + tours → 3. schedule

-- ── Staging tables ─────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS countries_cache_staging (
    country_id       INTEGER      PRIMARY KEY,
    country_code     VARCHAR(3),
    country_name     VARCHAR(100),
    continent        VARCHAR(50),
    currency_code    VARCHAR(3),
    is_visa_required BOOLEAN
);

CREATE TABLE IF NOT EXISTS guides_cache_staging (
    guide_id        INTEGER      PRIMARY KEY,
    first_name      VARCHAR(50),
    last_name       VARCHAR(50),
    specialization  VARCHAR(100),
    rating          FLOAT,
    languages       JSONB,
    is_active       BOOLEAN
);

CREATE TABLE IF NOT EXISTS tours_cache_staging (
    tour_id                INTEGER      PRIMARY KEY,
    tour_code              VARCHAR(20),
    tour_name              VARCHAR(150),
    description            TEXT,
    destination_country_id INTEGER,
    duration_days          SMALLINT,
    difficulty_level       VARCHAR(20),
    max_group_size         SMALLINT,
    base_price             FLOAT,
    is_active              BOOLEAN
);

CREATE TABLE IF NOT EXISTS schedule_cache_staging (
    schedule_id     INTEGER      PRIMARY KEY,
    tour_id         INTEGER,
    guide_id        INTEGER,
    start_date      DATE,
    end_date        DATE,
    available_slots SMALLINT,
    booked_slots    SMALLINT,
    price_modifier  FLOAT,
    status          VARCHAR(20),
    last_updated    TIMESTAMP
);

-- ── Merge procedures ──────────────────────────────────────────────────────────

CREATE OR REPLACE PROCEDURE merge_countries_cache()
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO countries_cache
        (country_id, country_code, country_name, continent, currency_code, is_visa_required, synced_at)
    SELECT
        country_id, country_code, country_name, continent, currency_code,
        is_visa_required,
        NOW()
    FROM countries_cache_staging
    ON CONFLICT (country_id) DO UPDATE SET
        country_code     = EXCLUDED.country_code,
        country_name     = EXCLUDED.country_name,
        continent        = EXCLUDED.continent,
        currency_code    = EXCLUDED.currency_code,
        is_visa_required = EXCLUDED.is_visa_required,
        synced_at        = NOW();

    TRUNCATE countries_cache_staging;
    RAISE NOTICE 'merge_countries_cache: done';
END;
$$;

CREATE OR REPLACE PROCEDURE merge_guides_cache()
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO guides_cache
        (guide_id, first_name, last_name, specialization, rating, languages, is_active, synced_at)
    SELECT
        guide_id, first_name, last_name, specialization,
        rating::NUMERIC(3,2),
        languages,
        is_active,
        NOW()
    FROM guides_cache_staging
    ON CONFLICT (guide_id) DO UPDATE SET
        first_name     = EXCLUDED.first_name,
        last_name      = EXCLUDED.last_name,
        specialization = EXCLUDED.specialization,
        rating         = EXCLUDED.rating,
        languages      = EXCLUDED.languages,
        is_active      = EXCLUDED.is_active,
        synced_at      = NOW();

    TRUNCATE guides_cache_staging;
    RAISE NOTICE 'merge_guides_cache: done';
END;
$$;

CREATE OR REPLACE PROCEDURE merge_tours_cache()
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO tours_cache
        (tour_id, tour_code, tour_name, description, destination_country_id,
         duration_days, difficulty_level, max_group_size, base_price, is_active, synced_at)
    SELECT
        tour_id, tour_code, tour_name, description, destination_country_id,
        duration_days, difficulty_level, max_group_size,
        base_price::NUMERIC(10,2),
        is_active,
        NOW()
    FROM tours_cache_staging
    -- skip tours whose country isn't synced yet (FK constraint in cache table)
    WHERE destination_country_id IS NULL
       OR destination_country_id IN (SELECT country_id FROM countries_cache)
    ON CONFLICT (tour_id) DO UPDATE SET
        tour_code              = EXCLUDED.tour_code,
        tour_name              = EXCLUDED.tour_name,
        description            = EXCLUDED.description,
        destination_country_id = EXCLUDED.destination_country_id,
        duration_days          = EXCLUDED.duration_days,
        difficulty_level       = EXCLUDED.difficulty_level,
        max_group_size         = EXCLUDED.max_group_size,
        base_price             = EXCLUDED.base_price,
        is_active              = EXCLUDED.is_active,
        synced_at              = NOW();

    TRUNCATE tours_cache_staging;
    RAISE NOTICE 'merge_tours_cache: done';
END;
$$;

CREATE OR REPLACE PROCEDURE merge_schedule_cache()
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO schedule_cache
        (schedule_id, tour_id, guide_id, start_date, end_date,
         available_slots, booked_slots, price_modifier, status, synced_at)
    SELECT
        schedule_id, tour_id, guide_id,
        start_date,
        end_date,
        available_slots, booked_slots,
        price_modifier::NUMERIC(5,2),
        status,
        NOW()
    FROM schedule_cache_staging
    -- skip if tour or guide not yet in cache (FK constraints)
    WHERE tour_id  IN (SELECT tour_id  FROM tours_cache)
      AND guide_id IN (SELECT guide_id FROM guides_cache)
    ON CONFLICT (schedule_id) DO UPDATE SET
        tour_id         = EXCLUDED.tour_id,
        guide_id        = EXCLUDED.guide_id,
        start_date      = EXCLUDED.start_date,
        end_date        = EXCLUDED.end_date,
        available_slots = EXCLUDED.available_slots,
        booked_slots    = EXCLUDED.booked_slots,
        price_modifier  = EXCLUDED.price_modifier,
        status          = EXCLUDED.status,
        synced_at       = NOW();

    TRUNCATE schedule_cache_staging;
    RAISE NOTICE 'merge_schedule_cache: done';
END;
$$;

DO $$ BEGIN RAISE NOTICE 'Branch staging tables and merge procedures ready'; END $$;
