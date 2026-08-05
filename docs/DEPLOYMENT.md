# Arteria Deployment Guide

## Prerequisites

- Docker Engine 24+ and Docker Compose v2
- 4GB RAM minimum (8GB recommended)
- Ports available: 2575, 3000, 4222, 8080, 8222, 9042

---

## 1. Local Development

### Start the full stack

```bash
git clone https://github.com/r2l332/arteria.app.git
cd arteria.app
docker compose up -d
```

**Default credentials:** `admin` / `arteria123` — you will be prompted to set a new password on first login.

This starts 10 containers:

| Container | Image | Purpose |
|-----------|-------|---------|
| `arteria-nats` | `nats:2-alpine` | NATS JetStream message broker |
| `arteria-scylladb` | `scylladb/scylla` | ScyllaDB database |
| `arteria-scylla-init` | `scylladb/scylla` | One-shot schema initialization |
| `arteria-ingestion` | Built from `backend/` | MLLP → NATS ingest (inbound) |
| `arteria-processing` | Built from `backend/` | NATS → V8 filter → route |
| `arteria-egress` | Built from `backend/` | Route → deliver to output CPs |
| `arteria-api` | Built from `backend/` | REST API for dashboard |
| `arteria-frontend` | Built from `frontend/` | Next.js dashboard |
| `arteria-tunnel-broker` | Built from `backend/` | Aorta mTLS broker |
| `arteria-caddy` | `caddy:2-alpine` | TLS reverse proxy |
| `arteria-backup` | Built from root | Scheduled config backup |

### Verify health

```bash
# All containers running
docker compose ps

# API health
curl http://localhost:8080/health

# Send a test message
printf '\x0bMSH|^~\\&|SRC|HOSP|DST|FAC|202608040800||ADT^A01|123|P|2.3\rPID|||PAT001||Doe^John\x1c\r' \
  | nc -w 2 localhost 2575

# Check it arrived
curl http://localhost:8080/api/v1/messages
```

### Stop and clean up

```bash
docker compose down           # Stop containers
docker compose down -v        # Stop + delete volumes (data reset)
```

---

## 2. Configuration

### Environment Variables

Create a `.env` file in the project root to override defaults:

```bash
# Logging
LOG_LEVEL=INFO                # TRACE|DEBUG|INFO|WARN|ERROR|FATAL
LOG_SINKS=stdout,file         # stdout, file, http (comma-separated)

# HTTP log shipping (e.g., Loki, Splunk HEC, Logstash)
LOG_HTTP_URL=http://loki:3100/loki/api/v1/push
LOG_HTTP_HEADERS=X-Scope-OrgID=arteria

# ScyllaDB (for multi-node clusters)
# SCYLLA_HOSTS=scylla1,scylla2,scylla3
```

Docker Compose automatically reads `.env`.

### Reduce log verbosity

For production, reduce from TRACE to INFO or WARN:

```bash
# Via .env
LOG_LEVEL=WARN

# Or at runtime (no restart)
curl -X PUT http://localhost:8080/api/v1/config/log-level \
  -H 'Content-Type: application/json' -d '{"level":"WARN"}'
```

---

## 3. Data Persistence

All data survives container restarts via Docker named volumes:

| Volume | Mount | Contents |
|--------|-------|----------|
| `nats_data` | `/data` in NATS | JetStream message store |
| `scylla_data` | `/var/lib/scylla` in ScyllaDB | All tables, config, messages |
| `arteria_logs` | `/var/log/arteria/` in all backend services | Structured JSON log files |

### Backup ScyllaDB

```bash
# Snapshot
docker exec arteria-scylladb nodetool snapshot arteria

# Export data
docker exec arteria-scylladb cqlsh -e \
  "COPY arteria.routes TO '/tmp/routes.csv' WITH HEADER=TRUE;"
docker cp arteria-scylladb:/tmp/routes.csv ./backups/
```

### Access log files

```bash
# From any backend container
docker exec arteria-ingestion cat /var/log/arteria/ingestion.log | tail -20

# Or mount the volume directly
docker run --rm -v arteriaapp_arteria_logs:/logs alpine cat /logs/ingestion.log
```

---

## 4. Production Considerations

### ScyllaDB

For production, change the ScyllaDB configuration:

```yaml
# docker-compose.prod.yml
services:
  scylladb:
    command: ["--smp", "4", "--memory", "4G"]  # Remove --developer-mode
    deploy:
      resources:
        limits:
          cpus: '4'
          memory: 4G
```

Update the keyspace replication:
```sql
ALTER KEYSPACE arteria WITH replication = {
  'class': 'NetworkTopologyStrategy',
  'datacenter1': 3
};
```

### NATS JetStream

For production, switch from memory to file storage:

```yaml
services:
  nats:
    command: ["--jetstream", "--store_dir", "/data", "-m", "8222"]
```

