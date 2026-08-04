#!/bin/bash
set -e

SCYLLA_HOST="${SCYLLA_HOST:-scylladb}"
SCYLLA_PORT="${SCYLLA_PORT:-9042}"
CQL_DIR="/cql"

echo "Waiting for ScyllaDB to accept CQL connections..."
until cqlsh "$SCYLLA_HOST" "$SCYLLA_PORT" -e "DESCRIBE KEYSPACES" > /dev/null 2>&1; do
  echo "  ScyllaDB not ready yet, retrying in 2s..."
  sleep 2
done

echo "ScyllaDB is ready. Applying schemas..."

for cql_file in "$CQL_DIR"/*.cql; do
  echo "  Applying: $(basename "$cql_file")"
  cqlsh "$SCYLLA_HOST" "$SCYLLA_PORT" -f "$cql_file"
done

echo "Schema initialization complete."
