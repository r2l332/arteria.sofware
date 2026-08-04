#!/bin/bash
# arteria-agent port management
# Usage:
#   ./agent-ports.sh list                  - Show current port mappings
#   ./agent-ports.sh add 2579              - Add a new port
#   ./agent-ports.sh add 2579 2580 2581    - Add multiple ports
#   ./agent-ports.sh remove 2579           - Remove a port
#   ./agent-ports.sh restart               - Recreate container with current ports

set -e

CONTAINER_NAME="${AGENT_CONTAINER:-arteria-node}"
IMAGE="${AGENT_IMAGE:-arteria-agent:amd64}"
CONFIG_VOL="${AGENT_CONFIG_VOL:-/opt/arteria-agent}"
BROKER="${BROKER_ADDR:-arteria.software:9443}"
PORTS_FILE="${CONFIG_VOL}/ports.conf"

# Ensure ports file exists
sudo mkdir -p "$CONFIG_VOL"
if [ ! -f "$PORTS_FILE" ]; then
  echo "2575" | sudo tee "$PORTS_FILE" > /dev/null
fi

case "${1:-help}" in
  list)
    echo "Active ports:"
    cat "$PORTS_FILE" | while read port; do echo "  :$port"; done
    echo ""
    echo "Container status:"
    docker inspect "$CONTAINER_NAME" --format '{{.State.Status}}' 2>/dev/null || echo "not running"
    ;;

  add)
    shift
    for port in "$@"; do
      if grep -q "^${port}$" "$PORTS_FILE" 2>/dev/null; then
        echo "Port $port already configured"
      else
        echo "$port" | sudo tee -a "$PORTS_FILE" > /dev/null
        echo "Added port $port"
      fi
    done
    echo "Run './agent-ports.sh restart' to apply"
    ;;

  remove)
    shift
    for port in "$@"; do
      sudo sed -i "/^${port}$/d" "$PORTS_FILE"
      echo "Removed port $port"
    done
    echo "Run './agent-ports.sh restart' to apply"
    ;;

  restart)
    echo "Recreating agent container..."
    docker stop "$CONTAINER_NAME" 2>/dev/null || true
    docker rm "$CONTAINER_NAME" 2>/dev/null || true

    # Build port flags
    PORT_FLAGS=""
    while read port; do
      PORT_FLAGS="$PORT_FLAGS -p ${port}:${port}"
    done < "$PORTS_FILE"

    # Start container
    eval docker run -d --name "$CONTAINER_NAME" \
      --restart unless-stopped \
      -v "${CONFIG_VOL}:/etc/arteria-agent" \
      -e "BROKER_ADDR=${BROKER}" \
      $PORT_FLAGS \
      "$IMAGE" connect

    sleep 3
    echo ""
    echo "Agent running. Exposed ports:"
    cat "$PORTS_FILE" | while read port; do echo "  :$port"; done
    echo ""
    docker logs "$CONTAINER_NAME" 2>&1 | tail -5
    ;;

  help|*)
    echo "Arteria Agent Port Manager"
    echo ""
    echo "Usage:"
    echo "  $0 list                    Show current ports"
    echo "  $0 add <port> [port...]    Add port(s)"
    echo "  $0 remove <port>           Remove a port"
    echo "  $0 restart                 Recreate container with current ports"
    echo ""
    echo "Environment:"
    echo "  BROKER_ADDR       Broker address (default: arteria.software:9443)"
    echo "  AGENT_IMAGE       Docker image (default: arteria-agent:amd64)"
    echo "  AGENT_CONTAINER   Container name (default: arteria-node)"
    ;;
esac
