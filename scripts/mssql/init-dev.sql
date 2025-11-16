-- MS SQL Server Development Database Initialization
-- Version: 1.0
-- Date: 16.11.2025
-- Purpose: Development environment with modern SQL Server features

-- Create development database
IF NOT EXISTS (SELECT name FROM sys.databases WHERE name = 'DevDB')
BEGIN
    CREATE DATABASE DevDB;
END
GO

USE DevDB;
GO

-- Note: Development can use modern features
-- But be careful - code must also work in SQL Server 2012 compatibility mode!
PRINT 'Development database created successfully';
PRINT 'Database: DevDB';
PRINT 'Compatibility Level: ' + CAST((SELECT compatibility_level FROM sys.databases WHERE name = 'DevDB') AS VARCHAR);
GO

-- Create test table for adapter development
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

-- Insert test data
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

PRINT 'Development database initialization complete!';
GO
