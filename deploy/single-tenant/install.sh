#!/bin/bash
# Arteria Single-Tenant Quick Install
# Usage: curl -sSL https://get.arteria.software | bash -s <domain> [email]
#    or: ./install.sh <domain> [email]
set -e

DOMAIN="${1:?Usage: install.sh <domain> [email]}"
EMAIL="${2:-admin@$DOMAIN}"
INSTALL_DIR="${INSTALL_DIR:-/opt/arteria}"

echo "╔══════════════════════════════════════════════════════════╗"
echo "║  Arteria Integration Engine — Single-Tenant Install      ║"
echo "╠══════════════════════════════════════════════════════════╣"
echo "║  Domain:  $DOMAIN"
echo "║  Email:   $EMAIL"
echo "║  Install: $INSTALL_DIR"
echo "╚══════════════════════════════════════════════════════════╝"
echo ""

# Check prerequisites
command -v docker >/dev/null 2>&1 || { echo "Error: docker is required. Install from https://docs.docker.com/engine/install/"; exit 1; }
command -v docker compose version >/dev/null 2>&1 || { echo "Error: docker compose v2 is required."; exit 1; }

# Clone or update
if [ -d "$INSTALL_DIR/.git" ]; then
  echo "Existing installation found. Updating..."
  cd "$INSTALL_DIR"
  git pull origin main
else
  echo "Cloning Arteria..."
  git clone --depth 1 https://github.com/r2l332/arteria.sofware.git "$INSTALL_DIR"
  cd "$INSTALL_DIR"
fi

# Generate .env if it doesn't exist
if [ ! -f deploy/single-tenant/.env ]; then
  ADMIN_PASS="arteria123"
  JWT_SECRET=$(openssl rand -hex 32)

  cat > deploy/single-tenant/.env <<EOF
DOMAIN=$DOMAIN
TLS_EMAIL=$EMAIL
ADMIN_PASS=$ADMIN_PASS
JWT_SECRET=$JWT_SECRET
LOG_LEVEL=INFO
EOF

  echo "Generated deploy/single-tenant/.env"
  echo "Default credentials: admin / $ADMIN_PASS"
  echo "You will be prompted to change your password on first login."
else
  echo "Existing .env found, keeping current config."
fi

# Start the stack
echo ""
echo "Starting Arteria..."
cd deploy/single-tenant
docker compose up -d --build

echo ""
echo "╔══════════════════════════════════════════════════════════╗"
echo "║  Arteria is running!                                     ║"
echo "╠══════════════════════════════════════════════════════════╣"
echo "║  Dashboard:  https://$DOMAIN"
echo "║  API:        https://$DOMAIN/api/v1"
echo "║  Login:      admin / arteria123"
echo "║              (change on first login)"
echo "║                                                          ║"
echo "║  Manage:     cd $INSTALL_DIR/deploy/single-tenant"
echo "║              docker compose logs -f"
echo "║              docker compose ps"
echo "╚══════════════════════════════════════════════════════════╝"
