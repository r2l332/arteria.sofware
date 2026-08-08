#!/bin/bash
# Deploy latest changes to the running Arteria stack
# Run this on the Mac hosting arteria.software
# SAFE: does NOT restart caddy, tunnel-broker, nats, or scylladb

set -e
cd ~/arteria.sofware

echo "Pulling latest code..."
git pull origin main

echo "Rebuilding app services (caddy/broker/nats/scylla untouched)..."
docker compose build api processing frontend egress ingestion

echo "Restarting app services only..."
docker compose up -d --no-deps api processing frontend egress ingestion

echo "Waiting for services..."
sleep 5

echo "Verifying..."
curl -sk https://localhost/health && echo " OK"

echo ""
echo "Deploy complete. Caddy, tunnel-broker, NATS, ScyllaDB were NOT touched."
echo "Darren's Capillary connection is preserved."
