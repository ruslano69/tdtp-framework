-- Travel Guide Database Setup
-- MS SQL Server database with cities and attractions

USE master;
GO

-- Drop database if exists
IF EXISTS (SELECT name FROM sys.databases WHERE name = 'TravelGuide')
BEGIN
    ALTER DATABASE TravelGuide SET SINGLE_USER WITH ROLLBACK IMMEDIATE;
    DROP DATABASE TravelGuide;
END
GO

-- Create database
CREATE DATABASE TravelGuide;
GO

USE TravelGuide;
GO

-- Create cities table with tourist information
CREATE TABLE cities (
    city_id INT PRIMARY KEY IDENTITY(1,1),
    name NVARCHAR(100) NOT NULL,
    country NVARCHAR(100) NOT NULL,
    latitude DECIMAL(9,6) NOT NULL,
    longitude DECIMAL(9,6) NOT NULL,
    population INT NOT NULL,
    timezone VARCHAR(50) NOT NULL,
    attractions NVARCHAR(MAX), -- JSON field with attractions and prices
    created_at DATETIME2 DEFAULT GETDATE(),

    -- Indexes for performance
    INDEX idx_country (country),
    INDEX idx_population (population),
    INDEX idx_name (name)
);
GO

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
    JSON_VALUE(attractions, '$.length') as attraction_count,
    created_at
FROM cities;
GO

PRINT 'Database and table created successfully!';
PRINT 'Table: cities';
PRINT 'Columns: city_id, name, country, latitude, longitude, population, timezone, attractions (JSON)';
GO
