-- ============================================================
-- Migration: Add must_change_password to users table
-- Safe to re-run (ScyllaDB ignores duplicate column adds after first run)
-- ============================================================

ALTER TABLE arteria.users ADD must_change_password boolean;
