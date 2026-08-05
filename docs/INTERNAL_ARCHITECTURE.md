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

### Hybrid Architecture (Best of Both Worlds)

Keep NATS + custom Go services (they're already fast), but offload stateful storage to managed services:

```
┌─────────────────────────────────────────────────────────────┐
│  AKS / EKS Cluster (3 nodes, spot instances for processing) │
│                                                              │
│  Self-managed (in containers):          Managed services:    │
│  ┌────────────────────────┐   ┌────────────────────────────┐│
│  │ NATS JetStream (3-node)│   │ Cosmos DB / Keyspaces      ││
│  │ Ingestion              │   │ (serverless Cassandra)      ││
│  │ Processing (V8)        │   │                             ││
│  │ API                    │   │ Azure Blob / S3             ││
│  │ Aorta broker           │   │ (backup + archive)          ││
│  │ Frontend               │   │                             ││
│  └────────────────────────┘   │ Key Vault / Secrets Mgr    ││
│                                │ (JWT keys, TLS certs)       ││
│                                │                             ││
│                                │ Monitor / CloudWatch        ││
│                                │ (observability)             ││
│                                └────────────────────────────┘│
└─────────────────────────────────────────────────────────────┘
```

**Why hybrid is the sweet spot:**
- NATS is already incredibly fast (10M msgs/sec single node) — no point replacing with Event Hubs/MSK
- V8 processing is CPU-bound — managed services can't help here
- ScyllaDB → Cosmos/Keyspaces: **huge ops win** (no compaction tuning, no disk management, no node recovery)
- Secrets → Key Vault: security compliance checkbox
- Monitoring → managed: better dashboards, alerting, no Prometheus to maintain

**Cost: ~$300-500/month** depending on traffic, with the operational simplicity of managed storage.

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
| P99 Latency | 15ms | 10ms | 8ms |
| Scale-up time | Manual (minutes) | 30 seconds (HPA) | 5 seconds (KEDA) |
| Failover time | Manual / minutes | 30 seconds | Automatic / seconds |
| Ops burden | High (you manage everything) | Medium (manage NATS + apps) | Low (manage code only) |
| Monthly cost | $150-400 | $300-500 | $400-600 |
| Compliance | Self-attested | Inherited (AKS) | Inherited (PaaS) |

---

### Quick Migration Path

**Phase 1** (Current): Docker Compose on VM ✓
**Phase 2** (Next): Move ScyllaDB → Cosmos DB (Cassandra API)
- Change connection string in `scyllautil`
- Same CQL schema works
- Delete ScyllaDB container
- Gain: auto-scaling, backup, multi-region, no disk management

**Phase 3**: Move to Container Apps / Fargate
- Convert docker-compose.yml to Container Apps YAML or ECS task definitions
- Add KEDA scaling rules (scale processing on NATS queue depth)
- Gain: auto-scale to zero overnight, burst handling, no VM patches

**Phase 4** (Optional): Replace NATS with Event Hubs
- Only if you need >10M msgs/sec or multi-region event streaming
- NATS is already excellent — this is optional unless hitting limits
