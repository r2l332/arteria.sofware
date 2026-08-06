-- ============================================================
-- Capillary E2E Demo — Seed Data
-- Sets up the full round-trip flow:
--   VM → Capillary → Input CP → Route (Python filters) → Output CP → Capillary → VM
-- ============================================================

-- Tunnel Node representing the VM
INSERT INTO arteria.tunnel_nodes (node_id, name, site_name, enrollment_token, status, agent_version, created_at, updated_at)
VALUES (
    a1b2c3d4-e5f6-7890-abcd-ef1234567890,
    'demo-vm',
    'Demo VM (Capillary E2E)',
    'demo-token-e2e-2024',
    'PENDING',
    '0.1.0',
    toTimestamp(now()),
    toTimestamp(now())
);

-- Input Communication Point: receives HL7 from VM through Capillary
INSERT INTO arteria.communication_points (comm_point_id, name, direction, protocol, host, port, is_active, tunnel_enabled, tunnel_node_id, tunnel_local_port, config_json, created_at, updated_at)
VALUES (
    11111111-1111-1111-1111-111111111111,
    'Demo VM Input (MLLP via Capillary)',
    'INPUT',
    'MLLP',
    '0.0.0.0',
    2575,
    true,
    true,
    a1b2c3d4-e5f6-7890-abcd-ef1234567890,
    2575,
    '{"description": "Receives HL7 ADT messages from VM via Capillary tunnel"}',
    toTimestamp(now()),
    toTimestamp(now())
);

-- Output Communication Point: sends extracted data back to VM through Capillary
INSERT INTO arteria.communication_points (comm_point_id, name, direction, protocol, host, port, is_active, tunnel_enabled, tunnel_node_id, tunnel_local_port, config_json, created_at, updated_at)
VALUES (
    22222222-2222-2222-2222-222222222222,
    'Demo VM Output (MLLP via Capillary)',
    'OUTPUT',
    'MLLP',
    '0.0.0.0',
    2576,
    true,
    true,
    a1b2c3d4-e5f6-7890-abcd-ef1234567890,
    2576,
    '{"description": "Sends patient extract JSON back to VM via Capillary tunnel"}',
    toTimestamp(now()),
    toTimestamp(now())
);

-- Route: Input CP → Python Filters → Output CP
INSERT INTO arteria.routes (route_id, name, description, source_comm_point_id, dest_comm_point_id, source_topic, destination_topic, is_active, created_at, updated_at)
VALUES (
    33333333-3333-3333-3333-333333333333,
    'Demo Capillary Round-Trip',
    'Receives HL7 from VM, transforms to JSON via Python, extracts patient data, sends back to VM',
    11111111-1111-1111-1111-111111111111,
    22222222-2222-2222-2222-222222222222,
    'ADT^A04',
    'demo.output',
    true,
    toTimestamp(now()),
    toTimestamp(now())
);

-- Filter 1: Python — HL7 to JSON Transform
INSERT INTO arteria.filters (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active, created_at)
VALUES (
    44444444-4444-4444-4444-444444444444,
    33333333-3333-3333-3333-333333333333,
    'HL7 to JSON (Python)',
    'python',
    1,
    $$import sys, json

def parse_hl7(raw):
    for sep in [chr(13), chr(10), '\r']:
        segs = raw.split(sep)
        if len(segs) > 1:
            break
    else:
        segs = [raw]
    result = {"segments": []}
    for s in segs:
        if not s.strip():
            continue
        fields = s.split("|")
        seg = {"type": fields[0], "fields": fields[1:]}
        if fields[0] == "PID" and len(fields) > 5:
            name = fields[5].split("^")
            seg["patient_name"] = {"family": name[0], "given": name[1] if len(name) > 1 else ""}
            seg["patient_id"] = fields[3] if len(fields) > 3 else ""
        result["segments"].append(seg)
    return result

envelope = json.loads(sys.stdin.read())
envelope["rawPayload"] = json.dumps(parse_hl7(envelope.get("rawPayload", "")))
envelope["properties"] = envelope.get("properties") or {}
envelope["properties"]["transform"] = "hl7_to_json"
json.dump(envelope, sys.stdout)$$,
    '{"description": "Parses HL7 message and converts to structured JSON"}',
    true,
    toTimestamp(now())
);

-- Filter 2: Python — Extract Patient Name and ID
INSERT INTO arteria.filters (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active, created_at)
VALUES (
    55555555-5555-5555-5555-555555555555,
    33333333-3333-3333-3333-333333333333,
    'Extract Patient (Python)',
    'python',
    2,
    $$import sys, json

envelope = json.loads(sys.stdin.read())
try:
    parsed = json.loads(envelope.get("rawPayload", "{}"))
except:
    parsed = {"segments": []}

patient_id = ""
patient_name = ""
for seg in parsed.get("segments", []):
    if seg.get("type") == "PID":
        patient_id = seg.get("patient_id", "")
        n = seg.get("patient_name", {})
        if isinstance(n, dict):
            patient_name = (n.get("given", "") + " " + n.get("family", "")).strip()
        break

envelope["patientId"] = patient_id
envelope["rawPayload"] = json.dumps({"patient_id": patient_id, "patient_name": patient_name})
envelope["properties"] = {"patient_id": patient_id, "patient_name": patient_name, "transform": "patient_extract"}
json.dump(envelope, sys.stdout)$$,
    '{"description": "Extracts patient name and ID from parsed JSON, outputs compact summary"}',
    true,
    toTimestamp(now())
);
