# Arteria Production Architecture & Scaling Guide
# INTERNAL — Lee Jelley only

## Cost-Effective Production Architecture

### Tier 1: Single-Node (Up to 5M msgs/day) — ~$150/month

```
┌─────────────────────────────────────────────────────┐
│  Single VM: Standard_D4ds_v5 (4 vCPU, 16GB RAM)    │
│  Azure / AWS / Hetzner dedicated                     │
│                                                      │
│  ┌─────────┐ ┌────────────┐ ┌───────────────────┐  │
│  │  NATS   │ │  ScyllaDB  │ │  Arteria Stack    │  │
│  │  (RAM)  │ │  (1 node)  │ │  All services     │  │
│  └─────────┘ └────────────┘ └───────────────────┘  │
│                                                      │
│  Storage: 100GB SSD (messages + audit logs)          │
│  Network: 1Gbps                                      │
└─────────────────────────────────────────────────────┘
```

**Cost breakdown:**
- Azure Standard_D4ds_v5: ~$140/month (reserved 1yr)
- Or Hetzner CPX41: ~$30/month (way cheaper, EU only)
- Storage: Included in VM
- Bandwidth: ~$5-10/month for tunnel traffic

**Limits:**
- ~60 msgs/sec sustained = 5M/day
- 4 Capillary connections comfortably
- 30-day message retention
- Single point of failure (acceptable for small deployments)

---

### Tier 2: Resilient Pair (Up to 25M msgs/day) — ~$400/month

```
┌──────────────────────┐     ┌──────────────────────┐
│  Node A (Primary)    │     │  Node B (Hot Standby) │
│  D4ds_v5             │     │  D4ds_v5              │
│                      │     │                       │
│  NATS (cluster)  ◄───┼─────┼──► NATS (cluster)    │
│  ScyllaDB (RF=2) ◄───┼─────┼──► ScyllaDB (RF=2)   │
│  Arteria services    │     │  Arteria services     │
│  Aorta broker        │     │  Aorta broker (stby)  │
│  Caddy (active)      │     │  Caddy (passive)      │
└──────────────────────┘     └──────────────────────┘
         │                              │
         └──────── Azure LB ───────────┘
                      │
              DNS: arteria.software
```

**How it works:**
- NATS cluster (2-node, JetStream with R=2)
- ScyllaDB with RF=2 (each node has a full copy)
- Azure Load Balancer or Keepalived for failover
- Both nodes run all services; LB routes to healthy one
- If Node A dies, Node B serves within seconds (NATS re-elects leader)
- Capillary agents reconnect automatically (built-in retry)

**Cost:**
- 2x D4ds_v5: ~$280/month (reserved)
- Azure LB: ~$20/month
- Storage: 200GB each = ~$40/month
- Bandwidth: ~$20/month
- **Total: ~$360-400/month**

---

### Tier 3: Full HA (25M+ msgs/day, zero downtime) — ~$800-1200/month

```
┌────────────────────────────────────────────────────────────────────┐
│                        Azure / AWS Region                           │
│                                                                     │
│  ┌─── Availability Zone 1 ───┐   ┌─── Availability Zone 2 ───┐   │
│  │  NATS node 1              │   │  NATS node 2              │   │
│  │  ScyllaDB node 1          │   │  ScyllaDB node 2          │   │
│  │  Processing (x2 replicas) │   │  Processing (x2 replicas) │   │
│  │  Ingestion (x2 replicas)  │   │  Ingestion (x2 replicas)  │   │
│  │  API (x1)                 │   │  API (x1)                 │   │
│  │  Aorta broker (x1)        │   │  Aorta broker (x1)        │   │
│  └────────────────────────────┘   └────────────────────────────┘   │
│                                                                     │
│  ┌─── Availability Zone 3 ───┐                                     │
│  │  NATS node 3 (quorum)     │   ┌─────────────────────────────┐  │
│  │  ScyllaDB node 3          │   │  Azure Front Door / ALB     │  │
│  └────────────────────────────┘   │  TLS termination           │  │
│                                    │  Geo-routing               │  │
│                                    └─────────────────────────────┘  │
└────────────────────────────────────────────────────────────────────┘
```

**Key decisions:**
- NATS: 3-node cluster (Raft consensus, tolerates 1 failure)
- ScyllaDB: 3-node with RF=3 (tolerates 1 failure, CL=QUORUM)
- Processing: Horizontally scaled (NATS consumer groups handle this automatically)
- Ingestion: Horizontally scaled (each listens on same port via LB)
- API: 2 replicas behind LB
- Aorta: Active/passive (Capillaries reconnect on failover)

**Cost (Azure):**
- 3x D4ds_v5 (NATS+Scylla): ~$420/month
- 4x D2ds_v5 (Processing): ~$240/month  
- 2x B2ms (API+Ingestion): ~$60/month
- Azure Front Door: ~$35/month
- Managed disks: ~$100/month
- **Total: ~$850-1000/month**

**Or with Kubernetes (AKS):**
- 3-node AKS cluster (D4ds_v5): ~$420/month
- ScyllaDB Operator manages storage
- HPA auto-scales processing pods
- Same resilience, easier operations
- **Total: ~$600-800/month** (AKS is free, pay for nodes only)

---

## Scaling Levers

### What scales horizontally (cheap to add):
| Component | How | Cost per unit |
|-----------|-----|---------------|
| Processing | Add replicas, NATS consumer groups auto-balance | ~$60/month per D2 |
| Ingestion | Add replicas behind LB, same MLLP port | ~$60/month per D2 |
| Capillary agents | Unlimited, each connects to Aorta independently | $0 (runs at customer site) |
| API | Add replicas behind LB | ~$30/month per B2 |

### What scales vertically (scale up the box):
| Component | Limit | When to scale |
|-----------|-------|---------------|
| NATS | Memory (message buffer) | >10K msgs/sec sustained |
| ScyllaDB | Disk IOPS + Memory | >50K writes/sec |
| V8 Pool | CPU cores (1 isolate per core) | Filter execution > 10ms avg |
| Aorta broker | Memory (yamux sessions) | >100 Capillary connections |

### Key bottleneck points:
1. **ScyllaDB writes** — First bottleneck at high volume. Fix: add nodes, increase RF
2. **V8 filter execution** — CPU-bound. Fix: increase pool size, add processing replicas
3. **NATS JetStream** — Memory pressure at sustained high throughput. Fix: file storage, add nodes
4. **Network I/O** — Tunnel bandwidth. Fix: co-locate Aorta near Capillaries

---

## Cost Optimization Strategies

### 1. Use reserved instances (save 40-60%)
- 1-year reserved: ~40% savings
- 3-year reserved: ~60% savings
- Always reserve NATS+ScyllaDB nodes (they run 24/7)

### 2. Use spot instances for processing
- Processing is stateless and restartable
- Azure Spot VMs: 60-90% discount
- If evicted, NATS re-delivers unacked messages automatically
- Configure: `nats.AckWait(60*time.Second)` for spot tolerance

