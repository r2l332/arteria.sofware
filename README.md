# Arteria.app

A high-throughput, cloud-native HL7v2/FHIR/DICOM interoperability engine designed for 25M+ messages/day. Inspired by Rhapsody Integration Engine, built for speed — with an encrypted tunnel mesh that eliminates the need for VPNs, and enterprise-grade security for healthcare environments.

## Architecture

```
Hospital Network                    Internet (TLS 1.3)              Cloud / Arteria
┌──────────────┐  ┌──────────────┐  ══════════════════  ┌──────────────┐  ┌──────────────────┐
│  HL7 System  │─▶│ Tunnel Agent │══════ mTLS ════════▶│ Tunnel Broker│─▶│ NATS JetStream   │
│  (plain TCP) │  │  :2575-2578  │   yamux multiplex    │  :9443       │  │ Event Backbone   │
└──────────────┘  └──────────────┘                      └──────────────┘  └────────┬─────────┘
                   No firewall changes                  Deployable anywhere        │
                   Outbound-only                        Only needs NATS            ▼
                                                                           ┌──────────────┐
                                                                           │ Processing   │
                                                                           │ V8 Filters   │ ──▶ ScyllaDB
                                                                           └──────┬───────┘
                                                                                  │
                                                              ┌───────────────────┼─────────────┐
                                                              │                   │             │
                                                        ┌─────▼──────┐    ┌───────▼────┐ ┌─────▼──────┐
                                                        │ Mgmt API   │    │  Caddy TLS │ │  Backup    │
                                                        │ :8080      │    │  :443      │ │  (cron)    │
                                                        └────────────┘    └────────────┘ └────────────┘
```

## Technology Stack

| Component | Technology | Purpose |
|-----------|-----------|---------|
| Event Backbone | NATS JetStream | In-memory message streaming with persistence |
| Storage | ScyllaDB | High-throughput NoSQL for audit trails and config |
| Backend Services | Go 1.23 | Ingestion, Processing, API, Tunnel microservices |
| JS Engine | v8go (Google V8) | User-defined message transforms at native speed |
| Tunnel Mesh | Go + mTLS + yamux | Encrypted tunnels replacing VPNs for HL7 transport |
| TLS Termination | Caddy | Auto Let's Encrypt or custom certs, security headers |
| Frontend | Next.js 14 + Tailwind | Dashboard with Monaco editor and JS playground |
| Auth | JWT + bcrypt + RBAC | 5 roles, 33 permissions, rate limiting, audit logging |
| Testing | Go test harness | 35 automated tests across 6 suites |
| Infrastructure | Docker Compose | 9-service production stack |

## Security (Healthcare/HIPAA Ready)

