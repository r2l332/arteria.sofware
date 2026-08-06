# Arteria API Reference

**Base URL:** `http://localhost:8080/api/v1`

All requests and responses use `Content-Type: application/json`.

---

## Health

### `GET /health`

Returns service health status.

**Response:**
```json
{"status": "ok"}
```

---

## Communication Points

Communication points represent inbound/outbound endpoints for message transport.

### `GET /api/v1/comm-points`

List all communication points. When a CP is connected via the Aorta mesh, the response includes Capillary node details.

**Response:**
```json
{
  "communication_points": [
    {
      "comm_point_id": "a1000000-0000-0000-0000-000000000001",
      "name": "Default MLLP Input",
      "direction": "INPUT",
      "protocol": "MLLP",
      "host": "0.0.0.0",
      "port": 2575,
      "is_active": true,
      "max_retries": 0,
      "retry_delay_ms": 0,
      "timeout_ms": 30000,
      "tunnel_enabled": true,
      "tunnel_node_id": "node-uuid",
      "tunnel_local_port": 2575,
      "capillary_name": "Hospital A Agent",
      "capillary_site": "Hospital A",
      "capillary_status": "connected"
    }
  ],
  "count": 1
}
```

### `POST /api/v1/comm-points`

Create a communication point.

**Request Body:**
```json
{
  "name": "Lab Output",
  "direction": "OUTPUT",
  "protocol": "MLLP",
  "host": "lab-system.local",
  "port": 2575,
  "is_active": true,
  "max_retries": 3,
  "retry_delay_ms": 2000,
  "timeout_ms": 30000
}
```

**Response:** `201 Created`
```json
{"comm_point_id": "uuid"}
```

### `PUT /api/v1/comm-points/:id`

Update a communication point.

### `DELETE /api/v1/comm-points/:id`

Delete a communication point.

---

## Routes

Routes connect a source communication point to a destination through a filter chain. Routes support default properties, fan-out delivery to multiple outputs, conditional fan-out, and chaining to other routes.

### `GET /api/v1/routes`

List all routes.

**Response:**
```json
{
  "routes": [
    {
      "route_id": "b1000000-0000-0000-0000-000000000001",
      "name": "ADT Admissions Route",
      "description": "Routes ADT^A01 messages through a sample transform",
      "source_comm_point_id": "a1000000-...",
      "dest_comm_point_id": "a1000000-...",
      "source_topic": "ADT^A01",
      "destination_topic": "admissions",
      "is_active": true,
      "default_properties": {"environment": "production", "source": "hospital_a"},
      "next_route_id": "b2000000-..."
    }
  ],
  "count": 1
}
```

**Source topic matching:**
- `ADT^A01` — matches only ADT admit messages
- `ORM^O01` — matches only ORM order messages
- `*` — catch-all, matches any message type
- Most specific match wins; catch-all is a fallback

### `GET /api/v1/routes/:id`

Get a single route by ID. Includes `default_properties` and `next_route_id` if set.

### `POST /api/v1/routes`

Create a route.

**Request Body:**
```json
{
  "name": "Lab Orders Route",
  "description": "Routes ORM messages to the lab",
  "source_comm_point_id": "uuid",
  "dest_comm_point_id": "uuid",
  "source_topic": "ORM^O01",
  "destination_topic": "lab_orders",
  "is_active": true
}
```

**Response:** `201 Created`
```json
{"route_id": "uuid"}
```

### `PUT /api/v1/routes/:id`

Update a route. Supports `fan_out_cp_ids` for simple fan-out delivery.

**Request Body:**
```json
{
  "name": "Lab Orders Route",
  "description": "Routes ORM messages to the lab",
  "source_comm_point_id": "uuid",
  "dest_comm_point_id": "uuid",
  "fan_out_cp_ids": ["uuid-1", "uuid-2"],
  "source_topic": "ORM^O01",
  "destination_topic": "lab_orders",
  "is_active": true
}
```

### `DELETE /api/v1/routes/:id`

Delete a route.

### Route Properties

#### `PUT /api/v1/routes/:id/properties`

Set default properties on a route. These key-value pairs are injected into every message's `properties` map before the filter chain runs. Filters can read, modify, and add properties — they persist through the entire chain and are stored in the database.

