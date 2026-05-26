-- Travel Agency Database - PostgreSQL Version
-- Complete test dataset with maximum field type coverage

-- Drop tables if exist (in correct order)
DROP TABLE IF EXISTS tour_sales CASCADE;
DROP TABLE IF EXISTS tour_schedule CASCADE;
DROP TABLE IF EXISTS tours CASCADE;
DROP TABLE IF EXISTS guides CASCADE;
DROP TABLE IF EXISTS customers CASCADE;
DROP TABLE IF EXISTS countries CASCADE;

-- 1. Countries table
CREATE TABLE countries (
    country_id SERIAL PRIMARY KEY,
    country_code VARCHAR(3) NOT NULL UNIQUE,
    country_name VARCHAR(100) NOT NULL,
    continent VARCHAR(50) NOT NULL,
    currency_code VARCHAR(3) NOT NULL,
    phone_code VARCHAR(10) NOT NULL,
    is_visa_required BOOLEAN NOT NULL DEFAULT FALSE,
    official_languages JSONB, -- JSON array
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_countries_continent ON countries(continent);
CREATE INDEX idx_countries_code ON countries(country_code);
CREATE INDEX idx_countries_languages ON countries USING GIN(official_languages);

-- 2. Customers table
CREATE TABLE customers (
    customer_id SERIAL PRIMARY KEY,
    customer_uuid UUID DEFAULT gen_random_uuid() NOT NULL UNIQUE,
    first_name VARCHAR(50) NOT NULL,
    last_name VARCHAR(50) NOT NULL,
    email VARCHAR(100) NOT NULL UNIQUE,
    phone VARCHAR(20),
    date_of_birth DATE NOT NULL,
    passport_number VARCHAR(20),
    nationality_country_id INTEGER NOT NULL REFERENCES countries(country_id),
    address VARCHAR(200),
    city VARCHAR(50),
    postal_code VARCHAR(20),
    loyalty_points INTEGER DEFAULT 0,
    is_vip BOOLEAN DEFAULT FALSE,
    preferences JSONB, -- dietary, activities, etc.
    emergency_contact JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_updated TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_customers_email ON customers(email);
CREATE INDEX idx_customers_nationality ON customers(nationality_country_id);
CREATE INDEX idx_customers_vip ON customers(is_vip) WHERE is_vip = TRUE;
CREATE INDEX idx_customers_preferences ON customers USING GIN(preferences);

-- 3. Guides table
CREATE TABLE guides (
    guide_id SERIAL PRIMARY KEY,
    first_name VARCHAR(50) NOT NULL,
    last_name VARCHAR(50) NOT NULL,
    email VARCHAR(100) NOT NULL UNIQUE,
    phone VARCHAR(20) NOT NULL,
    languages JSONB NOT NULL, -- JSON array
    specialization VARCHAR(100),
    rating DECIMAL(3,2) CHECK (rating >= 0 AND rating <= 5.0),
    experience_years SMALLINT NOT NULL,
    certifications JSONB,
    is_active BOOLEAN DEFAULT TRUE,
    hire_date DATE NOT NULL,
    hourly_rate DECIMAL(10,2),
    bio TEXT,
    photo_url VARCHAR(500)
);

CREATE INDEX idx_guides_active ON guides(is_active) WHERE is_active = TRUE;
CREATE INDEX idx_guides_rating ON guides(rating DESC);
CREATE INDEX idx_guides_languages ON guides USING GIN(languages);

-- 4. Tours table
CREATE TABLE tours (
    tour_id SERIAL PRIMARY KEY,
    tour_code VARCHAR(20) NOT NULL UNIQUE,
    tour_name VARCHAR(150) NOT NULL,
    description TEXT,
    destination_country_id INTEGER NOT NULL REFERENCES countries(country_id),
    duration_days SMALLINT NOT NULL,
    difficulty_level VARCHAR(20) CHECK (difficulty_level IN ('Easy', 'Moderate', 'Challenging', 'Extreme')),
    max_group_size SMALLINT NOT NULL,
    min_age SMALLINT,
    base_price DECIMAL(10,2) NOT NULL,
    includes JSONB, -- accommodation, meals, transport
    excludes JSONB,
    itinerary JSONB, -- array of daily activities
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_tours_destination ON tours(destination_country_id);
CREATE INDEX idx_tours_active ON tours(is_active) WHERE is_active = TRUE;
CREATE INDEX idx_tours_difficulty ON tours(difficulty_level);
CREATE INDEX idx_tours_itinerary ON tours USING GIN(itinerary);

-- 5. Tour Schedule table
CREATE TABLE tour_schedule (
    schedule_id SERIAL PRIMARY KEY,
    tour_id INTEGER NOT NULL REFERENCES tours(tour_id),
    guide_id INTEGER NOT NULL REFERENCES guides(guide_id),
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    departure_time TIME,
    available_slots SMALLINT NOT NULL,
    booked_slots SMALLINT DEFAULT 0,
    price_modifier DECIMAL(5,2) DEFAULT 1.00,
    status VARCHAR(20) CHECK (status IN ('Scheduled', 'Confirmed', 'In Progress', 'Completed', 'Cancelled')) DEFAULT 'Scheduled',
    notes TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_schedule_dates ON tour_schedule(start_date, end_date);
CREATE INDEX idx_schedule_tour ON tour_schedule(tour_id);
CREATE INDEX idx_schedule_guide ON tour_schedule(guide_id);
CREATE INDEX idx_schedule_status ON tour_schedule(status);

-- 6. Tour Sales table
CREATE TABLE tour_sales (
    sale_id SERIAL PRIMARY KEY,
    booking_reference VARCHAR(20) NOT NULL UNIQUE,
    schedule_id INTEGER NOT NULL REFERENCES tour_schedule(schedule_id),
    customer_id INTEGER NOT NULL REFERENCES customers(customer_id),
    booking_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    number_of_travelers SMALLINT NOT NULL,
    total_amount DECIMAL(12,2) NOT NULL,
    deposit_paid DECIMAL(12,2) DEFAULT 0,
    balance_due DECIMAL(12,2),
    payment_status VARCHAR(20) CHECK (payment_status IN ('Pending', 'Deposit Paid', 'Fully Paid', 'Refunded', 'Cancelled')) DEFAULT 'Pending',
    payment_method VARCHAR(30),
    special_requests TEXT,
    traveler_details JSONB, -- array of traveler info
    insurance_purchased BOOLEAN DEFAULT FALSE,
    cancellation_date TIMESTAMP NULL,
    cancellation_reason TEXT NULL,
    refund_amount DECIMAL(12,2) NULL,
    sales_agent VARCHAR(50),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_updated TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_sales_customer ON tour_sales(customer_id);
CREATE INDEX idx_sales_schedule ON tour_sales(schedule_id);
CREATE INDEX idx_sales_booking_date ON tour_sales(booking_date DESC);
CREATE INDEX idx_sales_status ON tour_sales(payment_status);
CREATE INDEX idx_sales_reference ON tour_sales(booking_reference);
CREATE INDEX idx_sales_travelers ON tour_sales USING GIN(traveler_details);

-- Create update trigger for last_updated
CREATE OR REPLACE FUNCTION update_last_updated_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.last_updated = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_customers_last_updated BEFORE UPDATE ON customers
    FOR EACH ROW EXECUTE FUNCTION update_last_updated_column();

CREATE TRIGGER update_tour_sales_last_updated BEFORE UPDATE ON tour_sales
    FOR EACH ROW EXECUTE FUNCTION update_last_updated_column();

-- Success message
DO $$
BEGIN
    RAISE NOTICE 'TravelAgency database schema created successfully!';
END
$$;
