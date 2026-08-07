-- ============================================================
-- Complex Demo: Multi-route hospital integration scenario
-- 
-- Scenario: Hospital with ED, Lab, and Radiology departments
-- Flow:
--   ED (ADT^A01) → Validate → Enrich from lookup → Fan-out to Lab + Radiology + Archive
--   Lab (ORM^O01) → Validate → Transform → Deliver to Lab system
--   Results (ORU^R01) → Validate → Extract → Chain to notification route
--   Catch-all (*) → Log to archive
-- ============================================================

-- ===== TUNNEL NODE =====
INSERT INTO arteria.tunnel_nodes (node_id, name, site_name, enrollment_token, status, agent_version, created_at, updated_at)
VALUES (
    a1b2c3d4-e5f6-7890-abcd-ef1234567890,
    'demo-hospital',
    'City General Hospital',
    'demo-token-e2e-2024',
    'PENDING',
    '0.1.0',
    toTimestamp(now()),
    toTimestamp(now())
);

-- ===== COMMUNICATION POINTS =====

-- Input: Emergency Department (ADT messages)
INSERT INTO arteria.communication_points (comm_point_id, name, direction, protocol, host, port, is_active, tunnel_enabled, tunnel_node_id, tunnel_local_port, config_json, created_at, updated_at)
VALUES (
    10000000-0000-0000-0000-000000000001,
    'ED Admissions (MLLP)',
    'INPUT', 'MLLP', '0.0.0.0', 2575, true, true,
    a1b2c3d4-e5f6-7890-abcd-ef1234567890, 2575,
    '{"description": "Emergency Department ADT feed via Capillary"}',
    toTimestamp(now()), toTimestamp(now())
);

-- Input: Lab Orders (ORM messages)
INSERT INTO arteria.communication_points (comm_point_id, name, direction, protocol, host, port, is_active, tunnel_enabled, tunnel_node_id, tunnel_local_port, config_json, created_at, updated_at)
VALUES (
    10000000-0000-0000-0000-000000000002,
    'Lab Orders (MLLP)',
    'INPUT', 'MLLP', '0.0.0.0', 2576, true, true,
    a1b2c3d4-e5f6-7890-abcd-ef1234567890, 2576,
    '{"description": "Lab system order feed"}',
    toTimestamp(now()), toTimestamp(now())
);

-- Input: Results (ORU messages)
INSERT INTO arteria.communication_points (comm_point_id, name, direction, protocol, host, port, is_active, tunnel_enabled, tunnel_node_id, tunnel_local_port, config_json, created_at, updated_at)
VALUES (
    10000000-0000-0000-0000-000000000003,
    'Results Feed (MLLP)',
    'INPUT', 'MLLP', '0.0.0.0', 2577, true, true,
    a1b2c3d4-e5f6-7890-abcd-ef1234567890, 2577,
    '{"description": "Lab/Radiology results feed"}',
    toTimestamp(now()), toTimestamp(now())
);

-- Output: Primary delivery back to hospital
INSERT INTO arteria.communication_points (comm_point_id, name, direction, protocol, host, port, is_active, tunnel_enabled, tunnel_node_id, tunnel_local_port, config_json, created_at, updated_at)
VALUES (
    20000000-0000-0000-0000-000000000001,
    'Hospital EMR (MLLP Return)',
    'OUTPUT', 'MLLP', '0.0.0.0', 2576, true, true,
    a1b2c3d4-e5f6-7890-abcd-ef1234567890, 2576,
    '{"description": "Processed messages delivered back to hospital EMR"}',
    toTimestamp(now()), toTimestamp(now())
);

-- Output: Lab System
INSERT INTO arteria.communication_points (comm_point_id, name, direction, protocol, host, port, is_active, tunnel_enabled, tunnel_node_id, tunnel_local_port, config_json, created_at, updated_at)
VALUES (
    20000000-0000-0000-0000-000000000002,
    'Lab System (MLLP)',
    'OUTPUT', 'MLLP', '0.0.0.0', 2578, true, false,
    null, 0,
    '{"description": "Lab information system"}',
    toTimestamp(now()), toTimestamp(now())
);

