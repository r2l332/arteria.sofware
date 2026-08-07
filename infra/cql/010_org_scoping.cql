-- Add org_id to key tables for multi-tenant scoping
ALTER TABLE arteria.communication_points ADD org_id uuid;
ALTER TABLE arteria.routes ADD org_id uuid;
ALTER TABLE arteria.messages ADD org_id uuid;
ALTER TABLE arteria.error_messages ADD org_id uuid;

-- Index for org-scoped queries
CREATE INDEX IF NOT EXISTS ON arteria.messages (org_id);
CREATE INDEX IF NOT EXISTS ON arteria.error_messages (org_id);
CREATE INDEX IF NOT EXISTS ON arteria.routes (org_id);
CREATE INDEX IF NOT EXISTS ON arteria.communication_points (org_id);