### 3. Tiered storage for ScyllaDB
- Hot (SSD): Last 7 days of messages
- Warm (HDD): 7-30 days
- Cold (Blob): 30+ days (archive via cloud connector)
- TTL handles auto-expiry: no manual cleanup needed

### 4. Right-size for actual load
- Most hospitals: 1-5M msgs/day → Tier 1 is sufficient
- Large health systems: 5-25M → Tier 2
- HIE / National systems: 25M+ → Tier 3

### 5. Hetzner for non-regulated workloads
- 4x cheaper than Azure for equivalent specs
- Great for dev/staging/demo environments
- Dedicated servers: $50-100/month for 8 core, 32GB, NVMe

---

## Operational Playbook

### Monitoring checklist:
- NATS: stream lag (msgs pending > 1000 = falling behind)
- ScyllaDB: write latency P99 (> 10ms = needs attention)
- Processing: msgs/min rate vs ingestion rate (gap = backpressure)
- V8: timeout count (> 0.1% = scripts too complex)
- Aorta: active tunnel count, reconnection events
- Disk: ScyllaDB data size (plan for growth)

### Capacity planning formula:
```
Required processing power = (msgs/sec) × (avg filter time) × (safety factor 2x)
Required NATS memory = (msgs/sec) × (avg msg size) × (buffer seconds) × (RF)
Required ScyllaDB IOPS = (msgs/sec) × 3 (insert + 2 index updates)
Required disk = (msgs/day) × (avg msg size) × (retention days) × 1.3 (compaction overhead)
```

### Example: 10M msgs/day hospital system
```
msgs/sec = 10M / 86400 = ~116 msgs/sec
Avg msg size = 800 bytes (weighted by message mix)
Filter time = 5ms avg

Processing: 116 × 0.005 × 2 = 1.16 cores → 2 cores sufficient
NATS memory: 116 × 800 × 60 × 2 = 11MB buffer (trivial)
ScyllaDB IOPS: 116 × 3 = 348 IOPS (any SSD handles this)
Disk/month: 10M × 800 × 30 = 240GB (30-day retention)
```

**Conclusion: A single D4 VM handles 10M msgs/day comfortably.**

---

## Deployment Recommendations

### For selling to customers:
1. **Start with Tier 1** — Prove value on a single VM ($150/month)
2. **Upsell to Tier 2** when they need HA — "resilient pair" ($400/month)
3. **Tier 3 only for enterprise** with SLA requirements (>$800/month)
4. **Capillary is free** — runs at their site, no infrastructure cost to you
5. **Per-message pricing** doesn't make sense; use monthly subscription

### Pricing model suggestion:
| Tier | Price | Includes |
|------|-------|----------|
| Starter | $500/month | 5M msgs/day, 2 Capillaries, 30-day retention |
| Professional | $1500/month | 25M msgs/day, 10 Capillaries, 90-day retention, HA |
| Enterprise | $5000/month | Unlimited, dedicated, custom retention, SLA |

Your infrastructure cost is 10-30% of the price → healthy margin.

---

## Performance Test Scenarios

### Scenario 1: Steady State (normal day)
```bash
DURATION=30m CONCURRENCY=10 ./scripts/run-perftest.sh
```
Simulates typical hospital traffic: ~40 msgs/sec across 4 CPs.

### Scenario 2: Peak Load (morning shift change)
```bash
DURATION=10m CONCURRENCY=50 ./scripts/run-perftest.sh
```
Simulates 200+ msgs/sec burst when all departments start simultaneously.

### Scenario 3: Soak Test (24h endurance)
```bash
DURATION=24h CONCURRENCY=15 ./scripts/run-perftest.sh
```
Validates memory leaks, disk growth, GC pressure over extended period.

### Scenario 4: Chaos (network issues)
Run perftest while randomly killing/restarting containers:
```bash
# In one terminal:
DURATION=10m CONCURRENCY=30 ./scripts/run-perftest.sh

# In another:
while true; do
  sleep $((RANDOM % 30 + 10))
  docker restart arteria-processing
done
```
Validates zero message loss under failure conditions.

### Scenario 5: Tunnel Stress (Capillary throughput)
Run from the Azure VM through the tunnel:
```bash
ssh arteriaadmin@52.188.66.170
DURATION=5m CONCURRENCY=40 TARGET_HOST=localhost PORTS=2575,2576,2577,2578 ./perftest
```
Measures real cross-internet throughput through mTLS tunnel.

---

## Cloud-Native Architecture (Managed Services)

Replace self-managed components with cloud-native equivalents for maximum performance
with minimal operations overhead. Trade higher cost for zero-ops scaling.

---

### Azure Cloud-Native Architecture

```
┌────────────────────────────────────────────────────────────────────────────────┐
│                              Azure Region (East US)                              │
│                                                                                  │
│  ┌───────────────────────────────────────────────────────────────────────────┐  │
│  │  Azure Front Door (Global L7 LB + WAF + TLS)                             │  │
│  │  - Auto TLS cert management                                               │  │
│  │  - DDoS protection included                                               │  │
│  │  - Geo-routing for multi-region                                            │  │
│  └─────────────────────────────────┬─────────────────────────────────────────┘  │
│                                    │                                             │
│  ┌─────────────────────────────────▼─────────────────────────────────────────┐  │
│  │  Azure Container Apps (Serverless Containers)                              │  │
│  │                                                                            │  │
│  │  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐ ┌──────────────────┐ │  │
│  │  │  Ingestion   │ │  Processing  │ │  API         │ │  Frontend (SSR)  │ │  │
│  │  │  min:1 max:8 │ │  min:2 max:20│ │  min:1 max:4 │ │  min:1 max:2     │ │  │
│  │  │  KEDA: TCP   │ │  KEDA: NATS  │ │  KEDA: HTTP  │ │  KEDA: HTTP      │ │  │
│  │  └──────┬───────┘ └──────┬───────┘ └──────┬───────┘ └──────────────────┘ │  │
│  │         │                 │                 │                               │  │
│  └─────────┼─────────────────┼─────────────────┼───────────────────────────────┘  │
│            │                 │                 │                                   │
│  ┌─────────▼─────────────────▼─────────────────▼───────────────────────────────┐  │
│  │  Azure Event Hubs (replaces NATS JetStream)                                 │  │
│  │  - 1 TU = 1MB/s in, 2MB/s out = ~1200 msgs/sec                            │  │
│  │  - Auto-inflate to 20 TU for burst                                          │  │
│  │  - 7-day retention built-in                                                 │  │
│  │  - Kafka protocol compatible                                                │  │
│  └─────────────────────────────────┬───────────────────────────────────────────┘  │
│                                    │                                              │
│  ┌─────────────────────────────────▼───────────────────────────────────────────┐  │
│  │  Azure Cosmos DB (Cassandra API) — replaces ScyllaDB                        │  │
│  │  - Serverless mode: pay per RU (request unit)                               │  │
│  │  - Auto-scale 0 → 4000 RU/s                                                │  │
│  │  - Multi-region replication with single click                               │  │
│  │  - TTL per-item (message retention built-in)                                │  │
│  │  - 99.999% SLA with multi-region                                            │  │
│  └─────────────────────────────────────────────────────────────────────────────┘  │
│                                                                                   │
│  ┌─────────────┐  ┌──────────────────┐  ┌──────────────────────────────────────┐ │
│  │  Azure KV   │  │  Azure Monitor   │  │  Aorta Broker (Container App)       │ │
│  │  Secrets    │  │  + App Insights  │  │  - Dedicated plan (always-on)       │ │
│  │  JWT keys   │  │  Distributed     │  │  - TCP ingress on :9443             │ │
│  │  TLS certs  │  │  tracing         │  │  - Connects to Event Hubs           │ │
│  └─────────────┘  └──────────────────┘  └──────────────────────────────────────┘ │
└───────────────────────────────────────────────────────────────────────────────────┘
```