-- Output: Radiology System
INSERT INTO arteria.communication_points (comm_point_id, name, direction, protocol, host, port, is_active, tunnel_enabled, tunnel_node_id, tunnel_local_port, config_json, created_at, updated_at)
VALUES (
    20000000-0000-0000-0000-000000000003,
    'Radiology (RIS)',
    'OUTPUT', 'MLLP', '0.0.0.0', 2579, true, false,
    null, 0,
    '{"description": "Radiology Information System"}',
    toTimestamp(now()), toTimestamp(now())
);

-- Output: Archive (Discard/null sink for demo)
INSERT INTO arteria.communication_points (comm_point_id, name, direction, protocol, host, port, is_active, tunnel_enabled, tunnel_node_id, tunnel_local_port, config_json, created_at, updated_at)
VALUES (
    20000000-0000-0000-0000-000000000004,
    'Message Archive',
    'OUTPUT', 'DISCARD', '0.0.0.0', 0, true, false,
    null, 0,
    '{"description": "Long-term message archive (compliance)"}',
    toTimestamp(now()), toTimestamp(now())
);

-- Output: Notifications (webhook)
INSERT INTO arteria.communication_points (comm_point_id, name, direction, protocol, host, port, is_active, tunnel_enabled, tunnel_node_id, tunnel_local_port, config_json, created_at, updated_at)
VALUES (
    20000000-0000-0000-0000-000000000005,
    'Notifications (Webhook)',
    'OUTPUT', 'WEBHOOK', '0.0.0.0', 0, true, false,
    null, 0,
    '{"webhook_url": "http://localhost:8090/webhook", "webhook_method": "POST", "description": "Critical result notifications"}',
    toTimestamp(now()), toTimestamp(now())
);

-- ===== LOOKUP TABLE: Ward Codes =====
INSERT INTO arteria.lookup_tables (table_id, name, description, created_at) VALUES (
    aaaa0000-0000-0000-0000-000000000001, 'ward_names', 'Maps ward codes to display names', toTimestamp(now())
);
INSERT INTO arteria.lookup_entries (table_id, lookup_key, lookup_value) VALUES (aaaa0000-0000-0000-0000-000000000001, 'ED', 'Emergency Department');
INSERT INTO arteria.lookup_entries (table_id, lookup_key, lookup_value) VALUES (aaaa0000-0000-0000-0000-000000000001, 'ICU', 'Intensive Care Unit');
INSERT INTO arteria.lookup_entries (table_id, lookup_key, lookup_value) VALUES (aaaa0000-0000-0000-0000-000000000001, 'WARD_A', 'General Ward A');
INSERT INTO arteria.lookup_entries (table_id, lookup_key, lookup_value) VALUES (aaaa0000-0000-0000-0000-000000000001, 'WARD_B', 'General Ward B');
INSERT INTO arteria.lookup_entries (table_id, lookup_key, lookup_value) VALUES (aaaa0000-0000-0000-0000-000000000001, 'RAD', 'Radiology');
INSERT INTO arteria.lookup_entries (table_id, lookup_key, lookup_value) VALUES (aaaa0000-0000-0000-0000-000000000001, 'LAB', 'Pathology Laboratory');
INSERT INTO arteria.lookup_entries (table_id, lookup_key, lookup_value) VALUES (aaaa0000-0000-0000-0000-000000000001, 'OPD', 'Outpatient Department');

-- ===== ROUTES =====

-- Route 1: ED Admissions (ADT^A01) — Validate → Lookup → Enrich → Fan-out
INSERT INTO arteria.routes (route_id, name, description, source_comm_point_id, dest_comm_point_id, source_topic, destination_topic, is_active, default_properties, fan_out_config, created_at, updated_at)
VALUES (
    30000000-0000-0000-0000-000000000001,
    'ED Admissions Pipeline',
    'Validates patient ID, enriches with ward name, adds timestamps, fans out to Lab + Radiology + Archive',
    10000000-0000-0000-0000-000000000001,
    20000000-0000-0000-0000-000000000001,
    'ADT^A01',
    'admissions',
    true,
    '{"environment": "production", "source_dept": "emergency", "compliance": "hipaa"}',
    '[{"cp_id": "20000000-0000-0000-0000-000000000004", "name": "Archive", "condition_type": "", "condition": ""}]',
    toTimestamp(now()),
    toTimestamp(now())
);

