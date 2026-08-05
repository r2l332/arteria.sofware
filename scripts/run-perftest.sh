#!/bin/bash
# Run production performance test
# Usage: ./run-perftest.sh [duration] [concurrency]
#
# Examples:
#   ./run-perftest.sh              # 60s, 20 workers/port, 4 ports
#   ./run-perftest.sh 5m 50        # 5 minutes, 50 workers/port
#   ./run-perftest.sh 30m 100      # 30-minute soak test

set -e

DURATION="${1:-60s}"
CONCURRENCY="${2:-20}"
TARGET="${TARGET_HOST:-localhost}"
PORTS="${PORTS:-2575,2576,2577,2578}"

cd "$(dirname "$0")/.."

echo "Building performance test binary..."
cd testing
go build -o ../dist/perftest ./cmd/perftest
cd ..

echo ""
echo "Running performance test..."
echo "  Target:      $TARGET"
echo "  Ports:       $PORTS"
echo "  Duration:    $DURATION"
echo "  Concurrency: $CONCURRENCY workers/port"
echo ""

TARGET_HOST="$TARGET" \
  PORTS="$PORTS" \
  DURATION="$DURATION" \
  CONCURRENCY="$CONCURRENCY" \
  ADMIN_PASS="${ADMIN_PASS:-arteria123}" \
  ./dist/perftest
