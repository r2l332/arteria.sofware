#!/bin/bash
set -e

# Arteria Test Runner — Runs inside Docker or locally
# Usage: ./run-tests.sh [suite] [args]
#   Suites: all, ingest, api, filters, routing, dlq, load, metrics, server

HARNESS=${HARNESS_BIN:-/usr/local/bin/harness}
SUITE=${1:-all}
shift 2>/dev/null || true

case "$SUITE" in
  all)
    echo "Running all test suites..."
    exec "$HARNESS" test-all
    ;;
  ingest)
    exec "$HARNESS" test-ingest
    ;;
  api)
    exec "$HARNESS" test-api
    ;;
  filters)
    exec "$HARNESS" test-filters
    ;;
  routing)
    exec "$HARNESS" test-routing
    ;;
  dlq)
    exec "$HARNESS" test-dlq
    ;;
  load)
    COUNT=${1:-1000}
    echo "Running load test with $COUNT messages..."
    exec "$HARNESS" test-load "$COUNT"
    ;;
  metrics)
    exec "$HARNESS" test-metrics
    ;;
  server)
    echo "Starting MLLP receiver (for egress testing)..."
    exec "$HARNESS" server
    ;;
  send)
    FILE=${1:?Usage: run-tests.sh send <file>}
    exec "$HARNESS" send "$FILE"
    ;;
  *)
    echo "Unknown suite: $SUITE"
    echo "Available: all, ingest, api, filters, routing, dlq, load, metrics, server, send"
    exit 1
    ;;
esac
