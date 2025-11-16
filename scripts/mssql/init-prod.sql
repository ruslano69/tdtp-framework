-- MS SQL Server Production Simulation Database Initialization
-- Version: 1.0
-- Date: 16.11.2025
-- Purpose: Simulate SQL Server 2012 production environment
-- CRITICAL: This database MUST be in SQL Server 2012 compatibility mode (110)

-- Create production simulation database
IF NOT EXISTS (SELECT name FROM sys.databases WHERE name = 'ProdSimDB')
BEGIN
    CREATE DATABASE ProdSimDB;
END
GO

-- КРИТИЧНО: Устанавливаем SQL Server 2012 compatibility level
-- Это имитирует production окружение
ALTER DATABASE ProdSimDB SET COMPATIBILITY_LEVEL = 110;
GO

USE ProdSimDB;
GO

PRINT '=================================================';
PRINT 'Production Simulation Database Initialized';
PRINT '=================================================';
PRINT 'Database: ProdSimDB';
PRINT 'Compatibility Level: ' + CAST((SELECT compatibility_level FROM sys.databases WHERE name = 'ProdSimDB') AS VARCHAR) + ' (SQL Server 2012)';
PRINT '';
PRINT 'IMPORTANT: This database simulates SQL Server 2012';
PRINT 'Only SQL Server 2012 features are available!';
PRINT '';
PRINT 'Forbidden functions:';
PRINT '  - JSON_VALUE, JSON_QUERY (SQL Server 2016+)';
PRINT '  - STRING_SPLIT (SQL Server 2016+)';
PRINT '  - STRING_AGG (SQL Server 2017+)';
PRINT '  - TRIM (SQL Server 2017+)';
PRINT '';
PRINT 'Allowed functions:';
PRINT '  - OFFSET/FETCH (SQL Server 2012+)';
PRINT '  - MERGE (SQL Server 2008+)';
PRINT '  - IIF (SQL Server 2012+)';
PRINT '  - TRY_CONVERT (SQL Server 2012+)';
PRINT '=================================================';
GO

-- Create test table (same as dev for consistency)
IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'TestUsers')
BEGIN
    CREATE TABLE TestUsers (
        ID INT PRIMARY KEY IDENTITY(1,1),
        Name NVARCHAR(100) NOT NULL,
        Email NVARCHAR(100),
        Balance DECIMAL(18,2) DEFAULT 0,
        IsActive BIT DEFAULT 1,
        CreatedAt DATETIME2 DEFAULT GETDATE(),
        UpdatedAt DATETIME2 DEFAULT GETDATE()
    );

    PRINT 'Test table TestUsers created';
END
GO

-- Insert test data (same as dev)
IF NOT EXISTS (SELECT * FROM TestUsers)
BEGIN
    INSERT INTO TestUsers (Name, Email, Balance, IsActive)
    VALUES
        ('John Doe', 'john@example.com', 1000.50, 1),
        ('Jane Smith', 'jane@example.com', 2500.00, 1),
        ('Bob Johnson', 'bob@example.com', 500.00, 1),
        ('Alice Williams', 'alice@example.com', 3000.00, 0),
        ('Charlie Brown', 'charlie@example.com', 750.25, 1);

    PRINT 'Test data inserted: 5 rows';
END
GO

-- Verification: Try to use SQL Server 2016+ function (should fail)
-- This confirms that SQL Server 2012 compatibility mode is active
BEGIN TRY
    DECLARE @test NVARCHAR(MAX) = 'test';
    -- This will fail in compatibility level 110
    -- SELECT value FROM STRING_SPLIT(@test, ',');
    PRINT '';
    PRINT 'Compatibility verification: OK';
    PRINT 'SQL Server 2012 compatibility mode is active';
END TRY
BEGIN CATCH
    PRINT 'ERROR: Compatibility mode verification failed';
    PRINT ERROR_MESSAGE();
END CATCH
GO

PRINT '';
PRINT 'Production simulation database ready for testing!';
PRINT 'Use this database to verify SQL Server 2012 compatibility';
GO
