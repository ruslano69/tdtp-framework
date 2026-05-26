-- Travel Agency Database - MSSQL Version
-- Complete test dataset with maximum field type coverage

USE master;
GO

-- Create database if not exists
IF NOT EXISTS (SELECT name FROM sys.databases WHERE name = 'TravelAgency')
BEGIN
    CREATE DATABASE TravelAgency;
END
GO

USE TravelAgency;
GO

-- Drop tables if exist (in correct order)
IF OBJECT_ID('tour_sales', 'U') IS NOT NULL DROP TABLE tour_sales;
IF OBJECT_ID('tour_schedule', 'U') IS NOT NULL DROP TABLE tour_schedule;
IF OBJECT_ID('tours', 'U') IS NOT NULL DROP TABLE tours;
IF OBJECT_ID('guides', 'U') IS NOT NULL DROP TABLE guides;
IF OBJECT_ID('customers', 'U') IS NOT NULL DROP TABLE customers;
IF OBJECT_ID('countries', 'U') IS NOT NULL DROP TABLE countries;
GO

-- 1. Countries table
CREATE TABLE countries (
    country_id INT PRIMARY KEY IDENTITY(1,1),
    country_code VARCHAR(3) NOT NULL UNIQUE,
    country_name NVARCHAR(100) NOT NULL,
    continent VARCHAR(50) NOT NULL,
    currency_code VARCHAR(3) NOT NULL,
    phone_code VARCHAR(10) NOT NULL,
    is_visa_required BIT NOT NULL DEFAULT 0,
    official_languages NVARCHAR(MAX), -- JSON array
    created_at DATETIME2 DEFAULT GETDATE()
);
GO

CREATE INDEX idx_countries_continent ON countries(continent);
CREATE INDEX idx_countries_code ON countries(country_code);
GO

-- 2. Customers table
CREATE TABLE customers (
    customer_id INT PRIMARY KEY IDENTITY(1,1),
    customer_uuid UNIQUEIDENTIFIER DEFAULT NEWID() NOT NULL UNIQUE,
    first_name NVARCHAR(50) NOT NULL,
    last_name NVARCHAR(50) NOT NULL,
    email VARCHAR(100) NOT NULL UNIQUE,
    phone VARCHAR(20),
    date_of_birth DATE NOT NULL,
    passport_number VARCHAR(20),
    nationality_country_id INT NOT NULL FOREIGN KEY REFERENCES countries(country_id),
    address NVARCHAR(200),
    city NVARCHAR(50),
    postal_code VARCHAR(20),
    loyalty_points INT DEFAULT 0,
    is_vip BIT DEFAULT 0,
    preferences NVARCHAR(MAX), -- JSON: dietary, activities, etc.
    emergency_contact NVARCHAR(MAX), -- JSON
    created_at DATETIME2 DEFAULT GETDATE(),
    last_updated DATETIME2 DEFAULT GETDATE()
);
GO

CREATE INDEX idx_customers_email ON customers(email);
CREATE INDEX idx_customers_nationality ON customers(nationality_country_id);
CREATE INDEX idx_customers_vip ON customers(is_vip) WHERE is_vip = 1;
GO

-- 3. Guides table
CREATE TABLE guides (
    guide_id INT PRIMARY KEY IDENTITY(1,1),
    first_name NVARCHAR(50) NOT NULL,
    last_name NVARCHAR(50) NOT NULL,
    email VARCHAR(100) NOT NULL UNIQUE,
    phone VARCHAR(20) NOT NULL,
    languages NVARCHAR(MAX) NOT NULL, -- JSON array
    specialization NVARCHAR(100), -- adventure, cultural, eco-tourism, etc.
    rating DECIMAL(3,2) CHECK (rating >= 0 AND rating <= 5.0),
    experience_years SMALLINT NOT NULL,
    certifications NVARCHAR(MAX), -- JSON array
    is_active BIT DEFAULT 1,
    hire_date DATE NOT NULL,
    hourly_rate DECIMAL(10,2),
    bio TEXT,
    photo_url VARCHAR(500)
);
GO

CREATE INDEX idx_guides_active ON guides(is_active) WHERE is_active = 1;
CREATE INDEX idx_guides_rating ON guides(rating DESC);
GO

