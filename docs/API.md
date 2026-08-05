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

**Request Body:** Same as create.

**Response:**
```json
{"status": "updated"}
```

### `DELETE /api/v1/comm-points/:id`

Delete a communication point.

**Response:**
```json
{"status": "deleted"}
```

---

## Routes

Routes connect a source communication point to a destination through a filter chain.

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
      "is_active": true
    }
  ],
  "count": 1
}
```

**Source topic matching:**
- `ADT^A01` — matches only ADT^A01 messages
- `*` — catch-all, matches any message type
- Most specific match wins; catch-all is a fallback

### `GET /api/v1/routes/:id`

Get a single route by ID.

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

Update a route. Same body as create.

### `DELETE /api/v1/routes/:id`

Delete a route.

---

## Filters

Filters are ordered processing steps within a route's filter chain. They execute in `execution_order` (ascending).

### `GET /api/v1/routes/:id/filters`

List all filters for a route, ordered by `execution_order`.

**Response:**
```json
{
  "filters": [
    {
      "filter_id": "c1000000-...",
      "name": "Validate Patient ID",
      "filter_type": "conditional",
      "execution_order": 0,
      "js_script": "function evaluate(msg) { ... }",
      "config_json": "",
      "is_active": true
    },
    {
      "filter_id": "c1000000-...",
      "name": "Enrich ADT Message",
      "filter_type": "javascript",
      "execution_order": 1,
      "js_script": "function transform(msg) { ... }",
      "config_json": "",
      "is_active": true
    }
  ],
  "count": 2
}
```

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

| Type | JS Entry Point | Description |
|------|---------------|-------------|
| `javascript` | `transform(msg)` | Modify the message and return it |
| `conditional` | `evaluate(msg)` | Return `{action: "pass"}`, `{action: "reject", reason: "..."}`, or `{action: "route_to", route_to: "dest"}` |
| `lookup` | — | Enriches message from a lookup table (configured via `config_json`) |

**Message object available in JS:**
```javascript
{
  messageId: "uuid",
  messageType: "ADT",
  triggerEvent: "A01",
  sendingFacility: "HOSP_A",
  patientId: "PAT001",
  rawPayload: "MSH|...",
  properties: {}       // Read/write key-value metadata
}
```

### `PUT /api/v1/filters/:id`

Update a filter by its ID.

### `DELETE /api/v1/routes/:routeId/filters/:order?order=N`

Delete a filter by route ID and execution order.

---

## Lookup Tables

Shared key-value data accessible from lookup filters.

### `GET /api/v1/lookups`

List all lookup tables.

**Response:**
```json
{
  "lookup_tables": [
    {"table_id": "uuid", "name": "facility_codes", "description": "Maps facility IDs to names"}
  ],
  "count": 1
}
```

### `POST /api/v1/lookups`

Create a lookup table.

**Request Body:**
```json
{"name": "facility_codes", "description": "Maps facility IDs to display names"}
```

### `GET /api/v1/lookups/:id/entries`

List all entries in a lookup table.

**Response:**
```json
{
  "entries": [
    {"key": "HOSP_A", "value": "City Hospital"},
    {"key": "LAB_1", "value": "Central Lab"}
  ],
  "count": 2
}
```

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

**Response:**
```json
{
  "messages": [
    {
      "message_id": "uuid",
      "patient_id": "PAT001",
      "message_type": "ADT",
      "trigger_event": "A01",
      "sending_facility": "HOSP_A",
      "status": "ROUTED",
      "created_at": "2026-08-04T08:18:49Z"
    }
  ],
  "count": 1
}
```

### `GET /api/v1/messages/:id`

Get full message detail including raw and transformed payloads.

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

**Message statuses:** `RECEIVED`, `FILTERING`, `FILTERED`, `ROUTED`, `DELIVERED`, `ERROR`, `DLQ`

### `GET /api/v1/messages/status/:status?limit=50`

List messages by status.

### `GET /api/v1/messages/patient/:patientId?limit=50`

List messages by patient ID.

---

## Errors / Dead Letter Queue

### `GET /api/v1/errors?limit=50`

List error records.

**Response:**
```json
{
  "errors": [
    {
      "message_id": "uuid",
      "error_type": "FILTER_ERROR",
      "error_details": "message rejected by filter: Missing Patient ID",
      "retry_count": 0,
      "max_retries": 3,
      "created_at": "2026-08-04T09:19:16Z"
    }
  ],
  "count": 1
}
```

**Error types:** `FILTER_ERROR`, `TIMEOUT`, `DELIVERY_FAILED`, `VALIDATION`

---

## Statistics

### `GET /api/v1/stats`

Aggregate counts from ScyllaDB.

**Response:**
```json
{
  "total_messages": 15,
  "total_routes": 2,
  "total_errors": 1
}
```

---

## Live Metrics

Real-time metrics from running services, collected via NATS request-reply.

### `GET /api/v1/metrics`

Returns live counters from ingestion and processing services.

**Response:**
```json
{
  "ingestion": {
    "received": 10,
    "processed": 10,
    "routed": 0,
    "errors": 0,
    "rejected": 0,
    "dlq": 0,
    "bytes_in": 810,
    "uptime_seconds": 120.5,
    "msgs_per_second": 2.0,
    "msgs_per_minute": 35.3,
    "comm_points": {
      "a1000000-...": {
        "id": "a1000000-...",
        "name": "Default MLLP Input",
        "direction": "INPUT",
        "received": 10,
        "sent": 0,
        "errors": 0,
        "bytes_in": 810,
        "bytes_out": 0,
        "last_seen": "2026-08-04T09:50:08Z"
      }
    }
  },
  "processing": {
    "received": 10,
    "processed": 10,
    "routed": 10,
    "errors": 0,
    "rejected": 0,
    "dlq": 0,
    "bytes_in": 810,
    "uptime_seconds": 119.2,
    "msgs_per_second": 2.0,
    "msgs_per_minute": 37.5
  }
}
```

### `GET /api/v1/metrics/comm-points`

Returns per-communication-point metrics with recent logs.

### `GET /api/v1/metrics/comm-points/:id/logs`

Returns full log buffer (up to 200 entries) for a specific communication point.

**Response:**
```json
{
  "comm_point_id": "a1000000-...",
  "name": "Default MLLP Input",
  "direction": "INPUT",
  "received": 10,
  "errors": 0,
  "log_count": 20,
  "logs": [
    {
      "timestamp": "2026-08-04T09:50:08.525Z",
      "level": "INFO",
      "message": "message received",
      "message_id": "25defbb6-...",
      "size_bytes": 81
    },
    {
      "timestamp": "2026-08-04T09:50:08.526Z",
      "level": "INFO",
      "message": "published to NATS",
      "message_id": "25defbb6-...",
      "size_bytes": 81
    }
  ]
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

**Response:**
```json
{"status": "updated", "level": "WARN"}
```

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

List all users.

### `POST /api/v1/users`

Create a user. **Request:** `{"username": "dev1", "password": "securepass123", "role": "developer"}`

### `PUT /api/v1/users/:id/role`

Update role or active status. **Request:** `{"role": "operator"}` or `{"is_active": false}`

### `DELETE /api/v1/users/:id`

Delete a user.

### `GET /api/v1/roles`

List all roles with their permissions.

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

View security audit log for a user. **Required role:** `admin` or `security`

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
