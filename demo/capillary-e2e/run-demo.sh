#!/bin/bash
# Run the Capillary E2E Demo
#
# Supports multiple deployment modes:
#   all-docker    — Everything in containers (default)
#   app-on-host   — VM app on host, Capillary agent + Arteria in Docker
#   agent-on-host — Capillary agent on host, VM app + Arteria in Docker
#   test          — Run E2E test suite
#
# Usage:
#   ./run-demo.sh                    # Start all-docker mode
#   ./run-demo.sh app-on-host        # VM app runs natively
#   ./run-demo.sh agent-on-host      # Agent runs natively
#   ./run-demo.sh test               # Run tests (after starting infra)
#   ./run-demo.sh stop               # Stop everything
#   ./run-demo.sh logs               # Tail logs

set -e
cd "$(dirname "$0")"

ROOT_DIR="../.."
COMPOSE="docker compose -f $ROOT_DIR/docker-compose.yml -f ./docker-compose.yml"

start_core() {
    echo "[1/3] Ensuring Arteria core stack is running..."
    docker compose -f "$ROOT_DIR/docker-compose.yml" up -d nats scylladb scylla-init ingestion processing api tunnel-broker egress frontend caddy

    echo "[2/3] Waiting for schema initialization..."
    docker compose -f "$ROOT_DIR/docker-compose.yml" wait scylla-init 2>/dev/null || sleep 5

    echo "[3/3] Seeding demo route and Python filters..."
    $COMPOSE up demo-seed
}