**Request Body:**
```json
{
  "properties": {
    "environment": "production",
    "source_system": "hospital_a",
    "compliance_level": "hipaa"
  }
}
```

**Response:**
```json
{"status": "updated", "properties": {"environment": "production", "source_system": "hospital_a", "compliance_level": "hipaa"}}
```

#### `GET /api/v1/routes/:id/properties`

Get the default properties for a route.

**Response:**
```json
{"properties": {"environment": "production", "source_system": "hospital_a"}}
```

### Route Chaining

Routes can be chained so that after one route's filter chain completes, the message is forwarded to another route's filter chain. Maximum chain depth is 10 to prevent infinite loops.

#### `PUT /api/v1/routes/:id/chain`

Set the next route in a chain.

**Request Body:**
```json
{"next_route_id": "uuid-of-next-route"}
```

To clear the chain:
```json
{"next_route_id": null}
```

**Response:**
```json
{"status": "chained", "next_route_id": "uuid"}
```

#### `GET /api/v1/routes/:id/chain`

Get the chain configuration.

**Response:**
```json
{"next_route_id": "uuid", "next_route_name": "Post-Processing Route"}
```

### Fan-Out

#### `PUT /api/v1/routes/:id/fan-out`

Configure conditional fan-out. Each entry defines an output CP that receives the message, optionally gated by a condition script.

**Request Body:**
```json
{
  "entries": [
    {"cp_id": "uuid-1", "name": "Lab System", "condition_type": "", "condition": ""},
    {"cp_id": "uuid-2", "name": "Radiology", "condition_type": "python", "condition": "import sys,json; e=json.loads(sys.stdin.read()); print('true' if 'RAD' in e.get('rawPayload','') else 'false')"},
    {"cp_id": "uuid-3", "name": "ENT", "condition_type": "javascript", "condition": "const e=JSON.parse(require('fs').readFileSync('/dev/stdin','utf8')); console.log(e.rawPayload.includes('ENT'))"}
  ]
}
```

**Condition scripts:** Read the message envelope JSON from stdin, print `true`/`false`/`1`/`yes` to stdout. Supported types: `python`, `javascript`, `bash`, `dotnet`. Empty condition = unconditional.

**Response:**
```json
{"status": "configured", "entries": 3}
```

#### `GET /api/v1/routes/:id/fan-out`

Get the conditional fan-out configuration.

### Route Rewire

#### `PATCH /api/v1/routes/:id/rewire`

Quick rewire a route's source or destination CP.

**Request Body:**
```json
{"source_comm_point_id": "new-uuid", "dest_comm_point_id": "new-uuid"}
```

---

## Filters

Filters are ordered processing steps within a route's filter chain. They execute in `execution_order` (ascending).

### `GET /api/v1/routes/:id/filters`

List all filters for a route, ordered by `execution_order`.

### `POST /api/v1/routes/:id/filters`

Create a filter on a route.

**Request Body:**
```json
{
  "name": "Strip SSN",
  "filter_type": "javascript",
  "execution_order": 2,
  "js_script": "function transform(msg) { delete msg.properties.ssn; return msg; }",
  "config_json": "",
  "is_active": true
}
```

**Filter Types:**

| Type | Entry Point | Description |
|------|------------|-------------|
| `javascript` | `transform(msg)` in V8 | Modify the message and return it. Native V8 speed (~1ms). |
| `conditional` | `evaluate(msg)` in V8 | Return `{action: "pass"}`, `{action: "reject", reason: "..."}`, or `{action: "route_to", route_to: "dest"}` |
| `lookup` | — | Enriches message from a lookup table (configured via `config_json`) |
| `python` | Script via stdin/stdout | Full Python3 — reads envelope JSON from stdin, writes modified envelope to stdout |
| `bash` | Script via stdin/stdout | Bash script — same stdin/stdout contract |
| `dotnet` | `dotnet-script eval` | C# script via dotnet-script — same stdin/stdout contract. 10s timeout for JIT. |
| `connector` | — | Makes an outbound HTTP or MLLP call mid-chain and stores the response in properties (see below) |

**Timeouts:** JavaScript runs in V8 with 50ms limit. Python/bash have 2s limit. Dotnet has 10s limit (JIT cold-start).

