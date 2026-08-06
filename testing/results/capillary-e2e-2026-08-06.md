# Capillary E2E Load Test Results

**Date:** 2026-08-06  
**Environment:** Azure VM `arteria-vm` (Standard_D2ds_v7, Ubuntu 24.04, East US)  
**Broker:** arteria.software:9443 (Docker, home network)  
**Agent:** Native binary on VM, systemd service  
**Route:** Demo Capillary Round-Trip (ADT^A04 → Python filters → demo.output)

---

## Test Configuration

| Parameter | Value |
|-----------|-------|
| VM | `arteria-vm` (52.188.66.170) |
| VM OS | Ubuntu 24.04.4 LTS (x86_64) |
| VM Size | Standard_D2ds_v7 |
| Agent Binary | capillary-linux-amd64 (4.3MB) |
| VM App | vm-app-linux-amd64 (5.2MB) |
| Send Interval | 3000ms |
| Burst Size | 1 |
| Message Type | ADT^A04 (random patient names/IDs) |
| Protocol | MLLP over mTLS tunnel |
| Filters | 2x Python (HL7→JSON, Extract Patient) |

---

## Results

| Metric | Value |
|--------|-------|
| Messages Sent | 873+ |
| Messages Received (round-trip) | 1,579+ |
| Errors | 0 |
| Uptime | ~44 minutes |
| Avg Send Latency | 0.05ms |
| Extraction Success | ✓ (patient_name + patient_id populated) |

### Sample Extracted Output

| Patient Name | Patient ID | Facility | Type |
|---|---|---|---|
| William Gonzalez | MRN89214 | COUNTY_CLINIC | ADT^A04 |
| Anthony Martin | MRN32597 | ST_JAMES | ADT^A04 |
| Charles Clark | MRN82330 | COUNTY_CLINIC | ADT^A04 |
| Karen Jackson | MRN15520 | COUNTY_CLINIC | ADT^A04 |
| Jennifer Thomas | MRN85254 | GENERAL_HOSP | ADT^A04 |
| Daniel Martinez | MRN04213 | ST_JAMES | ADT^A04 |
| Elizabeth Williams | MRN85307 | CITY_MED | ADT^A04 |
| Karen Lewis | MRN45532 | COUNTY_CLINIC | ADT^A04 |
| Karen Martin | MRN15869 | COUNTY_CLINIC | ADT^A04 |
| Nancy Jones | MRN04753 | CITY_MED | ADT^A04 |

---

## Pipeline Verified

```
VM (random HL7 ADT^A04)
  → MLLP localhost:2575
  → Capillary Agent (native binary, systemd)
  → mTLS tunnel (yamux over TLS 1.3)
  → Tunnel Broker (Docker, arteria.software:9443)
  → NATS JetStream (arteria.ingest.raw)
  → Processing Engine
    → Route match: ADT^A04 → "Demo Capillary Round-Trip"
    → Filter 1: Python HL7→JSON (parse segments, extract PID fields)
    → Filter 2: Python Extract Patient (patient_name, patient_id)
  → NATS (arteria.route.demo.output)
  → Egress Service
    → Tunnel delivery (MLLP-framed, base64-decoded payload)
  → Tunnel Broker → yamux stream → Agent
  → Forward to localhost:2576
  → VM App MLLP Receiver ✓
```

---

## Issues Found & Fixed During Test

| Issue | Root Cause | Fix |
|-------|-----------|-----|
| HL7 not parsing (message_type empty) | Broker forwarded MLLP framing bytes (0x0B/0x1C) to NATS | Strip MLLP framing in broker's OnInbound handler |
| Processed messages not reaching VM | Broker used `json.RawMessage` for payload (kept base64 string) | Changed to `[]byte` for proper decode |
| VM received no MLLP messages | Egress sent raw bytes through tunnel without framing | Added MLLP wrapping in `deliverViaTunnel` for MLLP protocol |
| Route collision (ADT^A01) | Existing "ADT Admissions Route" matched first | Changed demo to ADT^A04 |
| VM received 400k+ messages | Egress delivered to ALL output CPs (wildcard topic) | Load `dest_topic` from routes table per CP |
| Python filter SyntaxError | CQL stored `\n` as literal text, not newlines | Use CQL `$$` dollar-quoting for multiline scripts |
| Patient extraction empty | Filter splitting on escaped `\r` string not actual CR byte | Use `chr(13)` / `chr(10)` fallback splitting |

---

## VM Dashboard

Accessible at `http://52.188.66.170:8090` (when VM is running)
- Real-time sent/received counters
- Last 100 processed messages with JSON patient extracts
- Health endpoint at `/health`