Update stream config in `natsutil/nats.go`:
```go
Storage: nats.FileStorage,  // Instead of MemoryStorage
```

### Scaling

The processing service supports horizontal scaling — multiple instances share the same NATS consumer group:

```yaml
services:
  processing:
    deploy:
      replicas: 4
```

### TLS

For MLLP with TLS, configure the ingestion service with certificates:

```yaml
services:
  ingestion:
    environment:
      TLS_CERT: /certs/server.crt
      TLS_KEY: /certs/server.key
    volumes:
      - ./certs:/certs:ro
```

---

## 5. Monitoring

### Dashboard

Access at `https://your-domain`:

- **Dashboard** — Live throughput metrics, recent messages, errors
- **Messages** — Full message log with detail viewer (raw + transformed payload)
- **Routes** — Route configuration with Monaco JS editor for filters
- **Comm Points** — Communication point management with live log viewer
- **Aorta Mesh** — Capillary node management + enrollment
- **Errors / DLQ** — Dead letter queue viewer

### NATS Monitoring

```bash
# Stream info
curl http://localhost:8222/jsz | python3 -m json.tool

# Server info
curl http://localhost:8222/varz | python3 -m json.tool
```

### API Metrics

```bash
# Live service metrics
curl http://localhost:8080/api/v1/metrics

# Per communication point metrics + logs
curl http://localhost:8080/api/v1/metrics/comm-points

# Single CP log viewer
curl http://localhost:8080/api/v1/metrics/comm-points/<cp-id>/logs
```

### Log Shipping

To ship logs to an external system (Loki, Splunk, Logstash):

```bash
# .env
LOG_SINKS=stdout,file,http
LOG_HTTP_URL=http://loki:3100/loki/api/v1/push
LOG_HTTP_HEADERS=X-Scope-OrgID=arteria,Authorization=Bearer TOKEN
```

Logs are batched (100 entries or 5s flush interval) and shipped as JSON arrays via HTTP POST.

---

## 6. Troubleshooting

### ScyllaDB won't start

```bash
# Check logs
docker logs arteria-scylladb

# Common fix: increase shared memory
docker compose down -v  # Reset volumes
docker compose up -d
```

### Schema init fails

```bash
# Check init logs
docker logs arteria-scylla-init

# Re-run manually
docker compose up -d scylla-init
```

### Messages not appearing in ScyllaDB

```bash
# Check processing service logs
docker logs arteria-processing

# Query ScyllaDB directly
docker exec arteria-scylladb cqlsh -e \
  "SELECT message_id, status FROM arteria.messages LIMIT 10;"
```

### V8 filter timeout

The default V8 execution timeout is 50ms. If filters are timing out, check:

```bash
# Look for timeout errors
docker logs arteria-processing | grep "timeout"
```

Simplify the JS script or increase the timeout in `v8pool.Config`.

### NATS connection issues

```bash
# Check NATS health
curl http://localhost:8222/healthz

# Check stream state
curl http://localhost:8222/jsz | python3 -m json.tool
```

---

## 7. Architecture Diagram (Docker Compose)

```
┌─────────────────────────────────────────────────────────────────┐
│                        Docker Network                           │
│                                                                 │
│  ┌───────────┐  ┌──────────┐  ┌───────────┐  ┌──────────────┐ │
│  │   NATS    │  │ ScyllaDB │  │  Scylla   │  │              │ │
│  │ JetStream │  │          │  │   Init    │  │              │ │
│  │  :4222    │  │  :9042   │  │ (one-shot)│  │              │ │
│  └─────┬─────┘  └────┬─────┘  └───────────┘  │              │ │
│        │              │                        │              │ │
│  ┌─────┴──────┐  ┌────┴─────────────┐  ┌─────┴────────────┐ │
│  │ Ingestion  │  │   Processing     │  │   Management     │ │
│  │  :2575     │  │   (V8 Engine)    │  │   API :8080      │ │
│  │  MLLP IN   │  │   Filter Chain   │  │   REST/JSON      │ │
│  └────────────┘  └──────────────────┘  └────────┬─────────┘ │
│                                                  │           │
│                                          ┌───────┴────────┐  │
│                                          │   Frontend     │  │
│                                          │   :3000        │  │
│                                          │   Next.js      │  │
│                                          └────────────────┘  │
│                                                              │
│  Volumes: nats_data, scylla_data, arteria_logs               │
└──────────────────────────────────────────────────────────────┘
```

---

## 8. TLS / HTTPS

Caddy reverse proxy auto-generates TLS certificates on first boot.

### Auto Let's Encrypt (production)

```bash
# .env
DOMAIN=arteria.software
TLS_EMAIL=admin@arteria.software
```

Caddy auto-provisions a Let's Encrypt cert. Requires ports 80+443 reachable from the internet.

### Custom / Enterprise Certificates

