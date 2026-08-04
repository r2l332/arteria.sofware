-- ============================================================
-- Message Retention & Config Versioning
-- ============================================================

-- Set default TTL on message tables (30 days = 2592000 seconds)
ALTER TABLE arteria.messages WITH default_time_to_live = 2592000;
ALTER TABLE arteria.messages_by_patient WITH default_time_to_live = 2592000;
ALTER TABLE arteria.messages_by_status WITH default_time_to_live = 2592000;

-- Error messages kept longer (90 days = 7776000 seconds)
ALTER TABLE arteria.error_messages WITH default_time_to_live = 7776000;

-- Add retention_days to routes (overrides global TTL per-route)
-- Note: Applied at insert time via TTL per row, not table-level

-- ============================================================
-- Config Version History (audit trail for all config changes)
-- ============================================================
CREATE TABLE IF NOT EXISTS arteria.config_history (
    change_id uuid,
    entity_type text,         -- 'route', 'filter', 'comm_point', 'tunnel_node', 'lookup'
    entity_id uuid,
    action text,              -- 'CREATE', 'UPDATE', 'DELETE'
    changed_by text,          -- username
    before_json text,         -- JSON snapshot before change (null for CREATE)
    after_json text,          -- JSON snapshot after change (null for DELETE)
    created_at timestamp,
    PRIMARY KEY (entity_type, created_at, change_id)
) WITH CLUSTERING ORDER BY (created_at DESC)
  AND default_time_to_live = 31536000;  -- Keep history for 1 year

-- Config backups table (stores full config snapshots)
CREATE TABLE IF NOT EXISTS arteria.config_backups (
    backup_id uuid,
    name text,
    description text,
    backup_json text,         -- Full JSON export of all config
    created_by text,
    created_at timestamp,
    PRIMARY KEY (backup_id)
);
