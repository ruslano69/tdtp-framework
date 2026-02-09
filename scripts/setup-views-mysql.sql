-- ============================================
-- MySQL Database Views Setup Script
-- ============================================
-- This script creates test views for tdtpcli --list-views demonstration
--
-- View types:
--   U* = Updatable view (simple SELECT, meets MySQL updatable criteria)
--   R* = Read-only view (complex SELECT with aggregates/joins)
--
-- MySQL updatable view requirements:
--   - Simple SELECT from single table or updatable view
--   - No GROUP BY, HAVING, DISTINCT, aggregates
--   - No UNION, subqueries in FROM clause
--   - All columns must be simple column references
--
-- Usage:
--   mysql -h localhost -u root -p testdb < scripts/setup-views-mysql.sql
--   # Or via Docker:
--   docker exec -i mysql_container mysql -u root -ptestpass testdb < scripts/setup-views-mysql.sql

-- ============================================
-- 1. READ-ONLY VIEWS (R* prefix in --list-views)
-- ============================================

-- Drop views if they exist
DROP VIEW IF EXISTS users_readonly;
DROP VIEW IF EXISTS users_summary;
DROP VIEW IF EXISTS users_with_stats;

-- Complex view with aggregates - NOT updatable
CREATE VIEW users_summary AS
SELECT
    COUNT(*) as total_users,
    SUM(CASE WHEN email LIKE '%@gmail.com' THEN 1 ELSE 0 END) as gmail_users,
    SUM(CASE WHEN email LIKE '%@yahoo.com' THEN 1 ELSE 0 END) as yahoo_users,
    AVG(LENGTH(name)) as avg_name_length,
    MIN(created_at) as first_user_date,
    MAX(created_at) as last_user_date
FROM users;

-- View with DISTINCT - NOT updatable
CREATE VIEW users_readonly AS
SELECT DISTINCT
    SUBSTRING_INDEX(email, '@', -1) as email_domain,
    COUNT(*) as user_count
FROM users
WHERE email IS NOT NULL
GROUP BY SUBSTRING_INDEX(email, '@', -1);

-- View with derived column - NOT updatable
CREATE VIEW users_with_stats AS
SELECT
    id,
    name,
    email,
    created_at,
    CONCAT(UPPER(LEFT(name, 1)), LOWER(SUBSTRING(name, 2))) as name_formatted,
    TIMESTAMPDIFF(DAY, created_at, NOW()) as days_since_created
FROM users;

-- ============================================
-- 2. UPDATABLE VIEWS (U* prefix in --list-views)
-- ============================================
-- MySQL automatically makes these updatable because they meet the criteria

-- Drop updatable views if they exist
DROP VIEW IF EXISTS users_editable;
DROP VIEW IF EXISTS users_active;
DROP VIEW IF EXISTS users_recent;

-- Simple view - automatically UPDATABLE
CREATE VIEW users_editable AS
SELECT id, name, email, created_at
FROM users;

-- Filtered view - automatically UPDATABLE (with CHECK OPTION)
CREATE VIEW users_active AS
SELECT id, name, email, created_at
FROM users
WHERE email IS NOT NULL AND email != ''
WITH CHECK OPTION;

-- Another simple updatable view with WHERE clause
CREATE VIEW users_recent AS
SELECT id, name, email, created_at
FROM users
WHERE created_at >= DATE_SUB(CURDATE(), INTERVAL 30 DAY);

-- ============================================
-- 3. TESTING: Updatable view with limited columns
-- ============================================
-- This view is updatable but only for the columns it exposes

DROP VIEW IF EXISTS users_limited;

CREATE VIEW users_limited AS
SELECT id, name, email
FROM users;

-- Note: This view is updatable, but you can only UPDATE id, name, email
-- You cannot UPDATE created_at through this view

-- ============================================
-- Verification
-- ============================================
-- List all views with updatable status
SELECT '=== Created Views ===' as info;

SELECT
    table_name,
    CASE is_updatable
        WHEN 'YES' THEN CONCAT('U*', table_name, ' (updatable)')
        ELSE CONCAT('R*', table_name, ' (read-only)')
    END as status,
    is_updatable
FROM information_schema.views
WHERE table_schema = DATABASE()
  AND table_name LIKE 'users%'
ORDER BY table_name;

-- Show view definitions
SELECT '=== View Definitions ===' as info;
SHOW CREATE VIEW users_editable\G
SHOW CREATE VIEW users_summary\G

-- Test queries
SELECT '=== Test: Count records in views ===' as info;
SELECT 'users_editable' as view_name, COUNT(*) as record_count FROM users_editable
UNION ALL
SELECT 'users_active' as view_name, COUNT(*) as record_count FROM users_active
UNION ALL
SELECT 'users_recent' as view_name, COUNT(*) as record_count FROM users_recent;