-- Route 2: Lab Orders (ORM^O01) — Validate → Transform → Deliver to Lab
INSERT INTO arteria.routes (route_id, name, description, source_comm_point_id, dest_comm_point_id, source_topic, destination_topic, is_active, default_properties, created_at, updated_at)
VALUES (
    30000000-0000-0000-0000-000000000002,
    'Lab Orders Processing',
    'Validates and routes lab orders with priority classification',
    10000000-0000-0000-0000-000000000002,
    20000000-0000-0000-0000-000000000002,
    'ORM^O01',
    'lab_orders',
    true,
    '{"environment": "production", "source_dept": "ordering", "order_type": "lab"}',
    toTimestamp(now()),
    toTimestamp(now())
);

-- Route 3: Results (ORU^R01) — Validate → Extract → Chain to notifications
INSERT INTO arteria.routes (route_id, name, description, source_comm_point_id, dest_comm_point_id, source_topic, destination_topic, is_active, default_properties, next_route_id, created_at, updated_at)
VALUES (
    30000000-0000-0000-0000-000000000003,
    'Results Processing',
    'Processes lab/radiology results, extracts critical values, chains to notification route',
    10000000-0000-0000-0000-000000000003,
    20000000-0000-0000-0000-000000000001,
    'ORU^R01',
    'results',
    true,
    '{"environment": "production", "source_dept": "diagnostics"}',
    30000000-0000-0000-0000-000000000004,
    toTimestamp(now()),
    toTimestamp(now())
);

-- Route 4: Critical Result Notifications (chained from Route 3)
INSERT INTO arteria.routes (route_id, name, description, source_comm_point_id, dest_comm_point_id, source_topic, destination_topic, is_active, default_properties, created_at, updated_at)
VALUES (
    30000000-0000-0000-0000-000000000004,
    'Critical Result Alerts',
    'Evaluates results for critical values and sends notifications (chained from Results Processing)',
    10000000-0000-0000-0000-000000000003,
    20000000-0000-0000-0000-000000000005,
    'ORU^R01',
    'notifications',
    true,
    '{"alert_level": "critical", "notification_channel": "webhook"}',
    toTimestamp(now()),
    toTimestamp(now())
);

-- Route 5: Catch-All — Archive everything else
INSERT INTO arteria.routes (route_id, name, description, source_comm_point_id, dest_comm_point_id, source_topic, destination_topic, is_active, default_properties, created_at, updated_at)
VALUES (
    30000000-0000-0000-0000-000000000005,
    'Catch-All Archive',
    'Archives any message type not handled by a specific route',
    10000000-0000-0000-0000-000000000001,
    20000000-0000-0000-0000-000000000004,
    '*',
    'archive',
    true,
    '{"environment": "production", "handling": "archive_only"}',
    toTimestamp(now()),
    toTimestamp(now())
);

-- ===== FILTERS =====

-- Route 1 Filters: ED Admissions
-- Filter 1.1: Validate Patient ID (conditional JS)
INSERT INTO arteria.filters (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active, created_at)
VALUES (
    f1000000-0000-0000-0000-000000000001,
    30000000-0000-0000-0000-000000000001,
    'Validate Patient ID',
    'conditional', 0,
    $$function evaluate(msg) {
  if (!msg.patientId || msg.patientId.trim() === "") {
    return { action: "reject", reason: "ADT message missing Patient ID (PID-3)" };
  }
  if (msg.patientId.length < 3) {
    return { action: "reject", reason: "Patient ID too short: " + msg.patientId };
  }
  return { action: "pass" };
}$$,
    '', true, toTimestamp(now())
);
INSERT INTO arteria.filters_by_id (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active)
VALUES (f1000000-0000-0000-0000-000000000001, 30000000-0000-0000-0000-000000000001, 'Validate Patient ID', 'conditional', 0, '', '', true);

