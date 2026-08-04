-- ============================================================
-- Security Audit Log
-- ============================================================
CREATE TABLE IF NOT EXISTS arteria.audit_log (
    event_id uuid,
    timestamp timestamp,
    username text,
    action text,          -- 'POST', 'PUT', 'DELETE', 'LOGIN', 'LOGIN_FAILED', 'LOGOUT'
    resource text,        -- API path (e.g., '/api/v1/routes')
    resource_id text,
    client_ip text,
    user_agent text,
    details text,         -- JSON with additional context
    PRIMARY KEY ((username), timestamp, event_id)
) WITH CLUSTERING ORDER BY (timestamp DESC)
  AND default_time_to_live = 31536000;  -- 1 year retention

-- Index for searching by action type
CREATE TABLE IF NOT EXISTS arteria.audit_log_by_action (
    action text,
    timestamp timestamp,
    event_id uuid,
    username text,
    resource text,
    client_ip text,
    PRIMARY KEY ((action), timestamp, event_id)
) WITH CLUSTERING ORDER BY (timestamp DESC)
  AND default_time_to_live = 31536000;

-- ============================================================
-- PHI Access Log (who accessed which message — HIPAA requirement)
-- ============================================================
CREATE TABLE IF NOT EXISTS arteria.phi_access_log (
    access_id uuid,
    timestamp timestamp,
    username text,
    message_id uuid,
    patient_id text,
    action text,          -- 'VIEW', 'EXPORT', 'SEARCH'
    client_ip text,
    PRIMARY KEY ((patient_id), timestamp, access_id)
) WITH CLUSTERING ORDER BY (timestamp DESC)
  AND default_time_to_live = 63072000;  -- 2 year retention (HIPAA requires 6 years, adjust for prod)

-- ============================================================
-- Session Management
-- ============================================================
CREATE TABLE IF NOT EXISTS arteria.active_sessions (
    session_id text,
    user_id uuid,
    username text,
    created_at timestamp,
    expires_at timestamp,
    client_ip text,
    user_agent text,
    is_active boolean,
    PRIMARY KEY (session_id)
) WITH default_time_to_live = 86400;  -- Auto-expire after 24h
