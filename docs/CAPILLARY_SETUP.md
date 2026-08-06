# Capillary Setup Guide — End-to-End Loop

Step-by-step instructions for deploying the Arteria Capillary agent at a remote site and creating a working message loop (Input → Route → Transform → Output → back to site).

---

## Prerequisites

- A running Arteria instance with the tunnel broker exposed (e.g. `arteria.software:9443`)
- Admin access to the Arteria dashboard (https://arteria.software)
- A Linux VM/server at the remote site (Ubuntu 22.04+ recommended)
- Git and Go 1.23+ installed (for building from source), OR use a pre-built binary

---

## Step 1: Get the Code

```bash
git clone git@github.com:r2l332/arteria.sofware.git
cd arteria.sofware
git pull origin main
```

---

## Step 2: Build the Capillary Agent

```bash
cd backend
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o capillary ./cmd/tunnel-agent
```

Or build all platforms at once:

```bash
./scripts/build-capillary.sh
# Binaries appear in ./dist/
```

Copy the binary to your remote server:

```bash
scp capillary user@your-server:/opt/arteria/bin/capillary
ssh user@your-server "chmod +x /opt/arteria/bin/capillary"
```

---

## Step 3: Create a Tunnel Node in Arteria

In the Arteria dashboard (https://arteria.software) go to **Aorta Mesh** and click **Add Node**.

- **Name:** e.g. `darren-lab`
- **Site Name:** e.g. `Darren's Lab`

This generates an **enrollment token**. Copy it — you'll need it in the next step.

Or via the API:

```bash
curl -X POST https://arteria.software/api/v1/tunnel/nodes \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "darren-lab", "site_name": "Darren Lab"}'
```

The response includes `node_id` and `enrollment_token`.

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

In the Arteria dashboard go to **Comm Points** and create:

### Input CP (receives from your site)

| Field | Value |
|-------|-------|
| Name | `Darren Lab Input` |
| Direction | `INPUT` |
| Protocol | `MLLP` |
| Port | `2575` |
| Tunnel Enabled | ✓ |
| Tunnel Node | `darren-lab` |
| Tunnel Local Port | `2575` |

### Output CP (sends back to your site)

| Field | Value |
|-------|-------|
| Name | `Darren Lab Output` |
| Direction | `OUTPUT` |
| Protocol | `MLLP` |
| Port | `2576` |
| Tunnel Enabled | ✓ |
| Tunnel Node | `darren-lab` |
| Tunnel Local Port | `2576` |

After saving, push the config to the agent:
- Go to **Aorta Mesh** → click your node → **Push Config**

The agent will start listening on `:2575` and forwarding outbound traffic to `:2576`.

---

## Step 7: Create a Route with Filters

Go to **Routes & Filters** → **New Route**:

| Field | Value |
|-------|-------|
| Name | `Darren Lab Round-Trip` |
| Source CP | `Darren Lab Input` |
| Destination CP | `Darren Lab Output` |
| Source Topic | `ADT^A04` (or `*` for all) |
| Destination Topic | `darren.output` |
| Active | ✓ |

### Add Filters

Click the route, then **Add Filter**:

**Filter 1: HL7 to JSON (Python)**

| Field | Value |
|-------|-------|
| Name | `HL7 to JSON` |
| Type | `python` |
| Order | `1` |

Script:
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

**Filter 2: Extract Patient (Python)**

| Field | Value |
|-------|-------|
| Name | `Extract Patient` |
| Type | `python` |
| Order | `2` |

Script:
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

---

## Step 8: Set Up Applications on Your Site

You need something that:
1. **Sends HL7** to `localhost:2575` (the agent's inbound port)
2. **Listens on** `localhost:2576` for the processed results coming back

### Option A: Use the demo VM app

```bash
cd demo/capillary-e2e/vm-app
go build -o vm-app .
./vm-app run
```

This generates random HL7 messages, sends them through the Capillary, and shows processed results on a dashboard at http://localhost:8090.

### Option B: Use any MLLP sender/receiver

Send:
```bash
# Any HL7 sender pointing at localhost:2575
# e.g., Mirth Connect, HAPI test panel, or a simple script
```

Receive (simple netcat listener):
```bash
nc -lk 2576
```

---

## Step 9: Verify the Loop

Check the flow is working:

1. **Agent logs:** `sudo journalctl -u arteria-capillary -f`
   - Should see: `[AGENT] listening on :2575` and `forwarding outbound stream to 127.0.0.1:2576`

2. **Arteria dashboard:** Go to **Message Flow**, select your route
   - Counters should tick up in real-time

3. **Messages page:** Check that messages are appearing with status `DELIVERED`

4. **VM app dashboard** (if using Option A): http://localhost:8090
   - Shows sent vs received counts and the extracted patient data

---

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| `enrollment failed: invalid token` | Token may have expired or already been used. Create a new node in the dashboard. |
| `load CA cert: no such file` | Enrollment didn't save certs. Check directory permissions on `/opt/arteria/config`. |
| Agent connects but no port listeners | Config hasn't been pushed. Go to Aorta Mesh → Push Config. |
| Messages sent but nothing received back | Check the route's destination topic matches the output CP. Check egress logs. |
| `address already in use :2575` | Something else is using port 2575. Kill it or change the tunnel_local_port. |

---

## Environment Variables Reference

| Variable | Default | Description |
|----------|---------|-------------|
| `BROKER_ADDR` | `localhost:9443` | Broker address to connect to |
| `AGENT_CONFIG_DIR` | `/etc/arteria-agent` | Where certs and config are stored |
| `ACK_MODE` | `false` | Send MLLP ACK after forwarding (set `true` for HL7) |
| `FORWARD_HOST` | `127.0.0.1` | Where to forward outbound traffic |