-- Filter 1.2: Lookup Ward Name
INSERT INTO arteria.filters (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active, created_at)
VALUES (
    f1000000-0000-0000-0000-000000000002,
    30000000-0000-0000-0000-000000000001,
    'Enrich Ward Name',
    'lookup', 1, '',
    '{"table_name": "ward_names", "lookup_field": "sendingFacility", "output_field": "ward_display_name"}',
    true, toTimestamp(now())
);
INSERT INTO arteria.filters_by_id (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active)
VALUES (f1000000-0000-0000-0000-000000000002, 30000000-0000-0000-0000-000000000001, 'Enrich Ward Name', 'lookup', 1, '', '{"table_name": "ward_names", "lookup_field": "sendingFacility", "output_field": "ward_display_name"}', true);

-- Filter 1.3: JS Enrichment (add timestamps, format output)
INSERT INTO arteria.filters (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active, created_at)
VALUES (
    f1000000-0000-0000-0000-000000000003,
    30000000-0000-0000-0000-000000000001,
    'Enrich & Timestamp',
    'javascript', 2,
    $$function transform(msg) {
  msg.properties.processed_at = new Date().toISOString();
  msg.properties.processed_by = "arteria";
  msg.properties.message_class = "admission";
  msg.properties.priority = msg.triggerEvent === "A01" ? "high" : "normal";
  return msg;
}$$,
    '', true, toTimestamp(now())
);
INSERT INTO arteria.filters_by_id (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active)
VALUES (f1000000-0000-0000-0000-000000000003, 30000000-0000-0000-0000-000000000001, 'Enrich & Timestamp', 'javascript', 2, '', '', true);

-- Route 2 Filters: Lab Orders
-- Filter 2.1: Validate Patient ID
INSERT INTO arteria.filters (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active, created_at)
VALUES (
    f2000000-0000-0000-0000-000000000001,
    30000000-0000-0000-0000-000000000002,
    'Validate Order',
    'conditional', 0,
    $$function evaluate(msg) {
  if (!msg.patientId) {
    return { action: "reject", reason: "Lab order missing Patient ID" };
  }
  return { action: "pass" };
}$$,
    '', true, toTimestamp(now())
);
INSERT INTO arteria.filters_by_id (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active)
VALUES (f2000000-0000-0000-0000-000000000001, 30000000-0000-0000-0000-000000000002, 'Validate Order', 'conditional', 0, '', '', true);

-- Filter 2.2: Python — Classify priority from OBR segment
INSERT INTO arteria.filters (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active, created_at)
VALUES (
    f2000000-0000-0000-0000-000000000002,
    30000000-0000-0000-0000-000000000002,
    'Classify Priority (Python)',
    'python', 1,
    $$import sys, json

envelope = json.loads(sys.stdin.read())
raw = envelope.get("rawPayload", "")
props = envelope.get("properties") or {}

# Check for STAT priority in OBR segment
priority = "routine"
for line in raw.replace("\n", "\r").split("\r"):
    if line.startswith("OBR|"):
        fields = line.split("|")
        if len(fields) > 27 and fields[27].upper() in ("S", "STAT"):
            priority = "stat"
        elif len(fields) > 27 and fields[27].upper() in ("A", "ASAP"):
            priority = "urgent"

props["order_priority"] = priority
props["processed_at"] = __import__("datetime").datetime.now().isoformat()
envelope["properties"] = props
json.dump(envelope, sys.stdout)$$,
    '', true, toTimestamp(now())
);
INSERT INTO arteria.filters_by_id (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active)
VALUES (f2000000-0000-0000-0000-000000000002, 30000000-0000-0000-0000-000000000002, 'Classify Priority (Python)', 'python', 1, '', '', true);

