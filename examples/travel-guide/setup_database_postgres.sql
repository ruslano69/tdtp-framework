-- Travel Guide Database Setup for PostgreSQL
-- Creates cities table with tourist information

-- Drop table if exists
DROP TABLE IF EXISTS cities CASCADE;

-- Create cities table with tourist information
CREATE TABLE cities (
    city_id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    country VARCHAR(100) NOT NULL,
    latitude DECIMAL(9,6) NOT NULL,
    longitude DECIMAL(9,6) NOT NULL,
    population INTEGER NOT NULL,
    timezone VARCHAR(50) NOT NULL,
    attractions JSONB,  -- PostgreSQL native JSON type
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

    -- Indexes for performance
    CONSTRAINT cities_name_key UNIQUE (name, country)
);

-- Create indexes
CREATE INDEX idx_cities_country ON cities(country);
CREATE INDEX idx_cities_population ON cities(population);
CREATE INDEX idx_cities_name ON cities(name);

-- Create GIN index for JSON queries (PostgreSQL specific)
CREATE INDEX idx_cities_attractions ON cities USING GIN (attractions);

-- Create view for cities with attraction count
CREATE VIEW v_cities_summary AS
SELECT
    city_id,
    name,
    country,
    latitude,
    longitude,
    population,
    timezone,
    jsonb_array_length(attractions) as attraction_count,
    created_at
FROM cities;

-- Output confirmation
\echo 'Database and table created successfully!'
\echo 'Table: cities'
\echo 'Columns: city_id, name, country, latitude, longitude, population, timezone, attractions (JSONB)'
