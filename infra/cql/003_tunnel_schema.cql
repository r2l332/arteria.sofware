-- ============================================================
-- Tunnel Mesh — Node registry and CP mappings
-- ============================================================

-- Tunnel nodes (agents deployed at remote sites)
CREATE TABLE IF NOT EXISTS arteria.tunnel_nodes (
    node_id uuid,
    name text,
    site_name text,               -- e.g., "Hospital A", "Lab Downtown"
    enrollment_token text,        -- One-time token for agent enrollment
    status text,                  -- 'PENDING', 'ENROLLED', 'CONNECTED', 'DISCONNECTED'
    agent_version text,
    last_seen timestamp,
    public_key text,              -- Agent's public key (after enrollment)
    cert_serial text,
    created_at timestamp,
    updated_at timestamp,
    PRIMARY KEY (node_id)
);

-- Tunnel port mappings are now derived from communication_points.
-- When tunnel_enabled=true on a CP, the broker auto-generates mappings
-- and pushes them to the linked tunnel node.

-- Add tunnel columns to communication_points:
--   tunnel_enabled boolean   — enables encrypted tunnel for this CP
--   tunnel_node_id uuid      — which agent node handles this CP
--   tunnel_local_port int    — port the agent listens on at the remote site