**Message object (available in all filter types as JSON):**
```json
{
  "messageId": "uuid",
  "messageType": "ADT",
  "triggerEvent": "A01",
  "sendingFacility": "HOSP_A",
  "patientId": "PAT001",
  "rawPayload": "MSH|...",
  "properties": {
    "environment": "production",
    "my_custom_var": "value"
  }
}
```

**Properties** are the primary mechanism for passing data between filters. Route default properties are injected first, then each filter can read/write them freely. They persist through the entire chain and are stored in the database audit trail.

### Connector Filter

The `connector` filter type makes an outbound call (HTTP or MLLP) mid-filter-chain and stores the response in message properties. This enables patterns like: receive HL7 → call REST API for enrichment data → continue processing with the API response.

**`config_json` for HTTP connector:**
```json
{
  "connector_type": "HTTP",
  "url": "https://api.example.com/patient/lookup",
  "method": "POST",
  "headers": {"Content-Type": "application/json", "Authorization": "Bearer token"},
  "timeout_ms": 5000,
  "body_template": "{{.RawPayload}}",
  "response_property": "api_response",
  "response_status_property": "api_status"
}
```

**`config_json` for MLLP connector:**
```json
{
  "connector_type": "MLLP",
  "host": "downstream-system.local",
  "port": 2575,
  "timeout_ms": 5000,
  "response_property": "ack_message",
  "response_status_property": "ack_code"
}
```

After execution, subsequent filters can read `msg.properties.api_response`, `msg.properties.ack_code`, etc. The MLLP connector automatically extracts the ACK code from the MSA segment (AA/AE/AR).

If the outbound call fails, `response_status_property` is set to `"ERROR"` and `_connector_error` contains the error message. The filter chain continues — use a subsequent conditional filter to branch on errors.

**Example: Send HL7 → Check ACK → Route failures to email**

Route filter chain:
1. `connector` filter — sends to downstream MLLP, stores `ack_code` and `ack_message`
2. `conditional` filter — if `msg.properties.ack_code !== "AA"` → `{action: "route_to", route_to: "error_email"}`
3. On success, message continues to the route's normal destination

### `PUT /api/v1/filters/:id`

Update a filter by its ID.

### `DELETE /api/v1/routes/:routeId/filters/:order`

Delete a filter by route ID and execution order.

### `PUT /api/v1/routes/:id/filters/reorder`

Reorder filters within a route.

**Request Body:**
```json
{
  "order": [
    {"filter_id": "uuid-1", "position": 0},
    {"filter_id": "uuid-2", "position": 1},
    {"filter_id": "uuid-3", "position": 2}
  ]
}
```

### `POST /api/v1/filters/:id/move`

Move a filter from one route to another.

**Request Body:**
```json
{"from_route_id": "uuid", "to_route_id": "uuid", "position": 0}
```

### `POST /api/v1/filters/:id/test`

Test a filter with a specific payload (live debugging).

**Request Body:**
```json
{"payload": "{\"messageId\":\"test\",\"messageType\":\"ADT\",\"triggerEvent\":\"A01\",\"sendingFacility\":\"HOSP\",\"patientId\":\"PAT001\",\"rawPayload\":\"MSH|...\",\"properties\":{}}"}
```

---

## Lookup Tables

Shared key-value data accessible from lookup filters.

### `GET /api/v1/lookups`

List all lookup tables.

### `POST /api/v1/lookups`

Create a lookup table.

**Request Body:**
```json
{"name": "facility_codes", "description": "Maps facility IDs to display names"}
```

### `GET /api/v1/lookups/:id/entries`

List all entries in a lookup table.

### `PUT /api/v1/lookups/:id/entries`

Upsert a lookup entry.

**Request Body:**
```json
{"key": "HOSP_A", "value": "City Hospital"}
```

---

## Messages

### `GET /api/v1/messages?limit=50`

List recent messages. Maximum `limit=200`.

### `GET /api/v1/messages/:id`

Get full message detail including raw and transformed payloads, properties, and route metadata.

**Response:**
```json
{
  "message_id": "uuid",
  "patient_id": "PAT001",
  "message_type": "ADT",
  "trigger_event": "A01",
  "sending_facility": "HOSP_A",
  "raw_payload": "MSH|^~\\&|...",
  "transformed_payload": "{...}",
  "properties": "{}",
  "status": "ROUTED",
  "error_details": "",
  "created_at": "2026-08-04T08:18:49Z",
  "updated_at": "2026-08-04T08:18:49Z",
  "retry_count": 0
}
```

