-- ============================================================
-- In-app messaging between users
-- ============================================================

CREATE TABLE IF NOT EXISTS arteria.messages_internal (
    message_id uuid,
    from_user text,
    to_user text,
    subject text,
    body text,
    is_read boolean,
    created_at timestamp,
    PRIMARY KEY (message_id)
);

-- Index for inbox queries
CREATE INDEX IF NOT EXISTS ON arteria.messages_internal (to_user);
CREATE INDEX IF NOT EXISTS ON arteria.messages_internal (from_user);

-- Organisations / Tenants
CREATE TABLE IF NOT EXISTS arteria.organisations (
    org_id uuid,
    name text,
    slug text,
    custom_domain text,
    branding_logo_url text,
    branding_app_name text,
    branding_primary_color text,
    branding_accent_color text,
    branding_favicon_url text,
    support_email text,
    is_active boolean,
    created_at timestamp,
    updated_at timestamp,
    PRIMARY KEY (org_id)
);

CREATE INDEX IF NOT EXISTS ON arteria.organisations (slug);
CREATE INDEX IF NOT EXISTS ON arteria.organisations (custom_domain);

-- Link users to organisations
ALTER TABLE arteria.users ADD org_id uuid;