**Azure Service Mapping:**

| Arteria Component | Azure Managed Service | Why |
|---|---|---|
| NATS JetStream | **Azure Event Hubs** | Infinite scale, no ops, Kafka compat, capture to Blob |
| ScyllaDB | **Cosmos DB (Cassandra API)** | Same CQL queries work, serverless pricing, global replication |
| Docker Compose | **Container Apps** | Serverless, KEDA auto-scale, pay-per-use, zero k8s management |
| Caddy | **Azure Front Door** | Global CDN, WAF, DDoS, managed TLS, $0.01/10K requests |
| Config backup | **Azure Blob Storage** | $0.02/GB/month, immutable backups, lifecycle policies |
| Secrets | **Azure Key Vault** | HSM-backed, audit trail, managed rotation |
| Monitoring | **Application Insights** | Distributed tracing, live metrics, smart alerts |
| Log shipping | **Log Analytics** | KQL queries, 30-day free retention, dashboards |

**Cost estimate (10M msgs/day):**
- Container Apps: ~$80/month (consumption plan, auto-scale to zero overnight)
- Event Hubs: ~$100/month (2 TU standard, auto-inflate)
- Cosmos DB: ~$150/month (serverless, ~400 RU/s average)
- Front Door: ~$40/month
- Key Vault + Monitor: ~$30/month
- **Total: ~$400/month** (fully managed, zero ops, auto-scaling)

**Performance gains:**
- Event Hubs: 1M msgs/sec throughput ceiling (vs self-managed NATS needing tuning)
- Cosmos DB: <10ms P99 write latency globally (vs self-managed Scylla needing compaction tuning)
- Container Apps: Scale to 20 replicas in seconds during burst (vs manual scaling)
- Front Door: Edge caching for API reads, 200+ PoPs globally

**Code changes required:**
1. Replace `natsutil` with Event Hubs SDK (or use Kafka protocol — NATS client stays, add Kafka bridge)
2. Replace `scyllautil` connection string (Cosmos Cassandra API is wire-compatible — just change endpoint)
3. Add App Insights SDK for distributed tracing
4. Container Apps YAML instead of docker-compose for deploy

---

### AWS Cloud-Native Architecture

```
┌────────────────────────────────────────────────────────────────────────────────┐
│                              AWS Region (us-east-1)                              │
│                                                                                  │
│  ┌───────────────────────────────────────────────────────────────────────────┐  │
│  │  CloudFront + ALB (TLS termination + WAF)                                 │  │
│  └─────────────────────────────────┬─────────────────────────────────────────┘  │
│                                    │                                             │
│  ┌─────────────────────────────────▼─────────────────────────────────────────┐  │
│  │  ECS Fargate (Serverless Containers)                                       │  │
│  │                                                                            │  │
│  │  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐ ┌──────────────────┐ │  │
│  │  │  Ingestion   │ │  Processing  │ │  API         │ │  Frontend        │ │  │
│  │  │  Service     │ │  Service     │ │  Service     │ │  Service         │ │  │
│  │  │  (auto-scale)│ │  (auto-scale)│ │  (auto-scale)│ │                  │ │  │
│  │  └──────┬───────┘ └──────┬───────┘ └──────┬───────┘ └──────────────────┘ │  │
│  └─────────┼─────────────────┼─────────────────┼───────────────────────────────┘  │
│            │                 │                 │                                   │
│  ┌─────────▼─────────────────▼─────────────────▼───────────────────────────────┐  │
│  │  Amazon MSK Serverless (Kafka — replaces NATS)                              │  │
│  │  - Pay per data in/out                                                      │  │
│  │  - Auto-scales partitions                                                   │  │
│  │  - No cluster management                                                   │  │
│  └─────────────────────────────────┬───────────────────────────────────────────┘  │
│                                    │                                              │
│  ┌─────────────────────────────────▼───────────────────────────────────────────┐  │
│  │  Amazon Keyspaces (Cassandra-compatible — replaces ScyllaDB)                │  │
│  │  - Serverless: pay per read/write                                           │  │
│  │  - Same CQL, same schema, same queries                                     │  │
│  │  - Auto-scales capacity                                                    │  │
│  │  - TTL per-row                                                              │  │
│  │  - Multi-region with global tables                                          │  │
│  └─────────────────────────────────────────────────────────────────────────────┘  │
│                                                                                   │
│  ┌─────────────┐  ┌──────────────────┐  ┌──────────────────────────────────────┐ │
│  │  Secrets    │  │  CloudWatch +    │  │  Aorta Broker (Fargate + NLB)       │ │
│  │  Manager    │  │  X-Ray           │  │  - NLB for TCP :9443                │ │
│  │             │  │  (tracing)       │  │  - Always-on task                   │ │
│  └─────────────┘  └──────────────────┘  └──────────────────────────────────────┘ │
└───────────────────────────────────────────────────────────────────────────────────┘
```

**AWS Service Mapping:**

| Arteria Component | AWS Managed Service | Why |
|---|---|---|
| NATS JetStream | **MSK Serverless** | Zero ops Kafka, auto-scale, pay-per-GB |
| ScyllaDB | **Amazon Keyspaces** | CQL-compatible, serverless, same schema works |
| Docker Compose | **ECS Fargate** | Serverless containers, no EC2 management |
| Caddy | **ALB + CloudFront** | Managed TLS, WAF, edge caching |
| Config backup | **S3** | $0.023/GB, versioning, lifecycle rules |
| Secrets | **Secrets Manager** | Auto-rotation, audit trail |
| Monitoring | **CloudWatch + X-Ray** | Logs, metrics, distributed tracing |
| Tunnel (Aorta) | **NLB + Fargate** | TCP passthrough, static IP for Capillaries |

