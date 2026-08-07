#!/bin/bash
# Arteria DB Sync — Export from source, import to target
# Usage:
#   Export:  ./db-sync.sh export              (creates db-export.tar.gz)
#   Import:  ./db-sync.sh import              (loads db-export.tar.gz)
#   Remote:  ./db-sync.sh pull <user@host>    (SSH export from remote, import locally)

set -e

TABLES=(
  "communication_points"
  "routes"
  "filters"
  "filters_by_id"
  "lookup_tables"
  "lookup_entries"
  "tunnel_nodes"
  "users"
  "organisations"
  "org_users"
)

EXPORT_DIR="/tmp/arteria-db-export"
ARCHIVE="db-export.tar.gz"

export_db() {
  echo "Exporting Arteria database..."
  rm -rf "$EXPORT_DIR"
  mkdir -p "$EXPORT_DIR"

  for table in "${TABLES[@]}"; do
    echo "  Exporting arteria.$table..."
    docker exec arteria-scylladb cqlsh -e \
      "COPY arteria.$table TO '/tmp/$table.csv' WITH HEADER=TRUE;" 2>/dev/null || true
    docker cp arteria-scylladb:/tmp/$table.csv "$EXPORT_DIR/$table.csv" 2>/dev/null || true
  done

  tar -czf "$ARCHIVE" -C "$EXPORT_DIR" .
  echo "Exported to $ARCHIVE ($(du -h "$ARCHIVE" | cut -f1))"
}

import_db() {
  if [ ! -f "$ARCHIVE" ]; then
    echo "ERROR: $ARCHIVE not found. Run 'export' first or copy it here."
    exit 1
  fi

  echo "Importing Arteria database from $ARCHIVE..."
  rm -rf "$EXPORT_DIR"
  mkdir -p "$EXPORT_DIR"
  tar -xzf "$ARCHIVE" -C "$EXPORT_DIR"

  # Wait for ScyllaDB
  echo "Waiting for ScyllaDB..."
  until docker exec arteria-scylladb cqlsh -e "USE arteria" 2>/dev/null; do
    sleep 2
  done

  for table in "${TABLES[@]}"; do
    CSV="$EXPORT_DIR/$table.csv"
    [ ! -f "$CSV" ] && continue
    LINES=$(wc -l < "$CSV" | xargs)
    [ "$LINES" -le 1 ] && continue
    echo "  Importing arteria.$table ($((LINES-1)) rows)..."
    docker cp "$CSV" arteria-scylladb:/tmp/$table.csv
    docker exec arteria-scylladb cqlsh -e \
      "COPY arteria.$table FROM '/tmp/$table.csv' WITH HEADER=TRUE;" 2>/dev/null || echo "    (some rows may have failed — duplicates are OK)"
  done

  echo ""
  echo "Import complete. Restart processing to reload routes:"
  echo "  docker compose restart processing egress"
}

pull_remote() {
  REMOTE="$1"
  if [ -z "$REMOTE" ]; then
    echo "Usage: ./db-sync.sh pull user@host"
    exit 1
  fi

  echo "Pulling database from $REMOTE..."
  REMOTE_DIR=$(ssh "$REMOTE" "find ~ -maxdepth 3 -name 'docker-compose.yml' -path '*/arteria*' -exec dirname {} \; 2>/dev/null | head -1")
  if [ -z "$REMOTE_DIR" ]; then
    echo "ERROR: Could not find arteria project on $REMOTE"
    exit 1
  fi

  echo "  Found project at $REMOTE:$REMOTE_DIR"
  ssh "$REMOTE" "cd $REMOTE_DIR && bash scripts/db-sync.sh export"
  scp "$REMOTE:$REMOTE_DIR/$ARCHIVE" "./$ARCHIVE"
  import_db
}

case "${1:-}" in
  export)
    export_db
    ;;
  import)
    import_db
    ;;
  pull)
    pull_remote "$2"
    ;;
  *)
    echo "Arteria DB Sync"
    echo ""
    echo "Usage:"
    echo "  ./scripts/db-sync.sh export             Export local DB to db-export.tar.gz"
    echo "  ./scripts/db-sync.sh import             Import db-export.tar.gz into local DB"
    echo "  ./scripts/db-sync.sh pull user@host     SSH to remote, export, copy here, import"
    echo ""
    echo "Tables exported: ${TABLES[*]}"
    ;;
esac
