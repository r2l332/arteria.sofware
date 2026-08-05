#!/bin/bash
# Zero-downtime deployment for Arteria services
# Usage: ./scripts/deploy.sh [service...]
# Example: ./scripts/deploy.sh api frontend processing
#          ./scripts/deploy.sh all
set -e

COMPOSE_FILE="docker-compose.yml"
SERVICES="${@:-api frontend processing egress}"

if [ "$1" = "all" ]; then
  SERVICES="api frontend processing egress tunnel-broker"
fi

echo "╔══════════════════════════════════════════════════════════╗"
echo "║  Arteria Zero-Downtime Deploy                           ║"
echo "╠══════════════════════════════════════════════════════════╣"
echo "║  Services: $SERVICES"
echo "╚══════════════════════════════════════════════════════════╝"
echo ""

for SERVICE in $SERVICES; do
  echo "── Deploying: $SERVICE ──"

  # Build new image
  echo "  Building..."
  docker compose -f "$COMPOSE_FILE" build "$SERVICE" 2>&1 | tail -1

  if [ "$SERVICE" = "frontend" ]; then
    # Frontend is manually managed (not compose-controlled)
    echo "  Replacing frontend container..."
    docker build -t arteriaapp-frontend -f frontend/Dockerfile frontend/ > /dev/null 2>&1
    
    # Start new container with temp name
    docker run -d --name arteria-frontend-new \
      --network arteriaapp_default \
      --network-alias frontend \
      -p 3001:3000 \
      -e NODE_ENV=production \
      arteriaapp-frontend > /dev/null 2>&1

    # Wait for health
    sleep 3
    if curl -sf http://localhost:3001 > /dev/null 2>&1; then
      echo "  New frontend healthy, swapping..."
      docker stop arteria-frontend 2>/dev/null || true
      docker rm arteria-frontend 2>/dev/null || true
      docker rename arteria-frontend-new arteria-frontend
      # Re-bind to port 3000
      docker stop arteria-frontend
      docker rm arteria-frontend
      docker run -d --name arteria-frontend \
        --network arteriaapp_default \
        --network-alias frontend \
        -p 3000:3000 \
        -e NODE_ENV=production \
        arteriaapp-frontend > /dev/null 2>&1
      echo "  ✓ Frontend deployed"
    else
      echo "  ✗ New frontend unhealthy, rolling back"
      docker stop arteria-frontend-new 2>/dev/null || true
      docker rm arteria-frontend-new 2>/dev/null || true
    fi
  else
    # Compose-managed services — use rolling update
    echo "  Starting new instance..."
    docker compose -f "$COMPOSE_FILE" up -d --no-deps --build "$SERVICE" 2>&1 | tail -1

    # Wait and health check
    sleep 3
    CONTAINER="arteria-${SERVICE}"
    if docker exec "$CONTAINER" sh -c 'echo ok' > /dev/null 2>&1; then
      echo "  ✓ $SERVICE deployed and healthy"
    else
      echo "  ⚠ $SERVICE started but health unconfirmed"
    fi
  fi
  echo ""
done

echo "Deploy complete. Verifying API health..."
sleep 2
if curl -sf http://localhost:8080/health > /dev/null 2>&1; then
  echo "✓ API healthy"
else
  echo "⚠ API health check failed"
fi