-- Route 3 Filters: Results Processing
-- Filter 3.1: Validate
INSERT INTO arteria.filters (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active, created_at)
VALUES (
    f3000000-0000-0000-0000-000000000001,
    30000000-0000-0000-0000-000000000003,
    'Validate Result',
    'conditional', 0,
    $$function evaluate(msg) {
  if (!msg.patientId) {
    return { action: "reject", reason: "Result missing Patient ID" };
  }
  return { action: "pass" };
}$$,
    '', true, toTimestamp(now())
);
INSERT INTO arteria.filters_by_id (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active)
VALUES (f3000000-0000-0000-0000-000000000001, 30000000-0000-0000-0000-000000000003, 'Validate Result', 'conditional', 0, '', '', true);

-- Filter 3.2: Python — Extract OBX values and flag critical results
INSERT INTO arteria.filters (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active, created_at)
VALUES (
    f3000000-0000-0000-0000-000000000002,
    30000000-0000-0000-0000-000000000003,
    'Extract Results & Flag Critical',
    'python', 1,
    $$import sys, json

envelope = json.loads(sys.stdin.read())
raw = envelope.get("rawPayload", "")
props = envelope.get("properties") or {}

results = []
is_critical = False
for line in raw.replace("\n", "\r").split("\r"):
    if line.startswith("OBX|"):
        fields = line.split("|")
        if len(fields) > 8:
            test_name = fields[3].split("^")[1] if "^" in fields[3] else fields[3]
            value = fields[5]
            units = fields[6] if len(fields) > 6 else ""
            abnormal = fields[8] if len(fields) > 8 else ""
            results.append({"test": test_name, "value": value, "units": units, "flag": abnormal})
            if abnormal in ("HH", "LL", "A", "AA"):
                is_critical = True

props["result_count"] = str(len(results))
props["is_critical"] = str(is_critical).lower()
props["processed_at"] = __import__("datetime").datetime.now().isoformat()
if results:
    props["first_result"] = results[0].get("test", "") + ": " + results[0].get("value", "")
envelope["properties"] = props
json.dump(envelope, sys.stdout)$$,
    '', true, toTimestamp(now())
);
INSERT INTO arteria.filters_by_id (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active)
VALUES (f3000000-0000-0000-0000-000000000002, 30000000-0000-0000-0000-000000000003, 'Extract Results & Flag Critical', 'python', 1, '', '', true);

-- Route 4 Filters: Critical Result Alerts (chained)
-- Filter 4.1: Only alert on critical results
INSERT INTO arteria.filters (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active, created_at)
VALUES (
    f4000000-0000-0000-0000-000000000001,
    30000000-0000-0000-0000-000000000004,
    'Check if Critical',
    'conditional', 0,
    $$function evaluate(msg) {
  if (msg.properties.is_critical === "true") {
    return { action: "pass" };
  }
  return { action: "reject", reason: "Non-critical result, no alert needed" };
}$$,
    '', true, toTimestamp(now())
);
INSERT INTO arteria.filters_by_id (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active)
VALUES (f4000000-0000-0000-0000-000000000001, 30000000-0000-0000-0000-000000000004, 'Check if Critical', 'conditional', 0, '', '', true);

-- Filter 4.2: Format notification payload
INSERT INTO arteria.filters (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active, created_at)
VALUES (
    f4000000-0000-0000-0000-000000000002,
    30000000-0000-0000-0000-000000000004,
    'Format Alert',
    'javascript', 1,
    $$function transform(msg) {
  msg.properties.alert_type = "CRITICAL_RESULT";
  msg.properties.alert_summary = "Critical result for patient " + msg.patientId + ": " + (msg.properties.first_result || "unknown");
  msg.properties.alert_time = new Date().toISOString();
  msg.properties.requires_ack = "true";
  return msg;
}$$,
    '', true, toTimestamp(now())
);
INSERT INTO arteria.filters_by_id (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active)
VALUES (f4000000-0000-0000-0000-000000000002, 30000000-0000-0000-0000-000000000004, 'Format Alert', 'javascript', 1, '', '', true);