**Cost estimate (10M msgs/day):**
- ECS Fargate: ~$100/month (0.25 vCPU tasks, scale based on traffic)
- MSK Serverless: ~$80/month (data throughput charges)
- Keyspaces: ~$120/month (on-demand, ~400 WCU avg)
- ALB + CloudFront: ~$50/month
- NLB (Aorta): ~$25/month
- Secrets + CloudWatch: ~$25/month
- **Total: ~$400/month** (fully managed, auto-scaling)

---

### Deep Dive: Cosmos DB Throughput — Is It Enough?

**Short answer: Yes, with caveats.**

#### Cosmos DB Cassandra API — Throughput Reality

| Mode | Max Throughput | Cost | Best For |
|---|---|---|---|
| Serverless | 5,000 RU/s burst | Pay per request | Dev, < 1M msgs/day |
| Autoscale Provisioned | 1,000 → 100,000 RU/s | 1.5x provisioned cost | Production, variable load |
| Manual Provisioned | Up to 1,000,000 RU/s | Cheapest per RU at scale | High steady-state |

**RU cost per Arteria operation:**
- Insert message (1KB avg): ~10 RU
- Insert message_by_patient index: ~5 RU
- Insert message_by_status index: ~5 RU
- Read single message: ~5 RU
- List last 100 messages: ~50 RU

**So per message processed: ~20 RU (write) + occasional reads**

**Throughput math:**
| Messages/day | msgs/sec | RU/s needed | Cosmos Mode | Monthly Cost |
|---|---|---|---|---|
| 1M | 12 | 240 RU/s | Serverless | ~$50 |
| 5M | 58 | 1,160 RU/s | Autoscale (max 2K) | ~$120 |
| 10M | 116 | 2,320 RU/s | Autoscale (max 4K) | ~$200 |
| 25M | 290 | 5,800 RU/s | Autoscale (max 10K) | ~$450 |
| 50M | 580 | 11,600 RU/s | Provisioned 15K | ~$600 |
| 100M | 1,157 | 23,000 RU/s | Provisioned 30K | ~$1,100 |

**Cosmos DB limits that could bite you:**
1. **Single partition limit: 10,000 RU/s** — Arteria uses `message_id` as PK (UUID = perfect distribution). No hotspot risk.
2. **Item size limit: 2MB** — HL7 messages are typically 0.5-10KB. No issue.
3. **Cassandra API limitations**: No `ALLOW FILTERING` (need proper indexes), no lightweight transactions on non-PK columns.
4. **Latency**: P50 = 5ms, P99 = 15ms for writes. Slightly slower than local ScyllaDB (P50 = 1ms) but still well within requirements.
5. **TTL**: Native per-item TTL works identically to ScyllaDB. Message retention is handled automatically.

**Verdict: Cosmos DB handles up to 100M msgs/day without breaking a sweat.**
Above that, you'd use dedicated Cosmos capacity (unlimited) or stick with self-managed ScyllaDB cluster.

**The real question: Do you even need 100M msgs/day?**
- Largest health system in Australia: ~10M HL7 msgs/day
- NHS England (all trusts combined): ~50M msgs/day
- Average mid-size hospital: 500K-2M msgs/day

**Cosmos DB is overkill for almost every customer. That's the point — it never becomes a bottleneck.**

#### Schema changes needed for Cosmos Cassandra API:

```cql
-- The only change: use RU-based throughput instead of tablets
CREATE KEYSPACE arteria WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1};

-- All existing CREATE TABLE statements work unchanged
-- Cosmos auto-creates secondary indexes
-- TTL works via DEFAULT_TIME_TO_LIVE or per-row TTL
```

Code change in `scyllautil/scylla.go`:
```go
// Just change the connection config:
cfg := gocql.NewCluster("arteria-cosmos.cassandra.cosmos.azure.com")
cfg.Port = 10350
cfg.Authenticator = gocql.PasswordAuthenticator{
    Username: cosmosAccountName,
    Password: cosmosKey,
}
cfg.SslOpts = &gocql.SslOptions{EnableHostVerification: false}
cfg.ConnectTimeout = 10 * time.Second
// Everything else stays the same — same queries, same schema
```

---

### Deep Dive: NATS Deployment Options

NATS is Arteria's backbone — it handles all inter-service messaging, JetStream persistence, and tunnel routing. Here's how to deploy it properly in each environment:

#### Option 1: NATS in Container Apps / Fargate (Simplest)

```
┌─────────────────────────────────────────────────────────┐
│  Container Apps Environment (dedicated plan)             │
│                                                          │
│  ┌──────────────────────────────────────────────────┐   │
│  │  NATS Container (always-on, dedicated plan)       │   │
│  │  - 2 vCPU, 4GB RAM                               │   │
│  │  - Azure Files volume for JetStream persistence   │   │
│  │  - Internal-only TCP ingress (:4222)              │   │
│  │  - No scale-to-zero (must be always running)      │   │
│  └──────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
```

**Pros:** Simple, single container, works today
**Cons:** Single point of failure, max 4GB memory, no clustering
**Cost:** ~$60/month (dedicated plan, 2 vCPU always-on)
**Capacity:** ~5M msgs/day (limited by memory for JetStream)
**Best for:** Tier 1 (single customer, non-critical)

#### Option 2: NATS on AKS/EKS (Production — Recommended)

```
┌──────────────────────────────────────────────────────────────────┐
│  AKS Cluster (3 nodes: Standard_D2ds_v5)                         │
│                                                                   │
│  ┌────────────────┐  ┌────────────────┐  ┌────────────────┐     │
│  │  NATS Pod 1    │  │  NATS Pod 2    │  │  NATS Pod 3    │     │
│  │  (JetStream)   │  │  (JetStream)   │  │  (JetStream)   │     │
│  │  R=3 streams   │  │  R=3 streams   │  │  R=3 streams   │     │
│  │  PVC: 50GB SSD │  │  PVC: 50GB SSD │  │  PVC: 50GB SSD │     │
│  └────────┬───────┘  └────────┬───────┘  └────────┬───────┘     │
│           │                    │                    │              │
│           └────── NATS Cluster (Raft) ─────────────┘              │
│                          │                                        │
│  ┌───────────────────────▼───────────────────────────────────┐   │
│  │  Other Arteria pods (processing, ingestion, api, aorta)   │   │
│  │  Connect to nats://nats:4222 (cluster-internal)           │   │
│  └───────────────────────────────────────────────────────────┘   │
│                                                                   │
│  Deployed via: Helm chart (nats/nats) or NATS Operator           │
└──────────────────────────────────────────────────────────────────┘
```

**Deploy with Helm:**
```bash
helm repo add nats https://nats-io.github.io/k8s/helm/charts/
helm install nats nats/nats \
  --set nats.jetstream.enabled=true \
  --set nats.jetstream.memStorage.size=2Gi \
  --set nats.jetstream.fileStorage.size=50Gi \
  --set nats.jetstream.fileStorage.storageClassName=managed-premium \
  --set cluster.enabled=true \
  --set cluster.replicas=3 \
  --set nats.resources.requests.cpu=500m \
  --set nats.resources.requests.memory=1Gi \
  --set nats.resources.limits.cpu=2000m \
  --set nats.resources.limits.memory=4Gi
```

