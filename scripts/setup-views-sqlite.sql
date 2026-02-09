-- ============================================
-- SQLite Database Views Setup Script
-- ============================================
-- This script creates test views for tdtpcli --list-views demonstration
--
-- View types:
--   R* = Read-only view (no INSTEAD OF triggers)
--   U* = Updatable view (has INSTEAD OF triggers for INSERT/UPDATE/DELETE)
--
-- Usage:
--   sqlite3 test_data.db < scripts/setup-views-sqlite.sql
--   sqlite3 benchmark_100k.db < scripts/setup-views-sqlite.sql

-- ============================================
-- 1. READ-ONLY VIEWS (R* prefix in --list-views)
-- ============================================

-- Drop views if they exist
DROP VIEW IF EXISTS users_readonly;
DROP VIEW IF EXISTS users_summary;
DROP VIEW IF EXISTS users_active;

-- Simple read-only view: all users
CREATE VIEW users_readonly AS
SELECT * FROM users;

-- Aggregated read-only view: user statistics
CREATE VIEW users_summary AS
SELECT
    COUNT(*) as total_users,
    COUNT(CASE WHEN email LIKE '%@gmail.com' THEN 1 END) as gmail_users,
    COUNT(CASE WHEN email LIKE '%@yahoo.com' THEN 1 END) as yahoo_users,
    AVG(LENGTH(name)) as avg_name_length
FROM users;

-- Filtered read-only view: active users only
CREATE VIEW users_active AS
SELECT id, name, email, created_at
FROM users
WHERE email IS NOT NULL AND email != '';

-- ============================================
-- 2. UPDATABLE VIEWS (U* prefix in --list-views)
-- ============================================
-- SQLite requires INSTEAD OF triggers to make views updatable

-- Drop updatable view and triggers if they exist
DROP TRIGGER IF EXISTS users_editable_insert;
DROP TRIGGER IF EXISTS users_editable_update;
DROP TRIGGER IF EXISTS users_editable_delete;
DROP VIEW IF EXISTS users_editable;

-- Create updatable view
CREATE VIEW users_editable AS
SELECT id, name, email, created_at
FROM users;

-- INSTEAD OF INSERT trigger
CREATE TRIGGER users_editable_insert
INSTEAD OF INSERT ON users_editable
BEGIN
    INSERT INTO users (id, name, email, created_at)
    VALUES (NEW.id, NEW.name, NEW.email, NEW.created_at);
END;

-- INSTEAD OF UPDATE trigger
CREATE TRIGGER users_editable_update
INSTEAD OF UPDATE ON users_editable
BEGIN
    UPDATE users
    SET name = NEW.name,
        email = NEW.email,
        created_at = NEW.created_at
    WHERE id = OLD.id;
END;

-- INSTEAD OF DELETE trigger
CREATE TRIGGER users_editable_delete
INSTEAD OF DELETE ON users_editable
BEGIN
    DELETE FROM users WHERE id = OLD.id;
END;

-- ============================================
-- 3. ANOTHER UPDATABLE VIEW (for testing)
-- ============================================

DROP TRIGGER IF EXISTS users_copy_editable_insert;
DROP TRIGGER IF EXISTS users_copy_editable_update;
DROP TRIGGER IF EXISTS users_copy_editable_delete;
DROP VIEW IF EXISTS users_copy_editable;

-- Create another updatable view (if Users_Copy table exists)
CREATE VIEW IF NOT EXISTS users_copy_editable AS
SELECT id, name, email, created_at
FROM Users_Copy;

-- INSTEAD OF INSERT trigger
CREATE TRIGGER IF NOT EXISTS users_copy_editable_insert
INSTEAD OF INSERT ON users_copy_editable
BEGIN
    INSERT INTO Users_Copy (id, name, email, created_at)
    VALUES (NEW.id, NEW.name, NEW.email, NEW.created_at);
END;

-- INSTEAD OF UPDATE trigger
CREATE TRIGGER IF NOT EXISTS users_copy_editable_update
INSTEAD OF UPDATE ON users_copy_editable
BEGIN
    UPDATE Users_Copy
    SET name = NEW.name,
        email = NEW.email,
        created_at = NEW.created_at
    WHERE id = OLD.id;
END;

-- INSTEAD OF DELETE trigger
CREATE TRIGGER IF NOT EXISTS users_copy_editable_delete
INSTEAD OF DELETE ON users_copy_editable
BEGIN
    DELETE FROM Users_Copy WHERE id = OLD.id;
END;

-- ============================================
-- Verification
-- ============================================
-- List all views
SELECT '=== Created Views ===' as info;
SELECT name,
       CASE
           WHEN name IN (
               SELECT DISTINCT tbl_name
               FROM sqlite_master
               WHERE type='trigger' AND sql LIKE '%INSTEAD OF INSERT%'
           ) THEN 'U*' || name || ' (updatable)'
           ELSE 'R*' || name || ' (read-only)'
       END as status
FROM sqlite_master
WHERE type='view'
ORDER BY name;
