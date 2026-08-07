-- Migration: Add unique constraint to prevent duplicate Check Runs
-- Applies to Gitea's check_run table in models/actions/
--
-- Integration: Add this migration to Gitea's models/migrations/ directory
-- and register it in models/migrations/migrations.go
--
-- Compatible with: PostgreSQL, MySQL, SQLite, MSSQL

-- PostgreSQL
-- ALTER TABLE check_run ADD CONSTRAINT uq_check_run_repo_external
--   UNIQUE (repo_id, external_id);
--
-- On conflict during insert in models/actions/check_run.go:
--   INSERT INTO check_run (...) VALUES (...)
--   ON CONFLICT (repo_id, external_id) DO NOTHING;

-- MySQL
-- ALTER TABLE check_run ADD UNIQUE INDEX uq_check_run_repo_external (repo_id, external_id);
--
-- On conflict during insert:
--   INSERT INTO check_run (...) VALUES (...)
--   ON DUPLICATE KEY UPDATE id=id;  -- no-op

-- SQLite (add to model struct in check_run.go)
-- Add unique index via XORM tag:
--   ExternalID string `xorm:"UNIQUE(repo_id_external)"`

-- MSSQL
-- CREATE UNIQUE INDEX uq_check_run_repo_external
--   ON check_run (repo_id, external_id);
--
-- Use MERGE or check existence before INSERT
