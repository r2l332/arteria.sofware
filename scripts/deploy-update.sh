#!/bin/bash
# Deploy latest changes to the running Arteria stack
# Run this on the Mac hosting arteria.software

set -e
cd ~/arteria.sofware

echo "Pulling latest code..."
git pull origin main

echo "Rebuilding services..."
docker compose build api processing frontend

echo "Restarting updated services..."
docker compose up -d api processing frontend

echo "Waiting for services..."
sleep 5

echo "Verifying..."
curl -sk https://localhost/health && echo " OK"

echo ""
echo "Deploy complete. Refresh https://arteria.software"
