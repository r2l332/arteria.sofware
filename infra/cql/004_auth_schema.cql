-- ============================================================
-- Authentication — User accounts
-- ============================================================

CREATE TABLE IF NOT EXISTS arteria.users (
    user_id uuid,
    username text,
    password_hash text,
    role text,            -- 'admin', 'operator', 'viewer'
    is_active boolean,
    created_at timestamp,
    PRIMARY KEY (user_id)
);

-- Index for login lookups by username
CREATE INDEX IF NOT EXISTS ON arteria.users (username);