case "${1:-all-docker}" in
  all-docker|start)
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║   Arteria Capillary E2E Demo — All Docker                   ║"
    echo "╠══════════════════════════════════════════════════════════════╣"
    echo "║  All components run in containers.                          ║"
    echo "║  VM App ←→ Agent ←→ Broker ←→ Engine (all Docker network)  ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    echo ""

    start_core

    echo "Starting VM application and Capillary agent..."
    $COMPOSE up -d demo-capillary-agent demo-vm-app

    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║  Demo Running!                                              ║"
    echo "╠══════════════════════════════════════════════════════════════╣"
    echo "║  Dashboard:  http://localhost:8090                           ║"
    echo "║  Arteria UI: http://localhost:3000                           ║"
    echo "║  API:        http://localhost:8080                           ║"
    echo "║                                                             ║"
    echo "║  Stop: ./run-demo.sh stop                                   ║"
    echo "║  Test: ./run-demo.sh test                                   ║"
    echo "║  Logs: ./run-demo.sh logs                                   ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    ;;

  app-on-host)
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║   Arteria Capillary E2E Demo — App on Host                  ║"
    echo "╠══════════════════════════════════════════════════════════════╣"
    echo "║  VM App runs natively on the host.                          ║"
    echo "║  Capillary agent + Arteria run in Docker.                   ║"
    echo "║  Agent forwards outbound to host.docker.internal:2576       ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    echo ""

    start_core

    # Start agent with FORWARD_HOST pointing to host
    echo "Starting Capillary agent (forwarding outbound to host)..."
    FORWARD_HOST=host.docker.internal $COMPOSE up -d demo-capillary-agent

    # Expose agent's port 2575 to host
    echo ""
    echo "Agent MLLP port mapped. Starting VM app on host..."
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║  Infrastructure running. Start the VM app natively:         ║"
    echo "╠══════════════════════════════════════════════════════════════╣"
    echo "║                                                             ║"
    echo "║  cd vm-app && go run . run                                  ║"
    echo "║                                                             ║"
    echo "║  Or with custom settings:                                   ║"
    echo "║  SEND_ADDR=localhost:12575 RECV_ADDR=:2576 go run . run     ║"
    echo "║                                                             ║"
    echo "║  Dashboard: http://localhost:8090                            ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    ;;

  agent-on-host)
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║   Arteria Capillary E2E Demo — Agent on Host                ║"
    echo "╠══════════════════════════════════════════════════════════════╣"
    echo "║  Capillary agent runs natively on the host.                 ║"
    echo "║  VM App + Arteria run in Docker.                            ║"
    echo "║  Agent connects to broker at localhost:9443                  ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    echo ""

    start_core

    # Start VM app in docker, pointing SEND_ADDR to host agent
    echo "Starting VM app (sending to host agent)..."
    SEND_ADDR="host.docker.internal:2575" $COMPOSE up -d demo-vm-app

    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║  Infrastructure running. Start the agent natively:          ║"
    echo "╠══════════════════════════════════════════════════════════════╣"
    echo "║                                                             ║"
    echo "║  cd ../../backend                                           ║"
    echo "║  go run ./cmd/tunnel-agent enroll demo-token-e2e-2024       ║"
    echo "║  go run ./cmd/tunnel-agent connect                          ║"
    echo "║                                                             ║"
    echo "║  Or use a pre-built binary:                                 ║"
    echo "║  BROKER_ADDR=localhost:9443 capillary enroll <token>         ║"
    echo "║  BROKER_ADDR=localhost:9443 capillary connect                ║"
    echo "║                                                             ║"
    echo "║  Dashboard: http://localhost:8090                            ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    ;;

  test)
    echo "Running E2E test suite..."
    echo ""

    # Determine if vm-app binary exists or needs to be built
    if [ -f ./vm-app/vm-app ]; then
      VM_APP="./vm-app/vm-app"
    else
      echo "Building vm-app..."
      (cd vm-app && go build -o vm-app .)
      VM_APP="./vm-app/vm-app"
    fi

    # Determine connection mode
    SEND="${SEND_ADDR:-localhost:2575}"
    RECV="${RECV_ADDR:-:2576}"
    TEST_COUNT="${TEST_COUNT:-5}"

    echo "Send to: $SEND"
    echo "Receive: $RECV"
    echo ""

    SEND_ADDR="$SEND" RECV_ADDR="$RECV" TEST_COUNT="$TEST_COUNT" \
      CONNECT_TIMEOUT_S="${CONNECT_TIMEOUT_S:-15}" \
      "$VM_APP" test
    ;;

  test-docker)
    echo "Running E2E tests inside Docker..."
    $COMPOSE run --rm \
      -e SEND_ADDR="demo-capillary-agent:2575" \
      -e RECV_ADDR=":2576" \
      -e TEST_COUNT="${TEST_COUNT:-5}" \
      -e CONNECT_TIMEOUT_S="30" \
      demo-vm-app test
    ;;

  stop)
    echo "Stopping demo containers (preserving volumes)..."
    $COMPOSE stop demo-vm-app demo-capillary-agent demo-seed 2>/dev/null
    $COMPOSE rm -f demo-vm-app demo-capillary-agent demo-seed 2>/dev/null
    echo "Done. Core Arteria stack left running."
    echo "To stop everything: docker compose -f $ROOT_DIR/docker-compose.yml down"
    ;;

  logs)
    $COMPOSE logs -f demo-vm-app demo-capillary-agent processing egress tunnel-broker
    ;;

  seed)
    echo "Re-seeding demo data..."
    $COMPOSE up demo-seed
    echo "Done. Restart processing to pick up new config:"
    echo "  $COMPOSE restart processing"
    ;;

  *)
    echo "Usage: $0 {all-docker|app-on-host|agent-on-host|test|test-docker|stop|logs|seed}"
    echo ""
    echo "Modes:"
    echo "  all-docker     Everything in containers (default)"
    echo "  app-on-host    VM app on host, agent+Arteria in Docker"
    echo "  agent-on-host  Agent on host, VM app+Arteria in Docker"
    echo "  test           Run E2E tests (host binary)"
    echo "  test-docker    Run E2E tests inside Docker"
    echo "  stop           Tear down all containers"
    echo "  logs           Tail container logs"
    echo "  seed           Re-seed demo route/filters"
    exit 1
    ;;
esac
