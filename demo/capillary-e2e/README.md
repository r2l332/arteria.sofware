# Capillary E2E Demo

End-to-end demonstration of the Arteria Capillary tunnel system with a full round-trip message flow.

## Architecture

```
┌─────────────────────────────────────────────┐
│                  VM (Remote Site)            │
│                                             │
│  ┌──────────────┐       ┌───────────────┐  │
│  │ HL7 Generator│       │ MLLP Receiver │  │
│  │ (random ADT  │       │ (port 2576)   │  │
│  │  messages)   │       │               │  │
│  └──────┬───────┘       └───────▲───────┘  │
│         │                       │           │
│         ▼                       │           │
│  ┌──────────────────────────────────────┐   │
│  │     Web Dashboard (port 8090)        │   │
│  │  Shows sent/received + message view  │   │
│  └──────────────────────────────────────┘   │
│         │                       ▲           │
│         ▼                       │           │
│  ┌──────────────────────────────────────┐   │
│  │       Capillary Agent                │   │
│  │  MLLP :2575 (inbound to cloud)      │   │
│  │  Forwards :2576 (outbound to VM)     │   │
│  └──────────────┬───────────────────────┘   │
│                 │               ▲           │
└─────────────────┼───────────────┼───────────┘
                  │   mTLS Tunnel │
                  ▼               │
┌─────────────────┼───────────────┼───────────┐
│                 │  Arteria Cloud │           │
│  ┌──────────────▼───────────────────────┐   │
│  │       Capillary Broker               │   │
│  └──────────────┬───────────────▲───────┘   │
│                 │               │           │
│                 ▼               │           │
│  ┌──────────────────────┐      │           │
│  │   Input CP (MLLP)    │      │           │
│  └──────────┬───────────┘      │           │
│             │ NATS              │           │
│             ▼                   │           │
│  ┌────────────────────────────────────┐     │
│  │       Processing Engine            │     │
│  │                                    │     │
│  │  Filter 1: Python HL7→JSON         │     │
│  │  ┌────────────────────────────┐    │     │
│  │  │ Parses HL7 segments        │    │     │
│  │  │ Converts to structured JSON│    │     │
│  │  └─────────────┬──────────────┘    │     │
│  │                │                   │     │
│  │  Filter 2: Python Extract Patient  │     │
│  │  ┌────────────────────────────┐    │     │
│  │  │ Extracts patient_id        │    │     │
│  │  │ Extracts patient_name      │    │     │
│  │  │ Builds compact JSON output │    │     │
│  │  └─────────────┬──────────────┘    │     │
│  └────────────────┼──────────────────-┘     │
│                   │ NATS                    │
│                   ▼                         │
│  ┌──────────────────────┐                   │
│  │   Output CP (MLLP)   │──────────────┘   │
│  │   → Capillary → VM   │                   │
│  └──────────────────────┘                   │
└─────────────────────────────────────────────┘
```

## What it demonstrates

1. **Inbound tunneling**: HL7 messages generated on the VM flow through the Capillary mTLS tunnel into the Arteria engine
2. **Python filter transforms**: The engine runs Python scripts that:
   - Parse raw HL7 pipe-delimited format into structured JSON
   - Extract patient name and ID into a compact output
3. **Outbound tunneling**: The processed patient extract is sent back through the Capillary tunnel to the VM
4. **Visibility**: A web dashboard on the VM shows messages sent and received in real-time

## Running

```bash
./run-demo.sh          # Start everything
./run-demo.sh stop     # Tear down
./run-demo.sh logs     # Watch logs
./run-demo.sh seed     # Re-seed the demo route/filters
```

## Endpoints

| Service | URL | Description |
|---------|-----|-------------|
| VM Dashboard | http://localhost:8090 | Real-time view of sent/received messages |
| Arteria UI | http://localhost:3000 | Main Arteria management console |
| Arteria API | http://localhost:8080 | REST API |

## Configuration

Environment variables for the VM app:

| Variable | Default | Description |
|----------|---------|-------------|
| `SEND_ADDR` | `localhost:2575` | MLLP address to send HL7 to |
| `RECV_ADDR` | `:2576` | Address to listen for processed results |
| `WEB_PORT` | `8090` | Dashboard web server port |
| `SEND_INTERVAL_MS` | `3000` | Milliseconds between message sends |
| `BURST_SIZE` | `1` | Number of messages per burst |

## Python Filters

### Filter 1: HL7 to JSON (`hl7_to_json.py`)

Parses the raw HL7 pipe-delimited message into a structured JSON document with typed segments (MSH, PID, PV1).

**Input**: Raw HL7 in `rawPayload`  
**Output**: JSON-structured message in `rawPayload`

### Filter 2: Extract Patient (`extract_patient.py`)

Extracts patient identifiers from the JSON-transformed message and produces a compact summary.

**Input**: JSON-structured HL7 from Filter 1  
**Output**: `{"patient_id": "MRN12345", "patient_name": "John Smith", ...}`

## Sample Output

The VM dashboard will show processed messages like:

```json
{
  "patient_id": "MRN42857",
  "patient_name": "James Wilson",
  "date_of_birth": "19720315",
  "sex": "M",
  "message_id": "MSG000042",
  "facility": "GENERAL_HOSP"
}
```