```bash
mkdir certs/
cp your-cert.crt certs/server.crt
cp your-key.key certs/server.key

# Use the custom certs Caddyfile
cp infra/caddy/Caddyfile.custom-certs infra/caddy/Caddyfile

# .env
DOMAIN=arteria.hospital.internal
TLS_CERT_DIR=./certs
```

### Self-Signed (local dev)

No config needed — Caddy generates a self-signed cert for `localhost` automatically.

---

## 9. Security Configuration

### RBAC

Five built-in roles. Users are managed via dashboard or API:

```bash
# Create a developer user
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -d '{"username":"dev1","password":"securepass","role":"developer"}' \
  https://arteria.software/api/v1/users
```

### Rate Limiting

Login attempts are rate-limited: 5 attempts per 5 minutes, 15-minute lockout.

### Audit Log

All mutations and logins are logged:

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "https://arteria.software/api/v1/audit-log?username=admin"
```

### IP Allowlisting

Set `IP_ALLOWLIST=10.0.0.0/8,192.168.1.0/24` in the API environment to restrict access.

---

## 10. Backup & Retention

### Automatic Backups

The `arteria-backup` container runs a cron job every 6 hours:
- Exports all config to `/backups/arteria-config-YYYYMMDD-HHMMSS.json`
- Saves a named snapshot to ScyllaDB
- Cleans up backups older than 30 days

### Manual Backup/Restore

```bash
# Export
curl -H "Authorization: Bearer $TOKEN" \
  https://arteria.software/api/v1/config/export > backup.json

# Restore
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -d @backup.json https://arteria.software/api/v1/config/import
```

### Message Retention

Messages auto-expire via ScyllaDB TTL:

| Data | Default TTL | Configurable |
|------|------------|-------------|
| Messages | 30 days | Yes, via API |
| Error messages | 90 days | Yes, via API |
| Config history | 1 year | — |
| Audit log | 1 year | — |
| PHI access log | 2 years | — |

```bash
# Change retention
curl -X PUT -H "Authorization: Bearer $TOKEN" \
  -d '{"messages_ttl_days":60,"error_messages_ttl_days":180}' \
  https://arteria.software/api/v1/config/retention
```

---

## 11. Aorta Mesh Deployment (Capillary Agents)

### Option 1: Standalone Binary (Recommended)

No Docker required at the remote site. Download the single Capillary binary:

```bash
# Download for your platform (linux/amd64, linux/arm64, windows/amd64, darwin/*)
curl -LO https://github.com/r2l332/arteria.app/releases/download/v0.1.0/capillary-0.1.0-linux-amd64
chmod +x capillary-0.1.0-linux-amd64
mv capillary-0.1.0-linux-amd64 /usr/local/bin/capillary

# 1. Create node in Arteria dashboard (Aorta Mesh page) → get enrollment token
# 2. Enroll the Capillary agent:
capillary enroll <token> --broker arteria.software:9443

# 3. Connect and start tunneling:
capillary connect --broker arteria.software:9443
```

Run as a systemd service:

```ini
# /etc/systemd/system/capillary.service
[Unit]
Description=Arteria Capillary Agent
After=network.target

[Service]
ExecStart=/usr/local/bin/capillary connect --broker arteria.software:9443
Restart=always
RestartSec=5
Environment=AGENT_CONFIG_DIR=/etc/arteria-agent

[Install]
WantedBy=multi-user.target
```

### Option 2: Docker Deployment

```bash
# Enroll:
docker run -d --name capillary \
  --restart unless-stopped \
  -v /opt/arteria-agent:/etc/arteria-agent \
  -e BROKER_ADDR=arteria.software:9443 \
  -p 2575:2575 \
  capillary enroll <token>

# Connect:
docker run -d --name capillary \
  --restart unless-stopped \
  -v /opt/arteria-agent:/etc/arteria-agent \
  -e BROKER_ADDR=arteria.software:9443 \
  -p 2575:2575 -p 2576:2576 \
  arteria-agent connect
```

### Port Management

```bash
# On the agent host:
./agent-ports.sh list              # Show current ports
./agent-ports.sh add 2579 2580     # Add ports
./agent-ports.sh restart           # Recreate container
```

### Required Firewall Ports

| Location | Port | Direction | Purpose |
|----------|------|-----------|---------|
| Arteria (cloud) | 443 | Inbound | Dashboard + API (HTTPS) |
| Arteria (cloud) | 9443 | Inbound | Aorta broker (Capillary connections) |
| Capillary (hospital) | — | Outbound to :9443 | Capillary connects to Aorta |
| Capillary (hospital) | 2575+ | Local | HL7 apps send to Capillary |

---

## 12. Testing

### Automated Test Suite

```bash
docker compose --profile test run --rm test-runner all
```

35 tests across 6 suites: Ingestion, API CRUD, Filter Chain, Routing, Error Handling, Metrics.

### System Health Check

Dashboard → Settings → Run Tests (checks API, V8, NATS, ScyllaDB connectivity).