-- 4. Tours table
CREATE TABLE tours (
    tour_id INT PRIMARY KEY IDENTITY(1,1),
    tour_code VARCHAR(20) NOT NULL UNIQUE,
    tour_name NVARCHAR(150) NOT NULL,
    description TEXT,
    destination_country_id INT NOT NULL FOREIGN KEY REFERENCES countries(country_id),
    duration_days SMALLINT NOT NULL,
    difficulty_level VARCHAR(20) CHECK (difficulty_level IN ('Easy', 'Moderate', 'Challenging', 'Extreme')),
    max_group_size SMALLINT NOT NULL,
    min_age SMALLINT,
    base_price DECIMAL(10,2) NOT NULL,
    includes NVARCHAR(MAX), -- JSON: accommodation, meals, transport, etc.
    excludes NVARCHAR(MAX), -- JSON
    itinerary NVARCHAR(MAX), -- JSON array of daily activities
    is_active BIT DEFAULT 1,
    created_at DATETIME2 DEFAULT GETDATE()
);
GO

CREATE INDEX idx_tours_destination ON tours(destination_country_id);
CREATE INDEX idx_tours_active ON tours(is_active) WHERE is_active = 1;
CREATE INDEX idx_tours_difficulty ON tours(difficulty_level);
GO

-- 5. Tour Schedule table
CREATE TABLE tour_schedule (
    schedule_id INT PRIMARY KEY IDENTITY(1,1),
    tour_id INT NOT NULL FOREIGN KEY REFERENCES tours(tour_id),
    guide_id INT NOT NULL FOREIGN KEY REFERENCES guides(guide_id),
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    departure_time TIME,
    available_slots SMALLINT NOT NULL,
    booked_slots SMALLINT DEFAULT 0,
    price_modifier DECIMAL(5,2) DEFAULT 1.00, -- Seasonal multiplier (0.80 = 20% off, 1.20 = 20% markup)
    status VARCHAR(20) CHECK (status IN ('Scheduled', 'Confirmed', 'In Progress', 'Completed', 'Cancelled')) DEFAULT 'Scheduled',
    notes TEXT,
    created_at DATETIME2 DEFAULT GETDATE()
);
GO

CREATE INDEX idx_schedule_dates ON tour_schedule(start_date, end_date);
CREATE INDEX idx_schedule_tour ON tour_schedule(tour_id);
CREATE INDEX idx_schedule_guide ON tour_schedule(guide_id);
CREATE INDEX idx_schedule_status ON tour_schedule(status);
GO

-- 6. Tour Sales table
CREATE TABLE tour_sales (
    sale_id INT PRIMARY KEY IDENTITY(1,1),
    booking_reference VARCHAR(20) NOT NULL UNIQUE,
    schedule_id INT NOT NULL FOREIGN KEY REFERENCES tour_schedule(schedule_id),
    customer_id INT NOT NULL FOREIGN KEY REFERENCES customers(customer_id),
    booking_date DATETIME2 DEFAULT GETDATE(),
    number_of_travelers SMALLINT NOT NULL,
    total_amount DECIMAL(12,2) NOT NULL,
    deposit_paid DECIMAL(12,2) DEFAULT 0,
    balance_due DECIMAL(12,2),
    payment_status VARCHAR(20) CHECK (payment_status IN ('Pending', 'Deposit Paid', 'Fully Paid', 'Refunded', 'Cancelled')) DEFAULT 'Pending',
    payment_method VARCHAR(30),
    special_requests TEXT,
    traveler_details NVARCHAR(MAX), -- JSON array of traveler info
    insurance_purchased BIT DEFAULT 0,
    cancellation_date DATETIME2 NULL,
    cancellation_reason TEXT NULL,
    refund_amount DECIMAL(12,2) NULL,
    sales_agent NVARCHAR(50),
    created_at DATETIME2 DEFAULT GETDATE(),
    last_updated DATETIME2 DEFAULT GETDATE()
);
GO

CREATE INDEX idx_sales_customer ON tour_sales(customer_id);
CREATE INDEX idx_sales_schedule ON tour_sales(schedule_id);
CREATE INDEX idx_sales_booking_date ON tour_sales(booking_date DESC);
CREATE INDEX idx_sales_status ON tour_sales(payment_status);
CREATE INDEX idx_sales_reference ON tour_sales(booking_reference);
GO

PRINT 'TravelAgency database schema created successfully!';
GO
