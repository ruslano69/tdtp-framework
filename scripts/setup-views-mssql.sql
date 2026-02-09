-- ============================================
-- MS SQL Server Database Views Setup Script
-- ============================================
-- This script creates test views for tdtpcli --list-views demonstration
--
-- View types:
--   U* = Updatable view (simple SELECT, meets SQL Server updatable criteria)
--   R* = Read-only view (complex SELECT with aggregates/joins)
--
-- MS SQL Server updatable view requirements:
--   - Simple SELECT from single base table or updatable view
--   - No GROUP BY, HAVING, DISTINCT, TOP, aggregates
--   - No UNION, derived tables, or CTEs in FROM clause
--   - All columns must be simple column references (not expressions)
--
-- Usage:
--   sqlcmd -S localhost -U sa -P YourPassword -d testdb -i scripts/setup-views-mssql.sql
--   # Or via Docker:
--   docker exec -i mssql_container /opt/mssql-tools/bin/sqlcmd -S localhost -U sa -P YourPassword -d testdb -i /scripts/setup-views-mssql.sql

USE testdb;
GO

-- ============================================
-- 1. READ-ONLY VIEWS (R* prefix in --list-views)
-- ============================================

-- Drop views if they exist
IF OBJECT_ID('dbo.users_readonly', 'V') IS NOT NULL
    DROP VIEW dbo.users_readonly;
GO

IF OBJECT_ID('dbo.users_summary', 'V') IS NOT NULL
    DROP VIEW dbo.users_summary;
GO

IF OBJECT_ID('dbo.users_with_stats', 'V') IS NOT NULL
    DROP VIEW dbo.users_with_stats;
GO

-- Complex view with aggregates - NOT updatable
CREATE VIEW dbo.users_summary AS
SELECT
    COUNT(*) as total_users,
    SUM(CASE WHEN email LIKE '%@gmail.com' THEN 1 ELSE 0 END) as gmail_users,
    SUM(CASE WHEN email LIKE '%@yahoo.com' THEN 1 ELSE 0 END) as yahoo_users,
    AVG(LEN(name)) as avg_name_length,
    MIN(created_at) as first_user_date,
    MAX(created_at) as last_user_date
FROM users;
GO

-- View with DISTINCT - NOT updatable
CREATE VIEW dbo.users_readonly AS
SELECT DISTINCT
    SUBSTRING(email, CHARINDEX('@', email) + 1, LEN(email)) as email_domain,
    COUNT(*) OVER (PARTITION BY SUBSTRING(email, CHARINDEX('@', email) + 1, LEN(email))) as domain_count
FROM users
WHERE email IS NOT NULL;
GO

-- View with derived columns - NOT updatable
CREATE VIEW dbo.users_with_stats AS
SELECT
    id,
    name,
    email,
    created_at,
    UPPER(LEFT(name, 1)) + LOWER(SUBSTRING(name, 2, LEN(name))) as name_formatted,
    DATEDIFF(DAY, created_at, GETDATE()) as days_since_created,
    ROW_NUMBER() OVER (ORDER BY created_at) as row_num
FROM users;
GO

-- ============================================
-- 2. UPDATABLE VIEWS (U* prefix in --list-views)
-- ============================================
-- MS SQL Server automatically makes these updatable

-- Drop updatable views if they exist
IF OBJECT_ID('dbo.users_editable', 'V') IS NOT NULL
    DROP VIEW dbo.users_editable;
GO

IF OBJECT_ID('dbo.users_active', 'V') IS NOT NULL
    DROP VIEW dbo.users_active;
GO

IF OBJECT_ID('dbo.users_recent', 'V') IS NOT NULL
    DROP VIEW dbo.users_recent;
GO

-- Simple view - automatically UPDATABLE
CREATE VIEW dbo.users_editable AS
SELECT id, name, email, created_at
FROM users;
GO

-- Filtered view - automatically UPDATABLE (with CHECK OPTION)
CREATE VIEW dbo.users_active AS
SELECT id, name, email, created_at
FROM users
WHERE email IS NOT NULL AND email <> ''
WITH CHECK OPTION;
GO

-- Another simple updatable view with WHERE clause
CREATE VIEW dbo.users_recent AS
SELECT id, name, email, created_at
FROM users
WHERE created_at >= DATEADD(DAY, -30, GETDATE());
GO

-- ============================================
-- 3. TESTING: Updatable view with INSTEAD OF triggers
-- ============================================
-- Manually make a complex view updatable using INSTEAD OF triggers

IF OBJECT_ID('dbo.users_manual_update', 'V') IS NOT NULL
    DROP VIEW dbo.users_manual_update;
GO

CREATE VIEW dbo.users_manual_update AS
SELECT id, UPPER(name) as name_upper, email, created_at
FROM users;
GO

-- INSTEAD OF INSERT trigger
IF OBJECT_ID('dbo.trg_users_manual_update_insert', 'TR') IS NOT NULL
    DROP TRIGGER dbo.trg_users_manual_update_insert;
GO

CREATE TRIGGER dbo.trg_users_manual_update_insert
ON dbo.users_manual_update
INSTEAD OF INSERT
AS
BEGIN
    SET NOCOUNT ON;
    INSERT INTO users (id, name, email, created_at)
    SELECT id, LOWER(name_upper), email, created_at
    FROM inserted;
END;
GO

-- INSTEAD OF UPDATE trigger
IF OBJECT_ID('dbo.trg_users_manual_update_update', 'TR') IS NOT NULL
    DROP TRIGGER dbo.trg_users_manual_update_update;
GO

CREATE TRIGGER dbo.trg_users_manual_update_update
ON dbo.users_manual_update
INSTEAD OF UPDATE
AS
BEGIN
    SET NOCOUNT ON;
    UPDATE u
    SET u.name = LOWER(i.name_upper),
        u.email = i.email,
        u.created_at = i.created_at
    FROM users u
    INNER JOIN inserted i ON u.id = i.id;
END;
GO

-- INSTEAD OF DELETE trigger
IF OBJECT_ID('dbo.trg_users_manual_update_delete', 'TR') IS NOT NULL
    DROP TRIGGER dbo.trg_users_manual_update_delete;
GO

CREATE TRIGGER dbo.trg_users_manual_update_delete
ON dbo.users_manual_update
INSTEAD OF DELETE
AS
BEGIN
    SET NOCOUNT ON;
    DELETE FROM users
    WHERE id IN (SELECT id FROM deleted);
END;
GO

-- ============================================
-- Verification
-- ============================================
-- List all views with updatable status
PRINT '=== Created Views ===';
SELECT
    TABLE_NAME as view_name,
    CASE IS_UPDATABLE
        WHEN 'YES' THEN 'U*' + TABLE_NAME + ' (updatable)'
        ELSE 'R*' + TABLE_NAME + ' (read-only)'
    END as status,
    IS_UPDATABLE
FROM INFORMATION_SCHEMA.VIEWS
WHERE TABLE_SCHEMA = 'dbo'
  AND TABLE_NAME LIKE 'users%'
ORDER BY TABLE_NAME;
GO

-- Show view definitions
PRINT '=== View Definitions ===';
EXEC sp_helptext 'dbo.users_editable';
GO
EXEC sp_helptext 'dbo.users_summary';
GO

-- Test queries
PRINT '=== Test: Count records in views ===';
SELECT 'users_editable' as view_name, COUNT(*) as record_count FROM dbo.users_editable
UNION ALL
SELECT 'users_active' as view_name, COUNT(*) as record_count FROM dbo.users_active
UNION ALL
SELECT 'users_recent' as view_name, COUNT(*) as record_count FROM dbo.users_recent;
GO
