# Arteria.app

A high-throughput, cloud-native HL7v2/FHIR/DICOM interoperability engine designed for 25M+ messages/day. Inspired by Rhapsody Integration Engine, built for speed — with an encrypted tunnel mesh that eliminates the need for VPNs.

## Architecture

```
Hospital A                     Internet (TLS)                      Cloud
┌──────────┐  ┌─────────────┐  ═══════════════  ┌──────────────┐  ┌──────────────────────┐
│  HL7 App │─▶│ Tunnel Agent│══════mTLS════════▶│ Tunnel Broker│─▶│ Ingestion Service    │
│(plain TCP)│  │  :2575      │  (yamux mux)      │  :9443       │  │  (MLLP) :2575        │
└──────────┘  └─────────────┘                    └──────────────┘  └──────────┬───────────┘
                                                                              │
                                                                         NATS JetStream
                                                                      arteria.ingest.raw
                                                                              │
                                                                   ┌──────────▼───────────┐
                                                                   │ Processing Service   │
                                                                   │ V8 Filter Chain      │
                                                                   │ Parse→Filter→Route   │
                                                                   └──────────┬───────────┘
                                                                              │
                                                                         ScyllaDB
                                                                    (messages, config)
                                                                              │
                                                              ┌───────────────┼───────────┐
                                                              │               │           │
                                                        ┌─────▼──────┐ ┌──────▼─────┐ ┌──▼───────┐
                                                        │ Mgmt API   │ │  Frontend  │ │  Egress  │
                                                        │ :8080      │ │  :3000     │ │  Service │
                                                        └────────────┘ └────────────┘ └──────────┘
```

## Technology Stack

| Component | Technology | Purpose |
|-----------|-----------|---------|
| Event Backbone | NATS JetStream | In-memory message streaming with persistence |
| Storage | ScyllaDB | High-throughput NoSQL for audit trails and config |
| Backend Services | Go 1.23 | Ingestion, Processing, API, Tunnel microservices |
| JS Engine | v8go (Google V8) | User-defined message transforms at native speed |
| Tunnel Mesh | Go + mTLS + yamux | Encrypted tunnels replacing VPNs for HL7 transport |
| Frontend | Next.js 14 + Tailwind | Dashboard with Monaco editor and JS playground |
| Testing | Go test harness | Automated test suites deployable as Docker service |
| Infrastructure | Docker Compose | Local development and deployment |

## Rhapsody Concept Mapping

| Rhapsody | Arteria | Description |
|----------|---------|-------------|
| Communication Point | `communication_points` table | Configurable endpoints with retry/timeout and tunnel toggle |
| Route | `routes` table | Connects source CP → filter chain → destination CP |
| Filter (JS/Mapper) | `filters` table + V8 pool | Ordered filter chain per route with JS transforms |
| Conditional Connector | Filter type `conditional` | JS predicate returning pass/reject/route_to |
| Locker Variables | `lookup_tables` + `lookup_entries` | Shared key-value data accessible from JS filters |
| Message Properties | `properties` JSON field | Metadata flowing through the pipeline |
| Error Queue | NATS DLQ + `error_messages` table | Failed messages with error details |
| Management Console | Next.js Dashboard | Routes, filters, CP logs, message viewer, tunnel config |
| CP Log Viewer | Per-CP ring buffer (200 entries) | Live log stream per communication point |
| VPN / TLS Certs | Tunnel Mesh (agent + broker) | Auto-provisioned mTLS tunnels, no VPN needed |

## Project Structure