**Pros:** HA (tolerates 1 node failure), auto-recovery, proven at scale
**Cons:** Needs AKS cluster ($200+ for 3 nodes), operational knowledge
**Cost:** ~$200/month (AKS nodes) + ~$30/month (PVC storage)
**Capacity:** 25M+ msgs/day per cluster, horizontal scaling via super-clusters
**Best for:** Tier 2/3 production deployments

#### Option 3: NATS on Dedicated VMs (Maximum Performance)

```
┌───────────────────────────────────────────────────────────────┐
│  3x Standard_E4ds_v5 (4 vCPU, 32GB RAM, NVMe temp disk)      │
│                                                                │
│  VM 1: NATS node 1     VM 2: NATS node 2     VM 3: NATS 3    │
│  JetStream: 16GB RAM   JetStream: 16GB RAM    JetStream: 16GB │
│  File store: NVMe      File store: NVMe        File store: NVMe│
│  Raft leader/follower  Raft leader/follower   Raft leader/fol │
│                                                                │
│  Connected via: Azure VNet peering (10Gbps between VMs)        │
│  No load balancer needed — clients discover via DNS/seed list  │
└───────────────────────────────────────────────────────────────┘
```

**Pros:** Maximum throughput (10M+ msgs/sec), full control, predictable latency
**Cons:** Most operational overhead, manual failover recovery
**Cost:** ~$300/month (3x E4ds reserved) + storage
**Capacity:** 100M+ msgs/day, limited only by disk I/O
**Best for:** Extreme throughput requirements, multi-tenant platform

#### NATS Performance Benchmarks (real-world):

| Config | Msgs/sec (publish) | Msgs/sec (consume) | Latency P99 |
|---|---|---|---|
| 1 node, memory storage | 1,200,000 | 2,000,000 | 0.3ms |
| 1 node, file storage | 400,000 | 1,500,000 | 1.2ms |
| 3 node cluster, R=3 | 200,000 | 1,000,000 | 2.5ms |
| 3 node cluster, R=1 | 800,000 | 1,500,000 | 0.8ms |

**Arteria's usage pattern:**
- Publish: ~200 msgs/sec (ingestion → stream)
- Consumer: ~200 msgs/sec (processing pulls from stream)
- NATS request/reply: ~50 req/sec (metrics, playground, tunnel routing)

**Even a single NATS node in memory mode handles 6000x our requirement.**
Clustering is for resilience, not throughput.

#### Recommended NATS Configuration for Each Tier:

| Tier | NATS Deploy | Storage | Replicas | Capacity |
|---|---|---|---|---|
| Single node | Container (Docker/CA) | Memory (RAM) | 1 | 5M msgs/day |
| Resilient pair | 2 containers (Container Apps) | Azure Files | R=2 | 15M msgs/day |
| AKS cluster | Helm chart, 3 pods | Premium SSD PVC | R=3 | 50M+ msgs/day |
| Dedicated VMs | Bare metal NATS | Local NVMe | R=3 | 100M+ msgs/day |

---

### Hybrid Architecture (Recommended Production)

```
┌────────────────────────────────────────────────────────────────────────────┐
│  AKS Cluster (3 nodes: Standard_D4ds_v5, 1 spot node pool for processing) │
│                                                                             │
│  System Node Pool (always-on, 2 nodes):                                     │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │  NATS (3 replicas, Helm chart, JetStream R=3, Premium SSD PVCs)     │  │
│  │  Ingestion (2 replicas, host-port :2575)                            │  │
│  │  API (2 replicas, behind Internal LB)                               │  │
│  │  Aorta broker (1 replica, host-port :9443)                          │  │
│  │  Frontend (1 replica)                                                │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
│  Spot Node Pool (scale 0-10, evictable):                                    │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │  Processing (2-20 replicas, HPA on NATS stream pending msgs)        │  │
│  │  Tolerations: azure.com/spot=true                                   │  │
│  │  If evicted: NATS re-delivers unacked messages, new pod picks up    │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
│  External (Managed Services):                                               │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │  Cosmos DB (Cassandra API, Autoscale 1K-10K RU/s)                   │  │
│  │  Azure Front Door (TLS + WAF + CDN)                                 │  │
│  │  Azure Key Vault (secrets)                                          │  │
│  │  Azure Monitor + App Insights (observability)                       │  │
│  │  Azure Blob (config backups, message archive)                       │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────────────────┘
```

**Why this is the best architecture:**

1. **NATS stays in-cluster** — It's already 6000x faster than needed. Running it in AKS with PVCs gives HA + persistence with zero external dependency. No network hop to a managed service.

2. **Cosmos DB for storage** — Eliminates ScyllaDB ops entirely. Auto-scales, auto-backups, TTL works, same CQL. The 15ms P99 latency is invisible in an async pipeline (messages flow through NATS, not Cosmos).

3. **Spot instances for processing** — V8 filter execution is stateless. If Azure evicts a spot VM, NATS simply re-delivers the message to another processing pod. Zero message loss, 60-90% cheaper compute.

4. **NATS-native scaling** — Processing pods use NATS consumer groups. Add pods → automatic load balancing. Remove pods → remaining pods pick up slack. No partition rebalancing (unlike Kafka).

5. **Aorta broker is always-on** — Capillary agents maintain persistent connections. Can't be on spot instances or scale-to-zero. Dedicated node affinity.

**Cost breakdown:**
- AKS cluster (2 system D4ds + 1 spot D4ds): ~$220/month
- Cosmos DB (autoscale 4K RU): ~$200/month
- Front Door: ~$40/month
- Key Vault + Monitor + Blob: ~$40/month
- **Total: ~$500/month** for 25M msgs/day with full HA

---

### Decision Matrix: When to Use What

| Scenario | Recommended | Reason |
|---|---|---|
| PoC / Demo | Docker Compose on single VM | Cheapest, fastest to deploy |
| Single hospital (< 5M/day) | Tier 1 VM | $150/month, simple, sufficient |
| Multi-site health system | Hybrid (AKS + Cosmos) | Balance of cost, ops, resilience |
| Enterprise with SLA | Full cloud-native (Azure/AWS) | Zero-ops, auto-scale, 99.99% SLA |
| Cost-sensitive at scale | Self-managed on Hetzner/OVH | 5-10x cheaper than cloud, more ops |
| Regulated (HIPAA/HITRUST) | Azure (Container Apps + Cosmos) | Compliance certifications inherited |

---

### Performance Comparison (10M msgs/day workload)

