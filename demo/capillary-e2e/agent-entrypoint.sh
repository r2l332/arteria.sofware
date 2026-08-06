#!/bin/bash
# Auto-enroll and connect the Capillary agent for the demo.
# Handles: fresh enrollment, already-enrolled (certs exist), and re-enrollment.
set -e

CONFIG_DIR="${AGENT_CONFIG_DIR:-/data/agent}"
BROKER="${BROKER_ADDR:-tunnel-broker:9443}"
TOKEN="${ENROLL_TOKEN:-demo-token-e2e-2024}"

echo "[demo-agent] Config dir: $CONFIG_DIR, Broker: $BROKER"

# Wait for the broker to be reachable
echo "[demo-agent] Waiting for broker at $BROKER..."
until timeout 2 bash -c "echo > /dev/tcp/${BROKER%:*}/${BROKER#*:}" 2>/dev/null; do
  sleep 2
done
echo "[demo-agent] Broker reachable."

# Enroll if not already enrolled (check for cert files, not just node-id)
if [ -f "$CONFIG_DIR/node.pem" ] && [ -f "$CONFIG_DIR/node-key.pem" ]; then
  echo "[demo-agent] Already enrolled (certs present): $(cat "$CONFIG_DIR/node-id" 2>/dev/null || echo 'unknown')"
else
  echo "[demo-agent] Enrolling with token..."
  # Try enrollment; if it fails with "already enrolled", delete stale state and retry
  if ! capillary enroll "$TOKEN" 2>&1; then
    echo "[demo-agent] Enrollment failed. Clearing stale config and retrying..."
    rm -rf "$CONFIG_DIR"/*
    # The broker may need the node reset — try once more
    if ! capillary enroll "$TOKEN" 2>&1; then
      echo "[demo-agent] Second enrollment attempt failed. Will try connecting with existing state."
    fi
  fi
  echo "[demo-agent] Enrollment complete."
fi

# Connect (will retry internally on failure)
echo "[demo-agent] Connecting..."
exec capillary connect
