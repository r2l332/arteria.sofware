# Capillary Setup Guide — End-to-End Loop

Step-by-step instructions for deploying the Arteria Capillary agent at a remote site and creating a working message loop (Input → Route → Transform → Output → back to site).

---

## Prerequisites

- A running Arteria instance with the tunnel broker exposed (e.g. `arteria.software:9443`)
- Admin access to the Arteria dashboard (https://arteria.software)
- Docker Desktop (Windows/Mac) OR a Linux VM/server
- Git installed

---

## Step 1: Get the Code

```bash
git clone git@github.com:r2l332/arteria.sofware.git
cd arteria.sofware
git pull origin main
```

---

## Step 2: Deploy the Capillary Agent

### Option A: Docker (Windows / Mac — Recommended)

This runs the agent in a container. Your application runs natively on the host or in a VM.

```powershell
# From the repo root
docker build -t capillary -f backend/Dockerfile.tunnel-agent backend/
```

Create a directory for the agent config:
```powershell
mkdir C:\arteria-agent   # Windows
# or: mkdir ~/arteria-agent   # Mac/Linux
```

Enroll:
```powershell
docker run --rm -v C:\arteria-agent:/etc/arteria-agent capillary enroll <YOUR_TOKEN> --broker arteria.software:9443
```

Run the agent (maps ports 2575 inbound, forwards 2576 outbound to host):
```powershell
docker run -d --name capillary --restart always ^
  -v C:\arteria-agent:/etc/arteria-agent ^
  -p 2575:2575 ^
  -e BROKER_ADDR=arteria.software:9443 ^
  -e ACK_MODE=true ^
  -e FORWARD_HOST=host.docker.internal ^
  capillary connect
```

> **Key:** `FORWARD_HOST=host.docker.internal` makes outbound traffic (results coming back) route to your Windows host on port 2576.

### Option B: Native Binary (Linux VM)

Build for Linux:
```bash
cd backend
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o capillary ./cmd/tunnel-agent
```

Or build for Windows:
```bash
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o capillary.exe ./cmd/tunnel-agent
```

Or build all platforms:
```bash
./scripts/build-capillary.sh
# Binaries appear in ./dist/
```

Copy to your server and run:
```bash
scp capillary user@your-server:/opt/arteria/bin/capillary
ssh user@your-server "chmod +x /opt/arteria/bin/capillary"
```

---

## Step 3: Create a Tunnel Node in Arteria

1. Open https://arteria.software and log in
2. Go to **Aorta Mesh** in the left sidebar
3. Click **Add Node**
4. Fill in:
   - **Name:** `darren-lab`
   - **Site Name:** `Darren's Lab`
5. Click **Create**
6. Copy the **enrollment token** shown — you'll need it next

---

## Step 4: Enroll the Agent

On the remote server:

```bash
export BROKER_ADDR=arteria.software:9443
export AGENT_CONFIG_DIR=/opt/arteria/config

mkdir -p /opt/arteria/config
/opt/arteria/bin/capillary enroll <YOUR_ENROLLMENT_TOKEN>
```

You should see:
```
Enrolling with Aorta at arteria.software:9443...
Enrollment successful. Run 'capillary connect' to start the tunnel.
```

---

## Step 5: Start the Agent

```bash
/opt/arteria/bin/capillary connect
```

For production, set it up as a systemd service:

```bash
sudo tee /etc/systemd/system/arteria-capillary.service << 'EOF'
[Unit]
Description=Arteria Capillary Tunnel Agent
After=network-online.target

[Service]
Type=simple
ExecStart=/opt/arteria/bin/capillary connect
Environment=BROKER_ADDR=arteria.software:9443
Environment=AGENT_CONFIG_DIR=/opt/arteria/config
Environment=ACK_MODE=true
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now arteria-capillary
```

Verify it's connected:
```bash
sudo journalctl -u arteria-capillary -f
# Should see: [AGENT] connected to broker as node <node-id>
```

---

## Step 6: Create Communication Points

1. Go to **Comm Points** in the left sidebar
2. Click **Add Communication Point**

### Create the Input CP

Fill in the form:

| Field | Value |
|-------|-------|
| Name | `Darren Lab Input` |
| Direction | `INPUT` |
| Protocol | `MLLP` |
| Port | `2575` |
| Tunnel Enabled | ✓ (toggle on) |
| Tunnel Node | Select `darren-lab` from dropdown |
| Tunnel Local Port | `2575` |

Click **Save**.

### Create the Output CP

Click **Add Communication Point** again:

| Field | Value |
|-------|-------|
| Name | `Darren Lab Output` |
| Direction | `OUTPUT` |
| Protocol | `MLLP` |
| Port | `2576` |
| Tunnel Enabled | ✓ (toggle on) |
| Tunnel Node | Select `darren-lab` from dropdown |
| Tunnel Local Port | `2576` |

Click **Save**.

### Push Config to the Agent

1. Go to **Aorta Mesh** in the sidebar
2. Click on your node (`darren-lab`)
3. Click **Push Config**
4. The agent will now start listening on `:2575` and forwarding outbound traffic to `:2576`

---

## Step 7: Create a Route with Filters

1. Go to **Routes & Filters** in the left sidebar
2. Click **New Route**
3. Fill in:

| Field | Value |
|-------|-------|
| Name | `Darren Lab Round-Trip` |
| Source CP | Select `Darren Lab Input` |
| Destination CP | Select `Darren Lab Output` |
| Source Topic | `ADT^A04` (or `*` for all message types) |
| Destination Topic | `darren.output` |
| Active | ✓ |

4. Click **Save**

### Add Filter 1: HL7 to JSON

1. Click on your new route in the list
2. Click **Add Filter**
3. Fill in:
   - **Name:** `HL7 to JSON`
   - **Type:** Select `python`
   - **Order:** `1`
4. Paste this script into the editor:
```python
import sys, json

def parse_hl7(raw):
    segments = raw.replace("\n", "\r").split("\r")
    result = {"segments": []}
    for s in segments:
        if not s.strip(): continue
        fields = s.split("|")
        seg = {"type": fields[0], "fields": fields[1:]}
        if fields[0] == "PID" and len(fields) > 5:
            name = fields[5].split("^")
            seg["patient_name"] = {"family": name[0], "given": name[1] if len(name) > 1 else ""}
            seg["patient_id"] = fields[3] if len(fields) > 3 else ""
        result["segments"].append(seg)
    return result

envelope = json.loads(sys.stdin.read())
envelope["rawPayload"] = json.dumps(parse_hl7(envelope.get("rawPayload", "")))
envelope["properties"] = envelope.get("properties") or {}
envelope["properties"]["transform"] = "hl7_to_json"
json.dump(envelope, sys.stdout)
```

5. Click **Save Filter**

### Add Filter 2: Extract Patient

1. Click **Add Filter** again
2. Fill in:
   - **Name:** `Extract Patient`
   - **Type:** Select `python`
   - **Order:** `2`
3. Paste this script into the editor:
```python
import sys, json

envelope = json.loads(sys.stdin.read())
try:
    parsed = json.loads(envelope.get("rawPayload", "{}"))
except: parsed = {"segments": []}

patient_id = patient_name = ""
for seg in parsed.get("segments", []):
    if seg.get("type") == "PID":
        patient_id = seg.get("patient_id", "")
        n = seg.get("patient_name", {})
        patient_name = f"{n.get('given','')} {n.get('family','')}".strip()
        break

envelope["patientId"] = patient_id
envelope["rawPayload"] = json.dumps({"patient_id": patient_id, "patient_name": patient_name})
envelope["properties"] = {"patient_id": patient_id, "patient_name": patient_name, "transform": "extract"}
json.dump(envelope, sys.stdout)
```

4. Click **Save Filter**

Your route is now configured: messages matching `ADT^A04` will flow through both Python filters before being delivered back to your site.

---

## Step 8: Connect Your Integration Engine

The Capillary agent exposes two local ports on your machine:
- **Port 2575** — Send HL7 messages INTO this port (your engine's outbound/output CP)
- **Port 2576** — Receive processed results BACK on this port (your engine's inbound/input CP)

### Rhapsody

1. **Sending (Output Communication Point):**
   - Type: MLLP Client
   - Host: `localhost`
   - Port: `2575`
   - Connection mode: Per-message or persistent

2. **Receiving (Input Communication Point):**
   - Type: MLLP Server
   - Port: `2576`
   - This receives the transformed/extracted data back from Arteria

### Mirth Connect

1. **Sending (Destination):**
   - Connector Type: TCP Sender
   - Host: `localhost`
   - Port: `2575`
   - Transmission Mode: MLLP

2. **Receiving (Source):**
   - Connector Type: TCP Listener
   - Port: `2576`
   - Transmission Mode: MLLP

### HAPI Test Panel / Any HL7 Tool

Point any MLLP sender at `localhost:2575`. Set up any MLLP listener on `localhost:2576`.

### Network Diagram (Windows + Docker)

```
┌──────────────────────────────────────────────────────────────┐
│  Windows Host                                                │
│                                                              │
│  ┌──────────────────┐     ┌─────────────────────────────┐   │
│  │ Rhapsody / Mirth │     │ Docker Desktop              │   │
│  │                  │     │                             │   │
│  │ Output CP ───────┼────►│  ┌───────────────────────┐  │   │
│  │ (MLLP :2575)     │     │  │ Capillary Agent       │  │   │
│  │                  │     │  │ listens :2575          │  │   │
│  │ Input CP ◄───────┼─────┼──│ forwards to host:2576 │  │   │
│  │ (MLLP :2576)     │     │  └───────────┬───────────┘  │   │
│  │                  │     │              │ mTLS tunnel   │   │
│  └──────────────────┘     └──────────────┼──────────────┘   │
│                                          │                   │
└──────────────────────────────────────────┼───────────────────┘
                                           │ Internet
                                           ▼
                               ┌───────────────────────┐
                               │ arteria.software:9443 │
                               │ (Tunnel Broker)       │
                               │                       │
                               │ Ingestion → Process   │
                               │ → Python Filters      │
                               │ → Egress → Tunnel     │
                               └───────────────────────┘
```

### Optional: Demo App for Quick Testing

If you want to verify the Capillary is working before connecting your real engine:

```powershell
# Build the demo app (generates random HL7 + shows dashboard)
cd demo\capillary-e2e\vm-app
go build -o vm-app.exe .
.\vm-app.exe run
# Dashboard at http://localhost:8090
```

---

## Step 9: Verify the Loop

Check the flow is working:

1. **Agent logs:**
   - Docker: `docker logs -f capillary`
   - Linux systemd: `sudo journalctl -u arteria-capillary -f`
   - Should see: `[AGENT] listening on :2575` and `forwarding outbound stream to ...`

2. **Arteria dashboard:** Go to **Message Flow** (https://arteria.software/flow), select your route
   - Counters should tick up in real-time every 2 seconds

3. **Messages page:** Check that messages are appearing with status `DELIVERED`

4. **VM app dashboard** (if using the demo app): http://localhost:8090
   - Shows sent vs received counts and the extracted patient data

5. **Run the E2E test suite:**
   ```bash
   # Windows
   set SEND_ADDR=localhost:2575
   set RECV_ADDR=:2576
   .\vm-app.exe test

   # Linux/Mac
   SEND_ADDR=localhost:2575 RECV_ADDR=:2576 ./vm-app test
   ```

---

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| `enrollment failed: invalid token` | Token expired or used. Create a new node in the dashboard. |
| `load CA cert: no such file` | Enrollment didn't save certs. Check directory permissions. On Docker check volume mount. |
| Agent connects but no port listeners | Config hasn't been pushed. Go to Aorta Mesh → Push Config. |
| Messages sent but nothing received back | Check route destination_topic matches output CP. Check egress logs: `docker logs arteria-egress` |
| `address already in use :2575` | Something else on port 2575. Change `tunnel_local_port` on the CP. |
| Docker: can't reach host app on :2576 | Ensure `FORWARD_HOST=host.docker.internal` is set on the agent container. |
| WSL2: can't reach Docker agent | Use the Windows host IP or `host.docker.internal` from WSL. |

---

## Environment Variables Reference

| Variable | Default | Description |
|----------|---------|-------------|
| `BROKER_ADDR` | `localhost:9443` | Broker address to connect to |
| `AGENT_CONFIG_DIR` | `/etc/arteria-agent` | Where certs and config are stored |
| `ACK_MODE` | `false` | Send MLLP ACK after forwarding (set `true` for HL7) |
| `FORWARD_HOST` | `127.0.0.1` | Where to forward outbound traffic |