| Metric | Self-Managed (VM) | Hybrid (AKS+Cosmos) | Full Cloud-Native |
|---|---|---|---|
| Throughput | 200 msgs/sec | 500 msgs/sec | 1000+ msgs/sec |
| P99 Latency (end-to-end) | 15ms | 20ms | 25ms |
| P99 Latency (NATS only) | 0.5ms | 2ms | N/A (Event Hubs ~50ms) |
| P99 Latency (DB write) | 2ms (ScyllaDB) | 15ms (Cosmos) | 15ms (Cosmos) |
| Scale-up time | Manual (minutes) | 30 seconds (HPA) | 5 seconds (KEDA) |
| Failover time | Manual / minutes | 30 seconds | Automatic / seconds |
| Max burst absorption | 2x (RAM limited) | 10x (NATS file store) | 100x (Event Hubs) |
| Ops burden | High (you manage everything) | Medium (manage NATS + apps) | Low (manage code only) |
| Monthly cost | $150-400 | $400-600 | $500-800 |
| Compliance | Self-attested | Inherited (AKS) | Inherited (PaaS) |

**Key insight on latency:** The end-to-end latency for Arteria is dominated by V8 filter execution (~5ms) and DB writes. NATS adds <1ms in all configurations. Replacing NATS with Event Hubs adds ~50ms latency per message hop — this is why keeping NATS is the right call unless you need multi-region streaming.

---

### Quick Migration Path

**Phase 1** (Current): Docker Compose on VM ✓
**Phase 2** (Next): Move ScyllaDB → Cosmos DB (Cassandra API)
- Change connection string in `scyllautil`
- Same CQL schema works (tested)
- Delete ScyllaDB container
- Gain: auto-scaling, backup, multi-region, no disk management
- Effort: 1 day

**Phase 3**: Move to AKS with NATS Helm chart
- Create AKS cluster (3 nodes)
- Deploy NATS via Helm (5 minutes)
- Convert docker-compose to Kubernetes manifests (Kompose or manual)
- Add HPA for processing (scale on NATS pending messages)
- Add spot node pool for processing
- Gain: HA, auto-healing, burst scaling, spot pricing
- Effort: 2-3 days

**Phase 4** (Optional): Replace NATS with Event Hubs
- Only if you need multi-region event streaming or >50M msgs/sec
- NATS single node already does 1.2M msgs/sec — you won't hit this limit
- Event Hubs adds latency (50ms vs 0.5ms) — worse for real-time HL7
- **Recommendation: Don't do this. NATS is better for this workload.**

---
---

## Delivery Models: SaaS vs Ship-to-Customer

Two distinct revenue models, each with different architecture, security, and operational requirements.

---

### Model 1: Ship & Deploy (Single-Tenant per Customer)

Each customer gets their own isolated Arteria instance — either self-hosted or managed by us.

#### Option A: Customer Self-Deploy

Package Arteria as a turnkey appliance. Customer runs it in their own cloud/on-prem.

**Delivery artifact:**
```
arteria-v1.0.0/
├── docker-compose.yml          # Single command to start
├── .env.example                # All configurable knobs
├── infra/
│   ├── cql/                    # Schema auto-applied
│   └── caddy/                  # TLS config
├── scripts/
│   ├── install.sh              # curl | bash one-liner
│   └── upgrade.sh              # Rolling upgrade
├── dist/
│   └── capillary-*             # Pre-built agent binaries
└── LICENSE
```

**install.sh (one-liner deploy):**
```bash
#!/bin/bash
# curl -sSL https://get.arteria.software | bash
set -e
DOMAIN="${1:?Usage: install.sh <domain> [email]}"
EMAIL="${2:-admin@$DOMAIN}"

git clone --depth 1 https://github.com/r2l332/arteria.sofware.git /opt/arteria
cd /opt/arteria

cat > .env <<EOF
DOMAIN=$DOMAIN
TLS_EMAIL=$EMAIL
ADMIN_PASS=$(openssl rand -base64 12)
JWT_SECRET=$(openssl rand -hex 32)
EOF

docker compose up -d

echo "Arteria deployed at https://$DOMAIN"
echo "Login: admin / $(grep ADMIN_PASS .env | cut -d= -f2)"
echo "You will be prompted to change your password on first login."
```

**Customer requirements:**
- Docker Engine + Docker Compose
- 4 vCPU, 16GB RAM minimum
- Ports 80, 443, 2575, 9443
- DNS record pointing to their server

**Upgrade path:**
```bash
cd /opt/arteria
git pull origin main
docker compose up -d --build
# Schema migrations auto-apply via scylla-init container
# NATS streams persist across restarts
# Zero downtime (rolling restart)
```

**Pros:** Customer owns data, no compliance questions, works air-gapped
**Cons:** Support overhead, version fragmentation, no visibility into issues

#### Option B: Managed Deployment (We Deploy in Their Cloud)

We provision and manage the infrastructure in the customer's Azure/AWS subscription.

**Deployment automation (Terraform/Bicep):**
```
Per customer:
  1x AKS cluster (3 nodes) — or single VM for smaller
  1x Cosmos DB account (Cassandra API)
  1x Azure Front Door (custom domain + TLS)
  1x Key Vault (customer-specific secrets)
  1x Log Analytics workspace
  GitOps: ArgoCD or Flux syncs from a customer-specific branch
```

**Access model:**
- We get Contributor access to a dedicated resource group
- Customer's IT team gets Reader access + alert notifications
- Break-glass: our SRE team can SSH only via Azure Bastion (audited)

**Pros:** Full control, customer data stays in their subscription, premium pricing
**Cons:** Operational overhead per customer, doesn't scale to 100 customers

---

### Model 2: Multi-Tenant SaaS Platform

Single Arteria platform serving multiple customers, with strict tenant isolation.

#### Tenant Isolation Architecture