**Message statuses:** `RECEIVED`, `ROUTED`, `DELIVERED`, `ERROR`, `DLQ`, `DROPPED`, `HELD`, `RETRYING`

### `GET /api/v1/messages/status/:status?limit=50`

List messages by status.

### `GET /api/v1/messages/patient/:patientId?limit=50`

List messages by patient ID.

### Message Control

#### `POST /api/v1/messages/:id/drop`

Drop a message (mark as DROPPED, remove from queue).

**Request Body:**
```json
{"reason": "Duplicate message"}
```

#### `POST /api/v1/messages/:id/retry`

Re-inject a failed message back into the ingestion stream.

#### `POST /api/v1/messages/:id/hold`

Hold a message (pause delivery, mark as HELD).

#### `POST /api/v1/messages/:id/release`

Release a held message back into processing.

### Message Trace

#### `GET /api/v1/messages/:id/trace`

Get the full journey trace of a message through the pipeline.

**Response:**
```json
{
  "message_id": "uuid",
  "status": "DELIVERED",
  "steps": [
    {"stage": "received", "timestamp": "...", "component": "Ingestion", "input": "MSH|..."},
    {"stage": "processed", "timestamp": "...", "component": "Processing Engine", "duration_ms": 12},
    {"stage": "routed", "timestamp": "...", "component": "Router"},
    {"stage": "delivered", "timestamp": "...", "component": "Egress"}
  ]
}
```

### Route Flush

#### `POST /api/v1/routes/:id/flush`

Purge all queued messages for a route.

**Request Body:**
```json
{"reason": "Clearing stale messages after config change"}
```

### Route Recent Messages

#### `GET /api/v1/routes/:id/recent?limit=10`

Get the last N messages that passed through a specific route.

---

## Errors / Dead Letter Queue

### `GET /api/v1/errors?limit=50`

List error records.

**Error types:** `FILTER_ERROR`, `TIMEOUT`, `DELIVERY_FAILED`, `VALIDATION`

---

## Statistics

### `GET /api/v1/stats`

Aggregate counts from ScyllaDB.

---

## Live Metrics

Real-time metrics from running services, collected via NATS request-reply.

### `GET /api/v1/metrics`

Returns live counters from ingestion and processing services.

### `GET /api/v1/metrics/comm-points`

Returns per-communication-point metrics with recent logs.

### `GET /api/v1/metrics/comm-points/:id/logs`

Returns full log buffer (up to 200 entries) for a specific communication point.

---

## WebSocket Live Streaming

### `WS /ws/flow`

WebSocket endpoint for real-time message flow events. Streams all pipeline events as JSON:

**Event types:**
- `message` — message received/routed/delivered/dropped
- `error` — processing error
- `metric` — live metric snapshot (every 1s)
- `trace` — message trace event

**Event format:**
```json
{
  "type": "message",
  "timestamp": "2026-08-06T10:30:00.000Z",
  "data": {
    "message_id": "uuid",
    "message_type": "ADT",
    "trigger_event": "A01",
    "patient_id": "PAT001",
    "status": "ROUTED",
    "stage": "routed",
    "route_name": "admissions"
  }
}
```

---

## Configuration

### `GET /api/v1/config/log-level`

Get the current API service log level.

### `PUT /api/v1/config/log-level`

Change log level at runtime (no restart required).

**Request Body:**
```json
{"level": "WARN"}
```

**Valid levels:** `TRACE`, `DEBUG`, `INFO`, `WARN`, `ERROR`, `FATAL`

### `GET /api/v1/config/modules`

List enabled modules (HL7, FHIR, DICOM, Tunnel).

---

## Aorta Mesh

### `GET /api/v1/tunnel/nodes`

List all Capillary nodes.

### `POST /api/v1/tunnel/nodes`

Create a Capillary node. Returns enrollment token.

**Request:** `{"name": "Hospital A", "site_name": "Hospital A"}`

**Response:** `201` `{"node_id": "uuid", "enrollment_token": "uuid", "instructions": "Run: capillary enroll <token>"}`

