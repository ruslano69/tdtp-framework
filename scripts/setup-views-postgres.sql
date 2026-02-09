-- ============================================
-- PostgreSQL Database Views Setup Script
-- ============================================
-- This script creates test views for tdtpcli --list-views demonstration
--
-- View types:
--   U* = Updatable view (simple SELECT, automatically updatable)
--   R* = Read-only view (complex SELECT with JOIN/GROUP BY/aggregates)
--
-- PostgreSQL automatically determines if a view is updatable based on:
--   - Simple SELECT from single table
--   - No GROUP BY, HAVING, DISTINCT, aggregates
--   - All columns from underlying table are present or have defaults
--
-- Usage:
--   psql -h localhost -U postgres -d testdb -f scripts/setup-views-postgres.sql
--   # Or via Docker:
--   docker exec -i postgres_container psql -U postgres -d testdb < scripts/setup-views-postgres.sql

-- ============================================
-- 1. READ-ONLY VIEWS (R* prefix in --list-views)
-- ============================================

-- Drop views if they exist
DROP VIEW IF EXISTS users_readonly CASCADE;
DROP VIEW IF EXISTS users_summary CASCADE;
DROP VIEW IF EXISTS users_with_stats CASCADE;

-- Complex view with aggregates - NOT updatable
CREATE VIEW users_summary AS
SELECT
    COUNT(*) as total_users,
    COUNT(*) FILTER (WHERE email LIKE '%@gmail.com') as gmail_users,
    COUNT(*) FILTER (WHERE email LIKE '%@yahoo.com') as yahoo_users,
    AVG(LENGTH(name)) as avg_name_length,
    MIN(created_at) as first_user_date,
    MAX(created_at) as last_user_date
FROM users;

COMMENT ON VIEW users_summary IS 'Read-only view: User statistics with aggregates';

-- View with DISTINCT - NOT updatable
CREATE VIEW users_readonly AS
SELECT DISTINCT
    SUBSTRING(email FROM POSITION('@' IN email) + 1) as email_domain,
    COUNT(*) OVER (PARTITION BY SUBSTRING(email FROM POSITION('@' IN email) + 1)) as domain_count
FROM users
WHERE email IS NOT NULL;

COMMENT ON VIEW users_readonly IS 'Read-only view: Email domains with counts';

-- View with window functions - NOT updatable
CREATE VIEW users_with_stats AS
SELECT
    id,
    name,
    email,
    created_at,
    ROW_NUMBER() OVER (ORDER BY created_at) as user_number,
    COUNT(*) OVER () as total_count
FROM users;

COMMENT ON VIEW users_with_stats IS 'Read-only view: Users with row numbers';

-- ============================================
-- 2. UPDATABLE VIEWS (U* prefix in --list-views)
-- ============================================
-- PostgreSQL automatically makes these updatable because they are simple SELECTs

-- Drop updatable views if they exist
DROP VIEW IF EXISTS users_editable CASCADE;
DROP VIEW IF EXISTS users_active CASCADE;
DROP VIEW IF EXISTS users_recent CASCADE;

-- Simple view - automatically UPDATABLE
CREATE VIEW users_editable AS
SELECT id, name, email, created_at
FROM users;

COMMENT ON VIEW users_editable IS 'Updatable view: Simple SELECT from users table';

-- Filtered view - automatically UPDATABLE (with CHECK OPTION)
CREATE VIEW users_active AS
SELECT id, name, email, created_at
FROM users
WHERE email IS NOT NULL AND email != ''
WITH CHECK OPTION;

COMMENT ON VIEW users_active IS 'Updatable view: Active users only (with CHECK OPTION)';

-- Another simple updatable view
CREATE VIEW users_recent AS
SELECT id, name, email, created_at
FROM users
WHERE created_at >= CURRENT_DATE - INTERVAL '30 days';

COMMENT ON VIEW users_recent IS 'Updatable view: Users created in last 30 days';

-- ============================================
-- 3. MIXED EXAMPLE: Making read-only view updatable with rules
-- ============================================
-- This demonstrates how to make a complex view updatable using INSTEAD OF rules

DROP VIEW IF EXISTS users_manual_update CASCADE;

CREATE VIEW users_manual_update AS
SELECT id, UPPER(name) as name_upper, email, created_at
FROM users;

-- Add INSTEAD OF INSERT rule to make it updatable
CREATE OR REPLACE RULE users_manual_update_insert AS
ON INSERT TO users_manual_update
DO INSTEAD
    INSERT INTO users (id, name, email, created_at)
    VALUES (NEW.id, LOWER(NEW.name_upper), NEW.email, NEW.created_at);

-- Add INSTEAD OF UPDATE rule
CREATE OR REPLACE RULE users_manual_update_update AS
ON UPDATE TO users_manual_update
DO INSTEAD
    UPDATE users
    SET name = LOWER(NEW.name_upper),
        email = NEW.email,
        created_at = NEW.created_at
    WHERE id = OLD.id;

-- Add INSTEAD OF DELETE rule
CREATE OR REPLACE RULE users_manual_update_delete AS
ON DELETE TO users_manual_update
DO INSTEAD
    DELETE FROM users WHERE id = OLD.id;

COMMENT ON VIEW users_manual_update IS 'Manually updatable view using INSTEAD OF rules';

-- ============================================
-- Verification
-- ============================================
-- List all views with updatable status
SELECT
    '=== Created Views ===' as info;

SELECT
    table_name,
    CASE is_updatable
        WHEN 'YES' THEN 'U*' || table_name || ' (updatable)'
        ELSE 'R*' || table_name || ' (read-only)'
    END as status,
    is_insertable_into,
    is_updatable
FROM information_schema.views
WHERE table_schema = 'public'
  AND table_name LIKE 'users%'
ORDER BY table_name;

-- Show view definitions
\echo '=== View Definitions ==='
\d+ users_editable
\d+ users_summary