```
┌────────────────────────────────────────────────────────────────────────────────┐
│                        Arteria SaaS Platform                                    │
│                                                                                 │
│  ┌──────────────────────────────────────────────────────────────────────────┐  │
│  │  Azure Front Door                                                        │  │
│  │  *.arteria.cloud                                                         │  │
│  │  hospital-a.arteria.cloud → tenant: hospital-a                           │  │
│  │  labcorp.arteria.cloud    → tenant: labcorp                              │  │
│  │  custom.hospital-b.com    → tenant: hospital-b (CNAME)                   │  │
│  └──────────────────────────────┬───────────────────────────────────────────┘  │
│                                 │                                              │
│  ┌──────────────────────────────▼───────────────────────────────────────────┐  │
│  │  API Gateway / Tenant Router                                              │  │
│  │  - Extract tenant from subdomain or JWT claim                             │  │
│  │  - Inject X-Tenant-ID header into all downstream requests                │  │
│  │  - Rate limit per tenant                                                  │  │
│  │  - WAF rules per tenant tier                                              │  │
│  └──────────────────────────────┬───────────────────────────────────────────┘  │
│                                 │                                              │
│  ┌──────────────────────────────▼───────────────────────────────────────────┐  │
│  │  Shared Compute (AKS)                                                     │  │
│  │                                                                           │  │
│  │  ┌─────────────┐ ┌──────────────┐ ┌─────────────┐ ┌─────────────────┐  │  │
│  │  │  Ingestion  │ │  Processing  │ │  API        │ │  Frontend       │  │  │
│  │  │  (shared)   │ │  (shared)    │ │  (shared)   │ │  (shared)       │  │  │
│  │  │  tenant-    │ │  tenant-     │ │  tenant-    │ │  tenant-aware   │  │  │
│  │  │  aware      │ │  aware       │ │  scoped     │ │  theming        │  │  │
│  │  └──────┬──────┘ └──────┬───────┘ └──────┬──────┘ └─────────────────┘  │  │
│  │         │                │                │                              │  │
│  └─────────┼────────────────┼────────────────┼──────────────────────────────┘  │
│            │                │                │                                  │
│  ┌─────────▼────────────────▼────────────────▼──────────────────────────────┐  │
│  │  NATS JetStream (shared cluster, tenant-prefixed subjects)               │  │
│  │  Streams: {tenant}.ingest.>, {tenant}.route.>, {tenant}.dlq.>           │  │
│  │  Each tenant's messages are isolated by subject prefix                    │  │
│  └──────────────────────────────┬───────────────────────────────────────────┘  │
│                                 │                                              │
│  DATA ISOLATION (per tenant):                                                  │
│  ┌──────────────────────────────▼───────────────────────────────────────────┐  │
│  │  Option A: Shared Cosmos DB, separate keyspace per tenant                │  │
│  │  ┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐           │  │
│  │  │ arteria_hosp_a  │ │ arteria_labcorp │ │ arteria_hosp_b  │           │  │
│  │  │  messages       │ │  messages       │ │  messages       │           │  │
│  │  │  routes         │ │  routes         │ │  routes         │           │  │
│  │  │  users          │ │  users          │ │  users          │           │  │
│  │  └─────────────────┘ └─────────────────┘ └─────────────────┘           │  │
│  │                                                                         │  │
│  │  Option B: Separate Cosmos DB account per tenant (premium tier)          │  │
│  │  - Complete physical isolation                                          │  │
│  │  - Independent scaling per customer                                     │  │
│  │  - Customer can have their own encryption key (CMK)                     │  │
│  └─────────────────────────────────────────────────────────────────────────┘  │
│                                                                                 │
│  ┌─────────────────────────────────────────────────────────────────────────┐   │
│  │  Per-Tenant Resources:                                                   │  │
│  │  - Aorta broker: shared (tenant ID in cert CN for routing)              │  │
│  │  - Capillary certs: signed with tenant-specific intermediate CA          │  │
│  │  - Encryption keys: per-tenant Key Vault keys (envelope encryption)     │  │
│  │  - Audit log: per-tenant, immutable, 7-year retention                   │  │
│  └─────────────────────────────────────────────────────────────────────────┘   │
└────────────────────────────────────────────────────────────────────────────────┘
```

#### Security: Data Isolation Layers

This is the critical part. Healthcare data must **never** cross tenant boundaries.

**Layer 1: Network Isolation**
```
┌─────────────────────────────────────────┐
│ Every request carries tenant context:    │
│                                          │
│ 1. Subdomain → tenant mapping            │
│ 2. JWT token contains tenant_id claim    │
│ 3. API middleware validates tenant_id    │
│    matches the subdomain                 │
│ 4. All DB queries scoped by keyspace     │
│ 5. All NATS subjects prefixed by tenant  │
│ 6. Logs tagged with tenant_id            │
│                                          │
│ Cross-tenant request = IMPOSSIBLE        │
│ (different keyspace, different subjects) │
└─────────────────────────────────────────┘
```

**Layer 2: Data Isolation (Cosmos DB)**

| Tier | Isolation | How | Security Level |
|---|---|---|---|
| Standard | Keyspace per tenant | `arteria_{tenant_id}.*` | Logical (shared account) |
| Premium | Cosmos account per tenant | Separate connection string | Physical (separate account) |
| Sovereign | Cosmos in customer's subscription | Cross-subscription link | Customer-controlled |

**Standard tier implementation:**
```go
// Tenant context flows through every request
type TenantContext struct {
    TenantID    string
    Keyspace    string // "arteria_" + tenantID
    NATSPrefix  string // tenantID + "."
}

// Middleware extracts tenant from JWT
func TenantMiddleware() fiber.Handler {
    return func(c *fiber.Ctx) error {
        claims := c.Locals("claims").(*auth.Claims)
        tenant := &TenantContext{
            TenantID:   claims.TenantID,
            Keyspace:   "arteria_" + claims.TenantID,
            NATSPrefix: claims.TenantID + ".",
        }
        c.Locals("tenant", tenant)
        return c.Next()
    }
}

// Every DB query uses tenant keyspace
func (s *Store) GetMessages(tenant *TenantContext, limit int) ([]Message, error) {
    query := fmt.Sprintf("SELECT * FROM %s.messages LIMIT ?", tenant.Keyspace)
    // SQL injection safe: keyspace is derived from JWT, not user input
    // Keyspace name is alphanumeric + underscore only (validated at provisioning)
    return s.session.Query(query, limit).Iter()...
}

// Every NATS publish uses tenant prefix
func (p *Publisher) Publish(tenant *TenantContext, subject string, data []byte) error {
    return p.js.Publish(tenant.NATSPrefix + subject, data)
}
```

**Layer 3: Encryption**

| Data | At Rest | In Transit | Key Management |
|---|---|---|---|
| Messages (PHI) | AES-256 (Cosmos SSE) | TLS 1.3 | Per-tenant CMK in Key Vault |
| Config (routes, filters) | AES-256 | TLS 1.3 | Shared platform key |
| Tunnel traffic | N/A | mTLS (ECDSA P-256) | Per-tenant intermediate CA |
| Backups | AES-256 (Blob SSE) | TLS 1.3 | Per-tenant CMK |
| Audit logs | AES-256 (immutable) | TLS 1.3 | Platform key (tamper-proof) |

**Premium tier: per-tenant encryption keys:**
```
Platform Key Vault
├── tenant-hosp-a/
│   ├── data-encryption-key     (encrypts PHI at rest)
│   ├── jwt-signing-key         (signs auth tokens)
│   └── tunnel-ca-key           (signs Capillary certs)
├── tenant-labcorp/
│   ├── data-encryption-key
│   ├── jwt-signing-key
│   └── tunnel-ca-key
```

**Layer 4: Access Control**

