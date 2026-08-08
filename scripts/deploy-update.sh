#!/bin/bash
# Deploy latest changes to the running Arteria stack
# Run this on the Mac hosting arteria.software
# Usage: ./scripts/deploy-update.sh [--with-broker]
#
# By default: rebuilds api, processing, frontend, egress, ingestion
# --with-broker: also rebuilds tunnel-broker (disconnects Capillary agents briefly)

set -e
cd ~/arteria.sofware

SERVICES="api processing frontend egress ingestion"

if [[ "$1" == "--with-broker" ]]; then
  SERVICES="api processing frontend egress ingestion tunnel-broker"
  echo "⚠  Including tunnel-broker — Capillary agents will reconnect automatically"
fi

echo "╔═══════════════════════════════════════════════╗"
echo "║  Arteria Deploy (arteria.software)            ║"
echo "╠═══════════════════════════════════════════════╣"
echo "║  Services: $SERVICES"
echo "╚═══════════════════════════════════════════════╝"

echo ""
echo "Pulling latest code..."
git pull origin main

echo "Rebuilding services..."
docker compose build $SERVICES

echo "Restarting services..."
docker compose up -d --no-deps $SERVICES

echo "Waiting for startup..."
sleep 5

echo ""
echo "── Health Check ──"
API_OK=$(curl -sk https://localhost/health 2>/dev/null | grep -c '"ok"' || true)
if [ "$API_OK" -gt 0 ]; then
  echo "  API:        ✓"
else
  echo "  API:        ✗ (check: docker logs arteria-api --tail 20)"
fi

# Check each container is running
for SVC in $SERVICES; do
  CONTAINER="arteria-${SVC}"
  STATE=$(docker inspect -f '{{.State.Status}}' "$CONTAINER" 2>/dev/null || echo "missing")
  if [ "$STATE" = "running" ]; then
    echo "  $SVC:$(printf '%*s' $((14 - ${#SVC})) '')✓"
  else
    echo "  $SVC:$(printf '%*s' $((14 - ${#SVC})) '')✗ ($STATE)"
  fi
done

echo ""
echo "Deploy complete. Caddy, NATS, ScyllaDB were NOT touched."
