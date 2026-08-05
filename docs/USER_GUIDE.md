# Arteria Console User Guide

This guide walks through every feature of the Arteria dashboard console at `http://localhost:3000`.

---

## 1. Dashboard (Home)

**URL:** `/`

The dashboard is your operational overview. It auto-refreshes every 3 seconds.

### Stats Bar

Three summary cards at the top:

| Card | What it shows |
|------|---------------|
| **Total Messages** | Count of all messages ever processed (from ScyllaDB) |
| **Active Routes** | Number of configured routes |
| **Errors** | Total error count |

### Live Throughput

Two panels showing real-time counters from the running services:

**Ingestion Service:**
- **Received** — messages accepted on the MLLP port
- **Published** — messages successfully pushed to NATS
- **msgs/min** — rolling throughput rate
- Footer shows total KB ingested and uptime

**Processing Service:**
- **Received** — messages pulled from NATS
- **Routed** — messages that passed the filter chain and were routed
- **Errors** — messages that failed processing
- **msgs/min** — rolling throughput rate
- Footer shows rejected count and DLQ count

### Recent Messages

A table of the last 10 messages showing type, patient ID, facility, status badge, and timestamp.

### Recent Errors

Appears only when errors exist. Shows a compact list of the latest failures with error type and detail.

---

## 2. Messages

**URL:** `/messages`

### Message Log Table

Displays the last 100 messages with columns:

| Column | Description |
|--------|-------------|
| **ID** | First 8 chars of the UUID (click row for full detail) |
| **Type** | HL7 message type and trigger event (e.g., `ADT^A01`) |
| **Patient** | Patient ID extracted from PID-3 |
| **Facility** | Sending facility from MSH-4 |
| **Status** | Color-coded badge: RECEIVED, ROUTED, DELIVERED, ERROR, DLQ |
| **Time** | When the message was received |

### Message Detail Modal

Click any row to open the detail view:

- **Metadata grid** — message ID, status, type, patient, facility, retry count
- **Error section** (if applicable) — red-highlighted error details
- **Raw Payload** — the original HL7 message exactly as received (green text)
- **Transformed Payload** — the output after V8 filter chain processing (cyan text)

Click outside the modal or press ✕ to close.

---

## 3. Routes

**URL:** `/routes`

This is the core configuration screen — the Route Editor. The page has three panels.

### Left Panel — Route List

Lists all configured routes with:
- Green/grey dot indicating active/inactive state
- Source topic → Destination topic
- **Edit** and **Delete** links per route

**Creating a new route:**

1. Click **+ New** at the top of the route list
2. Fill in the form:
   - **Name** — descriptive name (e.g., "ADT Admissions Route")
   - **Description** — what this route does
   - **Source Comm Point** — dropdown of INPUT communication points
   - **Dest Comm Point** — dropdown of OUTPUT communication points
   - **Source Topic** — the HL7 message pattern to match:
     - `ADT^A01` — matches only ADT admit messages
     - `ORM^O01` — matches only ORM order messages
     - `*` — catch-all for any unmatched message type
   - **Destination Topic** — where to route (e.g., `admissions`, `lab_orders`)
   - **Active** — toggle on/off
3. Click **Create**

**Editing a route:**

Click **Edit** on any route in the list. The same form opens pre-filled.

**Deleting a route:**

Click **Delete** and confirm the dialog.

### Center Panel — Filter Chain

When you select a route, the filter chain is displayed as a horizontal flow:

```
[#0 Validate Patient ID (conditional)] → [#1 Enrich ADT Message (javascript)]
```

Each filter is a clickable pill. The number indicates execution order — filters run in ascending order.

- Greyed-out filters are inactive (disabled but not deleted)
- Click a filter to edit it in the code editor
- Click **+ Add Filter** to create a new filter step

### Right Panel — Code Editor (Monaco)

When editing a filter, the Monaco code editor appears with:

**Toolbar:**
- **Filter name** — editable text field
- **Filter type** dropdown:
  - **JavaScript Transform** — modifies the message
  - **Conditional Router** — makes routing decisions
  - **Lookup Enrichment** — enriches from a lookup table
- **Active** checkbox — enable/disable this filter step
- **Cancel** and **Save Filter** buttons

**Writing a JavaScript Transform:**

The `transform(msg)` function receives the message object and must return it:

```javascript
function transform(msg) {
  // Add a timestamp
  msg.properties.processed_at = new Date().toISOString();

  // Add derived data
  msg.properties.facility_code = msg.sendingFacility.substring(0, 3);

  // You have full access to:
  // msg.messageId, msg.messageType, msg.triggerEvent
  // msg.sendingFacility, msg.patientId, msg.rawPayload
  // msg.properties (read/write key-value map)

  return msg;
}
```

**Writing a Conditional Router:**

The `evaluate(msg)` function returns a routing decision:

```javascript
function evaluate(msg) {
  // Reject messages without a patient ID
  if (!msg.patientId || msg.patientId === "") {
    return { action: "reject", reason: "Missing Patient ID" };
  }

  // Route lab messages to a different destination
  if (msg.sendingFacility.startsWith("LAB")) {
    return { action: "route_to", route_to: "lab_results" };
  }

  // Pass through to the next filter
  return { action: "pass" };
}
```

**Possible return values:**

| Action | Effect |
|--------|--------|
| `{ action: "pass" }` | Continue to next filter in the chain |
| `{ action: "reject", reason: "..." }` | Stop processing, send to DLQ with error |
| `{ action: "route_to", route_to: "dest" }` | Override the route destination |

**Filter execution order:**

Filters run sequentially by `execution_order`. If a conditional filter rejects, the chain stops and the message goes to the error queue. If it passes, the next filter runs. JS transforms modify the message in place for subsequent filters.

**Timeout:** Each filter has a 50ms execution limit. If your script takes longer, the message is rejected with a timeout error.

---

## 4. Communication Points

**URL:** `/comm-points`

Communication points define how Arteria connects to external systems.

### Left Panel — CP List

Shows all configured CPs as cards with:
- Active status indicator (green dot = running, grey = stopped)
- Direction badge: **INPUT** (blue) or **OUTPUT** (purple)
- Protocol, address (host:port), retry config
- **Edit** and **Del** actions

**Creating a new communication point:**

1. Click **+ New Comm Point**
2. Fill in the form:
   - **Name** — descriptive name (e.g., "Lab MLLP Output")
   - **Direction** — INPUT (receives messages) or OUTPUT (sends messages)
   - **Protocol** — MLLP, HTTP, TCP, REST, or cloud connectors (S3, Azure Blob, SQS, SNS, Event Hub, Service Bus, Webhook)
   - **Host** — hostname or IP (use `0.0.0.0` for INPUT to listen on all interfaces)
   - **Port** — TCP port number
   - **Max Retries** — how many times to retry on failure (OUTPUT only)
   - **Retry Delay (ms)** — wait between retries
   - **Timeout (ms)** — connection/send timeout
   - **Active** — enable/disable
   - **Route via Capillary** — enable to route traffic through an encrypted Aorta tunnel
     - Select the Capillary node (remote agent)
     - Set the local port at the remote site
3. Click **Create**

### Capillary Connection Indicator

Each CP card in the list shows which Capillary agent it is connected through:
- **⛓ Node Name (Site Name)** — with a green dot when the Capillary is connected
- **⛓ Node Name (Site Name)** — with a yellow dot when the Capillary is disconnected/enrolling

### Right Panel — Live CP Log Viewer

Click any communication point to see its live log stream.

**Log display:**
- Each entry shows: timestamp, log level, message, message ID, size
- Color-coded: ERROR (red), WARN (yellow), DEBUG (cyan), INFO (green)
- **Auto-refresh** checkbox — when enabled, logs update every 2 seconds