### `DELETE /api/v1/tunnel/nodes/:id`

Delete a Capillary node.

### `POST /api/v1/tunnel/nodes/:id/push-config`

Push updated CP configuration to a connected agent.

---

## JS Filter Playground

### `POST /api/v1/playground/execute`

Execute a JS filter against a test payload in a real V8 isolate.

**Request:**
```json
{
  "script": "function transform(msg) { msg.properties.test = true; return msg; }",
  "filter_type": "javascript",
  "payload": "{\"messageId\":\"test\",\"messageType\":\"ADT\",\"triggerEvent\":\"A01\",\"sendingFacility\":\"HOSP\",\"patientId\":\"PAT001\",\"rawPayload\":\"MSH|...\",\"properties\":{}}"
}
```

**Response:** `{"success": true, "output": "{...transformed JSON...}"}`

---

## User Management

**Required role:** `admin` or `security`

### `GET /api/v1/users`
### `POST /api/v1/users`

Create a user. **Request:** `{"username": "dev1", "password": "securepass123", "role": "developer"}`

### `PUT /api/v1/users/:id/role`

Update role or active status. **Request:** `{"role": "operator"}` or `{"is_active": false}`

### `DELETE /api/v1/users/:id`
### `GET /api/v1/roles`

List all roles with their permissions.

### `GET /api/v1/users/online`

List currently active sessions.

---

## Internal Messaging

### `GET /api/v1/messages/internal/inbox`
### `GET /api/v1/messages/internal/sent`
### `POST /api/v1/messages/internal`
### `PUT /api/v1/messages/internal/:id/read`
### `GET /api/v1/messages/internal/unread-count`

---

## Organisations

### `GET /api/v1/organisations`
### `POST /api/v1/organisations`
### `GET /api/v1/organisations/:id`
### `PUT /api/v1/organisations/:id/branding`

---

## Connector Types

### `GET /api/v1/connector-types`

Returns all supported CP connector types grouped by category.

**Response:**
```json
{
  "connector_types": {
    "traditional": ["MLLP", "TCP", "HTTP", "REST"],
    "storage": ["S3", "AZURE_BLOB"],
    "eventing": ["SQS", "SNS", "AZURE_EVENT_HUB", "AZURE_SERVICE_BUS"],
    "http": ["WEBHOOK"]
  }
}
```

---

## Audit Log

### `GET /api/v1/audit-log?username=admin&limit=50`

View security audit log. **Required role:** `admin` or `security`

---

## Config Backup & Restore

### `GET /api/v1/config/export`

Export all configuration as JSON (CPs, routes, filters, lookups, Capillary nodes).

### `POST /api/v1/config/import`

Import configuration from a JSON backup.

### `GET /api/v1/config/backups`

List saved config backups.

### `POST /api/v1/config/backups`

Create a named backup. **Request:** `{"name": "Before upgrade", "description": "Pre-v2 snapshot"}`

### `GET /api/v1/config/history?type=route`

View config change history. Types: `route`, `filter`, `comm_point`, `tunnel_node`, `lookup`

### `GET /api/v1/config/retention`

Get current message retention TTL settings.

### `PUT /api/v1/config/retention`

Update retention policy. **Request:** `{"messages_ttl_days": 30, "error_messages_ttl_days": 90}`

---

## Platform Administration

### `GET /api/v1/platform/health`

Full system health check (ScyllaDB, NATS, services).

### `GET /api/v1/platform/tunnel-stats`

Aorta tunnel statistics.

### `GET /api/v1/platform/usage`

Platform usage metrics.

### `GET /api/v1/platform/logs/:service`

View logs for a specific service.

### `GET /api/v1/platform/nats-stats`

NATS JetStream statistics.

### `GET /api/v1/platform/connections`

Connection history.

---

## USP Features

### Patient Journey

#### `GET /api/v1/patients/:id/journey`

Get the complete message timeline for a patient by MRN.

### AI Filter Generator

#### `POST /api/v1/ai/generate-filter`

Generate filter code from an English description.

**Request Body:**
```json
{"description": "reject messages without a patient ID", "language": "python"}
```

### Compliance Timeline

#### `GET /api/v1/compliance/timeline?hours=24`

View compliance events over a time window.
