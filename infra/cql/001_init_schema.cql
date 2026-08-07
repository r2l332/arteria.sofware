CREATE KEYSPACE IF NOT EXISTS arteria WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1} AND tablets = {'enabled': false};

-- ============================================================
-- Communication Points
-- Configurable inbound/outbound endpoints
-- ============================================================
CREATE TABLE IF NOT EXISTS arteria.communication_points (
    comm_point_id uuid,
    name text,
    direction text,           -- 'INPUT' or 'OUTPUT'
    protocol text,            -- 'MLLP', 'HTTP', 'TCP', 'REST'
    host text,
    port int,
    path text,                -- For HTTP endpoints (e.g., '/fhir/Bundle')
    is_active boolean,
    max_retries int,
    retry_delay_ms int,
    timeout_ms int,
    config_json text,         -- Additional protocol-specific config as JSON
    tunnel_enabled boolean,   -- When true, traffic is tunneled via a remote agent
    tunnel_node_id uuid,      -- Which tunnel node handles this CP
    tunnel_local_port int,    -- Port the agent listens on at the remote site
    created_at timestamp,
    updated_at timestamp,
    PRIMARY KEY (comm_point_id)
);

-- ============================================================
-- Routes (connects source → filter chain → destination)
-- ============================================================
CREATE TABLE IF NOT EXISTS arteria.routes (
    route_id uuid,
    name text,
    description text,
    source_comm_point_id uuid,
    dest_comm_point_id uuid,
    source_topic text,
    destination_topic text,
    is_active boolean,
    retention_days int,
    fan_out_cp_ids list<uuid>,
    fan_out_config text,
    default_properties text,
    next_route_id uuid,
    created_at timestamp,
    updated_at timestamp,
    PRIMARY KEY (route_id)
);

-- ============================================================
-- Filters (ordered processing steps within a route)
-- Types: JS Transform, Conditional Router, Lookup Enrichment
-- ============================================================
CREATE TABLE IF NOT EXISTS arteria.filters (
    filter_id uuid,
    route_id uuid,
    name text,
    filter_type text,         -- 'javascript', 'conditional', 'lookup', 'schema_validate', 'duplicate_check'
    execution_order int,      -- Order within the route's filter chain
    js_script text,           -- JavaScript code for 'javascript' and 'conditional' types
    config_json text,         -- Type-specific config (lookup table ID, schema def, etc.)
    is_active boolean,
    created_at timestamp,
    PRIMARY KEY (route_id, execution_order)
);

-- Index to look up individual filters
CREATE TABLE IF NOT EXISTS arteria.filters_by_id (
    filter_id uuid,
    route_id uuid,
    name text,
    filter_type text,
    execution_order int,
    js_script text,
    config_json text,
    is_active boolean,
    created_at timestamp,
    PRIMARY KEY (filter_id)
);

-- ============================================================
-- Lookup Tables
-- Shared key-value data accessible from JS filters
-- ============================================================
CREATE TABLE IF NOT EXISTS arteria.lookup_tables (
    table_id uuid,
    name text,
    description text,
    created_at timestamp,
    updated_at timestamp,
    PRIMARY KEY (table_id)
);

CREATE TABLE IF NOT EXISTS arteria.lookup_entries (
    table_id uuid,
    lookup_key text,
    lookup_value text,
    PRIMARY KEY (table_id, lookup_key)
);

-- ============================================================
-- Messages (full audit trail with lifecycle tracking)
-- ============================================================
CREATE TABLE IF NOT EXISTS arteria.messages (
    message_id uuid,
    route_id uuid,
    comm_point_id uuid,
    patient_id text,
    message_type text,
    trigger_event text,
    sending_facility text,
    raw_payload text,
    transformed_payload text,
    properties text,          -- JSON map of message properties
    status text,              -- RECEIVED, FILTERING, FILTERED, ROUTED, DELIVERED, ERROR, DLQ
    created_at timestamp,
    updated_at timestamp,
    error_details text,
    retry_count int,
    PRIMARY KEY (message_id)
);

CREATE TABLE IF NOT EXISTS arteria.messages_by_patient (
    patient_id text,
    created_at timestamp,
    message_id uuid,
    PRIMARY KEY (patient_id, created_at)
) WITH CLUSTERING ORDER BY (created_at DESC);

-- Messages by status for dashboard queries
CREATE TABLE IF NOT EXISTS arteria.messages_by_status (
    status text,
    created_at timestamp,
    message_id uuid,
    message_type text,
    patient_id text,
    PRIMARY KEY (status, created_at)
) WITH CLUSTERING ORDER BY (created_at DESC);

-- ============================================================
-- Error / Dead Letter Queue records
-- ============================================================
CREATE TABLE IF NOT EXISTS arteria.error_messages (
    message_id uuid,
    route_id uuid,
    filter_id uuid,
    error_type text,          -- 'FILTER_ERROR', 'TIMEOUT', 'DELIVERY_FAILED', 'VALIDATION'
    error_details text,
    raw_payload text,
    retry_count int,
    max_retries int,
    created_at timestamp,
    PRIMARY KEY (message_id)
);
