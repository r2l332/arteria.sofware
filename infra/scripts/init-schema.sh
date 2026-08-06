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
  if [[ "$(basename "$cql_file")" == 007_* ]] || [[ "$(basename "$cql_file")" == 008_* ]] || [[ "$(basename "$cql_file")" == 009_* ]]; then
    # Migration files may fail on re-run (e.g., column already exists) — tolerate errors
    cqlsh "$SCYLLA_HOST" "$SCYLLA_PORT" -f "$cql_file" 2>/dev/null || true
  else
    cqlsh "$SCYLLA_HOST" "$SCYLLA_PORT" -f "$cql_file"
  fi
done

echo "Schema initialization complete."
