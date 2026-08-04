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

This starts 6 containers:

| Container | Image | Purpose |
|-----------|-------|---------|
| `arteria-nats` | `nats:2-alpine` | NATS JetStream message broker |
| `arteria-scylladb` | `scylladb/scylla` | ScyllaDB database |
| `arteria-scylla-init` | `scylladb/scylla` | One-shot schema initialization |
| `arteria-ingestion` | Built from `backend/` | MLLP → NATS ingest |
| `arteria-processing` | Built from `backend/` | NATS → V8 filter → ScyllaDB |
| `arteria-api` | Built from `backend/` | REST API for dashboard |
| `arteria-frontend` | Built from `frontend/` | Next.js dashboard |

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

Access at `http://localhost:3000`:

- **Dashboard** — Live throughput metrics, recent messages, errors
- **Messages** — Full message log with detail viewer (raw + transformed payload)
- **Routes** — Route configuration with Monaco JS editor for filters
- **Comm Points** — Communication point management with live log viewer
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