| Feature | Implementation |
|---------|---------------|
| Authentication | JWT tokens (24h expiry), bcrypt cost 12 |
| Authorization | RBAC with 5 roles: admin, developer, operator, security, viewer |
| PHI Protection | Messages restricted by role; developer sees sandbox only |
| Brute-force Protection | Rate limiter: 5 attempts/5min, 15min lockout per IP+username |
| Audit Logging | All mutations + logins logged with IP, user agent, timestamp |
| PHI Access Log | Who viewed which message (2yr retention) |
| TLS | Caddy auto-generates certs (Let's Encrypt or self-signed) |
| Tunnel Encryption | TLS 1.3, mutual TLS, ECDSA P-256, yamux multiplexing |
| Security Headers | HSTS, X-Frame-Options DENY, CSP, no-cache, Permissions-Policy |
| IP Allowlisting | Configurable middleware |
| Session Management | JWT expiry, active sessions tracked |
| Config Backup | Scheduled every 6h, manual export/import, named snapshots |
| Message Retention | TTL-based auto-expiry (30d messages, 90d errors, configurable) |

## RBAC Roles

| Role | Routes | CPs | Messages | Tunnel | Playground | Errors | Config | Users |
|------|--------|-----|----------|--------|-----------|--------|--------|-------|
| admin | Full | Full | Full | Full | Full | Full | Full | Full |
| developer | Full | Full | Sandbox | Full | Full | View | View | — |
| operator | View | View | — | View | — | Full | View | — |
| security | — | — | — | — | — | — | Full | Full |
| viewer | View | View | View | View | — | View | — | — |

## Cloud Connector Types

Communication points support traditional and cloud protocols:

| Category | Connectors |
|----------|-----------|
| Traditional | MLLP, TCP, HTTP, REST |
| Cloud Storage | AWS S3, Azure Blob Storage |
| Event/Queue | AWS SQS, AWS SNS, Azure Event Hub, Azure Service Bus |
| HTTP | Webhook (POST/PUT with retries and custom headers) |

## Tunnel Mesh Encryption

| Layer | Implementation |
|-------|---------------|
| Transport | TLS 1.3 (minimum) |
| Authentication | Mutual TLS (agent + broker certificates) |
| Certificate Authority | Auto-generated Arteria CA (ECDSA P-256) |
| Enrollment | One-time token → agent keypair → broker-signed cert |
| Multiplexing | yamux — multiple CPs over a single TLS connection |
| Message Routing | NATS JetStream — broker publishes to ingest stream |

### Tested: Cross-Internet (Azure East US → Local)

```
Port 2575 → ADT^A01 (Admissions)  → mTLS → NATS → ROUTED ✓
Port 2576 → ORM^O01 (Lab Orders)  → mTLS → NATS → ROUTED ✓
Port 2577 → ORU^R01 (Results)     → mTLS → NATS → ROUTED ✓
Port 2578 → ADT^A08 (Updates)     → mTLS → NATS → ROUTED ✓
4/4 CPs multiplexed over 1 tunnel, fully encrypted
```

## Quick Start

```bash
git clone https://github.com/r2l332/arteria.app.git
cd arteria.app
docker compose up -d

# 9 services start: NATS, ScyllaDB, Ingestion, Processing, API,
# Frontend, Tunnel Broker, Caddy (TLS), Backup (cron)

# Dashboard:  https://localhost  (self-signed cert auto-generated)
# API:        https://localhost/api/v1
# Login:      admin / arteriadeployment

# Send a test HL7 message:
printf '\x0bMSH|^~\\&|SRC|HOSP|DST|FAC|202608040800||ADT^A01|123|P|2.3\rPID|||PAT001||Doe^John\x1c\r' \
  | nc -w 2 localhost 2575
```

## Dashboard Pages

| Page | URL | Purpose |
|------|-----|---------|
| Dashboard | `/` | Live metrics, throughput, recent messages |
| Messages | `/messages` | Message log with raw/transformed payload viewer |
| Routes & Filters | `/routes` | Route config + Monaco JS editor for filter chains |
| Comm Points | `/comm-points` | CP management, cloud connectors, live CP logs |
| Tunnel Mesh | `/tunnel` | Tunnel node management + enrollment |
| JS Playground | `/playground` | Test JS filters live against sample payloads |
| Settings | `/settings` | Retention, backups, system health checks |
| Errors / DLQ | `/errors` | Dead letter queue viewer |
| Users & Access | `/users` | User management, role assignment (admin/security only) |

## Services (Docker Compose)

| Container | Port | Purpose |
|-----------|------|---------|
| arteria-caddy | 80, 443 | TLS reverse proxy (auto-cert) |
| arteria-ingestion | 2575 | MLLP TCP listener |
| arteria-api | 8080 | REST API (50 endpoints) |
| arteria-frontend | 3000 | Next.js dashboard |
| arteria-tunnel-broker | 9443 | mTLS tunnel broker |
| arteria-processing | — | V8 filter chain engine |
| arteria-nats | 4222, 8222 | NATS JetStream |
| arteria-scylladb | 9042 | ScyllaDB |
| arteria-backup | — | Scheduled config backup (every 6h) |

## Configuration

```bash
# .env
DOMAIN=arteria.software           # Caddy auto-provisions Let's Encrypt cert
TLS_EMAIL=admin@arteria.software  # Let's Encrypt contact
ADMIN_PASS=your-secure-password   # Default admin password
JWT_SECRET=your-secret            # Persistent JWT signing key
LOG_LEVEL=INFO                    # TRACE|DEBUG|INFO|WARN|ERROR|FATAL
MODULE_HL7=true                   # Enable HL7v2 module
MODULE_FHIR=false                 # Enable FHIR module
MODULE_DICOM=false                # Enable DICOM module
MODULE_TUNNEL=true                # Enable Tunnel Mesh
```

## Documentation

- [API Reference](docs/API.md) — All 50 REST endpoints
- [Deployment Guide](docs/DEPLOYMENT.md) — Setup, production, monitoring, troubleshooting
- [Console User Guide](docs/USER_GUIDE.md) — Dashboard walkthrough

## License

Proprietary — All rights reserved.