```
arteria.app/
├── backend/
│   ├── cmd/
│   │   ├── ingestion/main.go        # MLLP TCP listener → NATS publisher
│   │   ├── processing/main.go       # NATS consumer → V8 filter chain → ScyllaDB
│   │   ├── api/main.go              # REST API (Fiber) for dashboard
│   │   ├── tunnel-broker/main.go    # Accepts mTLS connections from remote agents
│   │   └── tunnel-agent/main.go     # Standalone agent deployed at remote sites
│   ├── pkg/
│   │   ├── engine/engine.go         # Filter chain engine (route matching, V8 execution)
│   │   ├── hl7/parser.go            # Lightweight HL7v2 parser (MSH-9, PID-3, MSH-4)
│   │   ├── logging/                 # Structured JSON logger with multi-sink support
│   │   ├── metrics/metrics.go       # Atomic counters + per-CP metrics/log ring buffer
│   │   ├── mllp/server.go           # MLLP TCP server (frame parser, concurrent conns)
│   │   ├── natsutil/nats.go         # NATS JetStream connection wrapper
│   │   ├── scyllautil/scylla.go     # ScyllaDB connection wrapper with retry
│   │   ├── tunnel/                  # TLS tunnel mesh (certs, broker, agent)
│   │   └── v8pool/pool.go           # Pre-warmed V8 isolate pool with timeouts
│   └── Dockerfile.*                 # Per-service Docker images
├── frontend/
│   ├── app/
│   │   ├── page.tsx                 # Dashboard (stats, live metrics, recent messages)
│   │   ├── messages/page.tsx        # Message log with detail modal
│   │   ├── routes/page.tsx          # Route config + Monaco JS editor for filters
│   │   ├── comm-points/page.tsx     # CP management + live log viewer + tunnel toggle
│   │   ├── tunnel/page.tsx          # Tunnel node management + enrollment
│   │   ├── playground/page.tsx      # JS filter playground (live V8 sandbox)
│   │   └── errors/page.tsx          # Error/DLQ viewer
│   └── ...
├── testing/
│   ├── cmd/harness/main.go          # Go test harness (35 tests across 6 suites)
│   ├── scenarios/                   # Sample HL7 message files
│   ├── scripts/run-tests.sh         # Test runner script
│   └── Dockerfile                   # Deployable test service
├── infra/
│   ├── cql/                         # ScyllaDB schemas + seed data
│   └── scripts/init-schema.sh       # Boot-time schema initializer
├── docs/
│   ├── API.md                       # REST API reference (30+ endpoints)
│   ├── DEPLOYMENT.md                # Deployment and operations guide
│   └── USER_GUIDE.md                # Console user guide
└── docker-compose.yml               # Full stack: 8 services
```

## Quick Start

```bash
git clone https://github.com/r2l332/arteria.app.git
cd arteria.app
docker compose up -d

# Wait ~30s for ScyllaDB, then:
# Dashboard:      http://localhost:3000
# API:            http://localhost:8080
# JS Playground:  http://localhost:3000/playground
# MLLP Input:     localhost:2575
# Tunnel Broker:  localhost:9443

# Send a test HL7 message
printf '\x0bMSH|^~\\&|SRC|HOSP|DST|FAC|202608040800||ADT^A01|123|P|2.3\rPID|||PAT001||Doe^John\x1c\r' \
  | nc -w 2 localhost 2575

# Run the test suite
docker compose --profile test run --rm test-runner all
```

## Tunnel Mesh

The tunnel mesh solves the industry-wide problem of encrypting HL7 TCP traffic without VPNs.

**The problem:** Hospitals send plain-text HL7/MLLP. Arteria runs in the cloud. VPNs require firewall rules, multiple stakeholders, and ongoing ops burden.

**The solution:**
1. Deploy the **Tunnel Agent** (single Docker image) at the hospital
2. Agent connects **outbound** to the Arteria Tunnel Broker — no inbound firewall rules
3. Enable **tunnel on a CP** in the dashboard — config auto-pushes to the agent
4. Traffic flows: Hospital → Agent (plain TCP) → mTLS tunnel → Broker → Arteria

```bash
# At Hospital A:
docker run -d arteria-agent enroll <token> --broker arteria.example.com:9443
docker run -d arteria-agent connect --broker arteria.example.com:9443
```

## JS Filter Playground

Test JavaScript filters live before deploying them:
- Navigate to `/playground` in the dashboard
- Write `transform(msg)` or `evaluate(msg)` functions
- Edit the input payload JSON
- Click **▶ Run** — executes in a real V8 isolate, shows output instantly

## Configuration

All services are configured via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `NATS_URL` | `nats://localhost:4222` | NATS server URL |
| `SCYLLA_HOST` | `127.0.0.1` | ScyllaDB host(s) |
| `INGESTION_PORT` | `2575` | MLLP listener port |
| `API_PORT` | `8080` | REST API port |
| `BROKER_LISTEN` | `:9443` | Tunnel broker listen address |
| `LOG_LEVEL` | `TRACE` | TRACE, DEBUG, INFO, WARN, ERROR, FATAL |
| `LOG_SINKS` | `stdout,file` | stdout, file, http |
| `LOG_FILE` | `/var/log/arteria/<svc>.log` | Log file path |
| `LOG_HTTP_URL` | — | HTTP log shipping endpoint (Loki/Splunk) |

## Ports

| Service | Port | Protocol |
|---------|------|----------|
| Ingestion (MLLP) | 2575 | TCP |
| Management API | 8080 | HTTP |
| Frontend Dashboard | 3000 | HTTP |
| Tunnel Broker | 9443 | TLS |
| NATS Client | 4222 | TCP |
| NATS Monitoring | 8222 | HTTP |
| ScyllaDB CQL | 9042 | TCP |

## Documentation

- [API Reference](docs/API.md) — All 30+ REST endpoints
- [Deployment Guide](docs/DEPLOYMENT.md) — Setup, production, monitoring, troubleshooting
- [Console User Guide](docs/USER_GUIDE.md) — Dashboard walkthrough with screenshots

## License

Proprietary — All rights reserved.