**Header stats:**
- Total received count
- Error count
- Number of log entries in the buffer (up to 200)

This lets you watch messages flowing through a specific communication point in real time, identify failures, and correlate by message ID.

---

## 5. Errors / DLQ

**URL:** `/errors`

The dead letter queue shows all messages that failed processing.

### Error Table

| Column | Description |
|--------|-------------|
| **Message ID** | First 8 chars of the UUID |
| **Error Type** | Category: `FILTER_ERROR`, `TIMEOUT`, `DELIVERY_FAILED`, `VALIDATION` |
| **Details** | The error message (truncated) |
| **Retries** | Current retry count / max retries |
| **Time** | When the error occurred |

When no errors exist, the page shows: "No errors — all systems operational"

---

## 6. Common Workflows

### Setting Up a New Integration

1. **Create an INPUT Communication Point** — defines where Hospital A sends messages
   - Protocol: MLLP, Port: 2575 (or custom)

2. **Create an OUTPUT Communication Point** — defines where to deliver processed messages
   - Protocol: MLLP or HTTP, Host: target system address, Port: target port

3. **Create a Route** — connects the input to the output
   - Source Comm Point: your INPUT CP
   - Dest Comm Point: your OUTPUT CP
   - Source Topic: the HL7 message type to match (e.g., `ADT^A01` or `*`)
   - Destination Topic: a logical name for the route

4. **Add Filters** (optional) — click the route and add processing steps
   - Add a conditional filter to validate messages
   - Add a JS transform to modify or enrich messages
   - Arrange execution order (lower numbers run first)

5. **Monitor** — go to the Dashboard to watch throughput, or click the CP to see its live log

### Debugging a Failed Message

1. Go to **Errors / DLQ** — find the failed message
2. Note the **Message ID** and **Error Details**
3. Go to **Messages** — search for the message by scanning the list
4. Click the message to see the **Raw Payload** (what was received)
5. Check the error: was it a filter rejection? A timeout? A validation failure?
6. Go to **Routes** — open the route and check the filter chain
7. Edit the filter script if needed, click **Save Filter**
8. The processing service reloads routes every 30 seconds — or restart it to apply immediately

### Monitoring Throughput

1. Go to **Dashboard** — the live metrics panels update every 3 seconds
2. Check **msgs/min** on both ingestion and processing
3. If processing is slower than ingestion, messages are queuing in NATS
4. Click a **Comm Point** to see per-CP received/error counts and live logs

### Changing Log Verbosity at Runtime

You can reduce log noise without restarting:

1. Use the API (there is no UI toggle yet):
   ```bash
   # Set to WARN (reduces TRACE/DEBUG/INFO output)
   curl -X PUT http://localhost:8080/api/v1/config/log-level \
     -H 'Content-Type: application/json' -d '{"level":"WARN"}'

   # Set back to TRACE for debugging
   curl -X PUT http://localhost:8080/api/v1/config/log-level \
     -H 'Content-Type: application/json' -d '{"level":"TRACE"}'
   ```

---

## 7. Keyboard & Interaction Reference

| Action | How |
|--------|-----|
| Navigate pages | Click sidebar items |
| View message detail | Click any row in the Messages table |
| Close a modal | Click ✕ or click outside the modal |
| Edit a filter | Click the filter pill in the chain, or click + Add Filter |
| Save a filter | Click Save Filter (editor toolbar) |
| View CP logs | Click a communication point card |
| Toggle auto-refresh | Check/uncheck the Auto-refresh box on CP logs |
| Create new items | Click the + button on the relevant page |

---

## 8. Message Object Reference (for JS Filters)

When writing JavaScript filters, the message object has these fields:

```javascript
{
  messageId: "uuid-string",           // Unique message identifier
  messageType: "ADT",                 // HL7 message type (MSH-9.1)
  triggerEvent: "A01",                // HL7 trigger event (MSH-9.2)
  sendingFacility: "HOSPITAL_A",      // Sending facility (MSH-4)
  patientId: "MRN12345",             // Patient identifier (PID-3)
  rawPayload: "MSH|^~\\&|...",       // Original HL7 message text
  properties: {                       // Read/write metadata map
    // Add any key-value pairs here
    // These persist through the filter chain
    // and are stored in ScyllaDB
  }
}
```

**Rules:**
- `transform(msg)` must return the modified message object
- `evaluate(msg)` must return `{ action: "pass" }`, `{ action: "reject", reason: "..." }`, or `{ action: "route_to", route_to: "..." }`
- Scripts have a 50ms execution timeout
- Scripts cannot access the network, filesystem, or external APIs
- Properties are string key-value pairs only

---

## 9. Settings

**URL:** `/settings`

### Message Retention

Configure how long messages are kept before automatic purging:
- **Messages TTL** — default 30 days
- **Error Messages TTL** — default 90 days
- Click **Update Retention Policy** to apply

### Configuration Backups

- **Create Backup** — saves a named snapshot to the database
- **Export JSON** — downloads the full config as a `.json` file
- **Import JSON** — upload a backup file to restore configuration
- Auto-backup runs every 6 hours

### System Health Check

Click **Run Tests** to verify:
- API health
- V8 JS engine
- Ingestion service connectivity
- Processing service connectivity
- NATS connectivity
- ScyllaDB connectivity

---

## 10. Users & Access Control

**URL:** `/users` (visible to admin and security roles only)

### Managing Users

- Click **+ New User** → enter username, password (min 8 chars), and role
- **Change role** — use the inline dropdown on the user row
- **Disable a user** — click the status badge to toggle active/disabled
- **Delete a user** — click Delete on the user row

### Roles Reference

The bottom panel shows all roles with their exact permissions. The 5 roles are:

| Role | Purpose |
|------|---------|
| **admin** | Full system access |
| **developer** | Build routes/filters, test in playground, manage CPs. No production message access |
| **operator** | Monitor metrics and errors. No message content or config changes |
| **security** | Manage users, roles, config backups. No access to routes or messages |
| **viewer** | Read-only access to everything except config and users |

---

## 11. Cloud Connector Configuration

When creating or editing a Communication Point, the **Protocol / Connector** dropdown offers:

**Traditional:** MLLP, HTTP, TCP, REST — shows host, port, retry config

**Cloud Storage:** S3, Azure Blob — shows bucket/container, region, credentials

**Event/Queue:** SQS, SNS, Azure Event Hub, Azure Service Bus — shows queue URL, topic ARN, connection string

**HTTP:** Webhook — shows URL, method, retries, custom headers

The config panel changes dynamically based on the selected type. Cloud connectors store their configuration in the CP's `config_json` field.

---

## 12. Tunnel Mesh

**URL:** `/tunnel`

### Creating a Tunnel Node

1. Click **+ New Tunnel Node** → enter name and site name
2. An enrollment token is generated — copy the command shown
3. Deploy the agent Docker image at the remote site:
   ```bash
   docker run -d -e BROKER_ADDR=arteria.software:9443 \
     -p 2575:2575 arteria-agent enroll <token>
   docker run -d -e BROKER_ADDR=arteria.software:9443 \
     -p 2575:2575 arteria-agent connect
   ```
4. The node status changes from PENDING → ENROLLED → CONNECTED

### Linking CPs to Tunnel Nodes

1. Go to **Comm Points** → edit a CP → check **Enable Encrypted Tunnel**
2. Select the tunnel node from the dropdown
3. Set the local port the agent should listen on
4. Save — config is automatically pushed to the agent
5. The agent starts listening and all traffic flows encrypted

### Port Management

On the agent host:
```bash
./agent-ports.sh add 2579 2580     # Add new ports
./agent-ports.sh restart           # Recreate container
```
