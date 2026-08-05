-- Seed: Default MLLP Input Communication Point
INSERT INTO arteria.communication_points (comm_point_id, name, direction, protocol, host, port, is_active, max_retries, retry_delay_ms, timeout_ms, created_at, updated_at)
VALUES (
    a1000000-0000-0000-0000-000000000001,
    'Default MLLP Input',
    'INPUT',
    'MLLP',
    '0.0.0.0',
    2575,
    true,
    0,
    0,
    30000,
    toTimestamp(now()),
    toTimestamp(now())
) IF NOT EXISTS;

-- Seed: Default Output Communication Point (Discard sink — change via UI)
INSERT INTO arteria.communication_points (comm_point_id, name, direction, protocol, host, port, is_active, max_retries, retry_delay_ms, timeout_ms, created_at, updated_at)
VALUES (
    a1000000-0000-0000-0000-000000000002,
    'Default Output (Discard)',
    'OUTPUT',
    'DISCARD',
    '',
    0,
    true,
    0,
    0,
    0,
    toTimestamp(now()),
    toTimestamp(now())
) IF NOT EXISTS;

-- Seed: Default ADT Route
INSERT INTO arteria.routes (route_id, name, description, source_comm_point_id, dest_comm_point_id, source_topic, destination_topic, is_active, created_at, updated_at)
VALUES (
    b1000000-0000-0000-0000-000000000001,
    'ADT Admissions Route',
    'Routes all ADT^A01 messages through a sample transform',
    a1000000-0000-0000-0000-000000000001,
    a1000000-0000-0000-0000-000000000002,
    'ADT^A01',
    'admissions',
    true,
    toTimestamp(now()),
    toTimestamp(now())
) IF NOT EXISTS;

-- Seed: Catch-all Route
INSERT INTO arteria.routes (route_id, name, description, source_comm_point_id, dest_comm_point_id, source_topic, destination_topic, is_active, created_at, updated_at)
VALUES (
    b1000000-0000-0000-0000-000000000002,
    'Catch-All Route',
    'Catches all unmatched messages and passes them through',
    a1000000-0000-0000-0000-000000000001,
    a1000000-0000-0000-0000-000000000002,
    '*',
    'default',
    true,
    toTimestamp(now()),
    toTimestamp(now())
) IF NOT EXISTS;

-- Seed: Sample JS Transform Filter on ADT Route
INSERT INTO arteria.filters (filter_id, route_id, name, filter_type, execution_order, js_script, is_active, created_at)
VALUES (
    c1000000-0000-0000-0000-000000000001,
    b1000000-0000-0000-0000-000000000001,
    'Enrich ADT Message',
    'javascript',
    1,
    'function transform(msg) { msg.properties.processed_by = "arteria"; msg.properties.processed_at = new Date().toISOString(); return msg; }',
    true,
    toTimestamp(now())
) IF NOT EXISTS;

INSERT INTO arteria.filters_by_id (filter_id, route_id, name, filter_type, execution_order, js_script, is_active, created_at)
VALUES (
    c1000000-0000-0000-0000-000000000001,
    b1000000-0000-0000-0000-000000000001,
    'Enrich ADT Message',
    'javascript',
    1,
    'function transform(msg) { msg.properties.processed_by = "arteria"; msg.properties.processed_at = new Date().toISOString(); return msg; }',
    true,
    toTimestamp(now())
) IF NOT EXISTS;

-- Seed: Conditional Filter (example: drop messages without patient ID)
INSERT INTO arteria.filters (filter_id, route_id, name, filter_type, execution_order, js_script, is_active, created_at)
VALUES (
    c1000000-0000-0000-0000-000000000002,
    b1000000-0000-0000-0000-000000000001,
    'Validate Patient ID',
    'conditional',
    0,
    'function evaluate(msg) { if (!msg.patientId || msg.patientId === "") { return { action: "reject", reason: "Missing Patient ID" }; } return { action: "pass" }; }',
    true,
    toTimestamp(now())
) IF NOT EXISTS;

INSERT INTO arteria.filters_by_id (filter_id, route_id, name, filter_type, execution_order, js_script, is_active, created_at)
VALUES (
    c1000000-0000-0000-0000-000000000002,
    b1000000-0000-0000-0000-000000000001,
    'Validate Patient ID',
    'conditional',
    0,
    'function evaluate(msg) { if (!msg.patientId || msg.patientId === "") { return { action: "reject", reason: "Missing Patient ID" }; } return { action: "pass" }; }',
    true,
    toTimestamp(now())
) IF NOT EXISTS;