```
Platform roles (our team):
  platform-admin     → Can provision/deprovision tenants, view platform metrics
  platform-sre       → Can access tenant infra for support (audit-logged)
  platform-support   → Can view tenant config (NOT messages), respond to tickets

Tenant roles (customer's team):
  tenant-admin       → Full control within their tenant
  tenant-developer   → Routes, filters, CPs, playground
  tenant-operator    → View only, CP logs, metrics
  tenant-security    → Audit logs, user management, config
  tenant-viewer      → Read-only dashboard

CRITICAL: Platform roles CANNOT read tenant PHI.
Tenant roles CANNOT see other tenants.
Every PHI access is logged in an immutable audit trail.
```

**Layer 5: Audit & Compliance**

```
Per-tenant audit log (immutable, append-only):
- Every login (success + failure)
- Every PHI access (who viewed which message)
- Every config change (routes, filters, CPs)
- Every user management action
- Every Capillary enrollment/connection
- Retention: 7 years (HIPAA requirement)
- Storage: Azure Blob (immutable + WORM policy)
- Format: JSON Lines, exportable for compliance audits
```

#### White-Label / Custom Branding

Customers see their own brand, not Arteria.

**Implementation:**

```
Tenant config (stored in Cosmos):
{
  "tenant_id": "hospital-a",
  "branding": {
    "app_name": "HealthConnect",
    "logo_url": "https://hospital-a.arteria.cloud/assets/logo.svg",
    "primary_color": "#1a5276",
    "accent_color": "#2ecc71",
    "favicon_url": "https://hospital-a.arteria.cloud/assets/favicon.ico",
    "support_email": "support@hospital-a.com",
    "custom_domain": "integration.hospital-a.com"
  }
}
```

**Frontend changes needed:**
```typescript
// Layout fetches branding from /api/v1/tenant/branding (public endpoint)
// Returns colors, logo, app name based on subdomain

// Sidebar brand area:
<div className="px-5 py-5">
  <img src={branding.logo_url} alt={branding.app_name} />
  <h1>{branding.app_name}</h1>
</div>

// CSS variables set from branding config:
:root {
  --arteria-accent: ${branding.primary_color};
  --arteria-surface: ${branding.surface_color};
}
```

**Custom domains:**
```
Customer:  integration.hospital-a.com  →  CNAME to hospital-a.arteria.cloud
Front Door: Routes based on hostname → injects tenant_id
TLS: Front Door auto-provisions cert for custom domain
```

#### Multi-Tenant SaaS Cost Model

**Platform infrastructure (fixed cost, shared):**
- AKS cluster (5 nodes): ~$500/month
- NATS cluster (3 pods): included in AKS
- Front Door: ~$50/month
- Key Vault: ~$10/month
- Monitoring: ~$50/month
- **Platform base: ~$610/month**

**Per-tenant marginal cost:**
| Tier | Cosmos | Storage | Capillaries | Marginal Cost |
|---|---|---|---|---|
| Starter (1M msgs/day) | Serverless ~$30 | ~$5 | 2 | ~$35/month |
| Professional (10M msgs/day) | Autoscale ~$150 | ~$20 | 10 | ~$170/month |
| Enterprise (50M msgs/day) | Dedicated ~$500 | ~$50 | Unlimited | ~$550/month |

**Break-even math:**
- 10 Starter tenants: $610 + (10 × $35) = $960/month infra, charge $500/tenant = $5,000 revenue
- 5 Professional tenants: $610 + (5 × $170) = $1,460/month infra, charge $1,500/tenant = $7,500 revenue
- Mix of 20 tenants: ~$2,500/month infra → charge $15,000-30,000/month

**Margin: 80-90% at scale.** The platform cost is fixed; each tenant adds marginal cost only.

---

### Security Comparison: Single-Tenant vs Multi-Tenant

| Security Aspect | Single-Tenant (Ship) | Multi-Tenant SaaS |
|---|---|---|
| Data isolation | Physical (separate infra) | Logical (keyspace) or Physical (account) |
| Encryption keys | Customer manages | Per-tenant CMK in Key Vault |
| Network isolation | Customer's VNet | Shared VNet, tenant-scoped at app layer |
| Compliance scope | Customer's responsibility | Platform responsibility (harder) |
| Audit logs | Customer owns | Platform manages, customer can export |
| Breach blast radius | 1 customer | Potentially all (if isolation fails) |
| Pen testing | Customer runs their own | You must run continuously |
| SOC 2 / HITRUST | Not needed (customer's infra) | Required (you hold the data) |

**Multi-tenant security requirements (non-negotiable):**
1. Annual penetration test by third party
2. SOC 2 Type II certification
3. HITRUST CSF certification (for healthcare)
4. Bug bounty program
5. Tenant isolation tests in CI (verify cross-tenant queries fail)
6. Encryption key rotation every 90 days
7. Immutable audit logs (cannot be deleted by anyone, including platform admins)
8. RBAC: platform team can NEVER read PHI without customer-approved break-glass
9. Data residency: customer chooses Azure region, data never leaves
10. Right to deletion: full tenant data wipe within 30 days of offboarding

---

### Tenant Isolation CI Tests (add to pipeline)

These tests should run on every deploy to verify tenant isolation:

```go
func TestCrossTenantIsolation(t *testing.T) {
    // Create two tenants
    tenantA := provisionTenant("test-tenant-a")
    tenantB := provisionTenant("test-tenant-b")

    // Insert message as tenant A
    tokenA := login(tenantA, "admin", "pass")
    msgID := createMessage(tokenA, "ADT^A01", "PAT001")

    // Try to read tenant A's message as tenant B
    tokenB := login(tenantB, "admin", "pass")
    resp := getMessage(tokenB, msgID)
    assert(resp.StatusCode == 404, "Tenant B should NOT see Tenant A's message")

    // Try to list tenant A's routes as tenant B
    routes := listRoutes(tokenB)
    assert(len(routes) == 0, "Tenant B should NOT see Tenant A's routes")

    // Try to connect Capillary with tenant A's cert to tenant B's broker
    err := connectCapillary(tenantA.Cert, tenantB.BrokerAddr)
    assert(err != nil, "Capillary cert from tenant A should be rejected by tenant B")
}
```

---

### Recommended Go-to-Market Strategy

```
Phase 1: Ship & Deploy (NOW)
├── Sell to 5-10 design partners
├── docker-compose.yml + install.sh
├── Manual onboarding, learn what customers need
├── Revenue: $500-2000/month per customer
└── Total: $5K-20K MRR

Phase 2: Managed Deploy (6 months)
├── We deploy in customer's Azure subscription
├── Terraform/Bicep automation
├── 24/7 monitoring, SLA
├── Revenue: $2000-5000/month per customer
└── Total: $20K-50K MRR

Phase 3: Multi-Tenant SaaS (12 months)
├── arteria.cloud platform
├── Self-service onboarding
├── White-label portal
├── SOC 2 + HITRUST certification
├── Revenue: $500-5000/month per customer, 100+ customers
└── Total: $100K+ MRR

The ship-and-deploy model funds the SaaS development.
Don't build multi-tenant until you have 10+ paying customers
proving the product-market fit.
```
