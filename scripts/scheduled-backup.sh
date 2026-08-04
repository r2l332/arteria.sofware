#!/bin/sh
# Arteria Scheduled Backup
# Runs as a cron job — exports config and stores as a named backup

API_URL="${API_URL:-http://api:8080}"
ADMIN_USER="${ADMIN_USER:-admin}"
ADMIN_PASS="${ADMIN_PASS:-arteriadeployment}"
BACKUP_DIR="${BACKUP_DIR:-/backups}"
RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-30}"

mkdir -p "$BACKUP_DIR"

# Login
TOKEN=$(curl -sf -X POST "$API_URL/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}" | \
  python3 -c "import sys,json; print(json.load(sys.stdin).get('token',''))" 2>/dev/null)

if [ -z "$TOKEN" ]; then
  echo "$(date -Iseconds) ERROR: Failed to authenticate for backup"
  exit 1
fi

# Export config
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
FILENAME="arteria-config-${TIMESTAMP}.json"

curl -sf -H "Authorization: Bearer $TOKEN" "$API_URL/api/v1/config/export" > "$BACKUP_DIR/$FILENAME"

if [ $? -eq 0 ] && [ -s "$BACKUP_DIR/$FILENAME" ]; then
  SIZE=$(wc -c < "$BACKUP_DIR/$FILENAME" | tr -d ' ')
  echo "$(date -Iseconds) OK: Backup saved $FILENAME ($SIZE bytes)"

  # Also save to the API backup store
  curl -sf -X POST "$API_URL/api/v1/config/backups" \
    -H "Authorization: Bearer $TOKEN" \
    -H 'Content-Type: application/json' \
    -d "{\"name\":\"Scheduled $TIMESTAMP\",\"description\":\"Auto-backup\"}" > /dev/null

else
  echo "$(date -Iseconds) ERROR: Backup failed"
  rm -f "$BACKUP_DIR/$FILENAME"
  exit 1
fi

# Cleanup old file backups
find "$BACKUP_DIR" -name "arteria-config-*.json" -mtime +$RETENTION_DAYS -delete
REMAINING=$(ls "$BACKUP_DIR"/arteria-config-*.json 2>/dev/null | wc -l)
echo "$(date -Iseconds) INFO: $REMAINING backup files retained (${RETENTION_DAYS}d policy)"
