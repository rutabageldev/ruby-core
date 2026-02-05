#!/bin/bash
# setup-host-docker-tls.sh - Configures Docker daemon for TLS on Ubuntu host
#
# Prerequisites:
# - Root/sudo access
# - Docker installed
# - Vault CLI installed and authenticated
# - VAULT_ADDR environment variable set
#
# Usage: sudo -E ./setup-host-docker-tls.sh [PORT]
#        Default PORT is 2376

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_ok() { echo -e "${GREEN}✓${NC} $1"; }
log_fail() { echo -e "${RED}✗${NC} $1"; exit 1; }
log_warn() { echo -e "${YELLOW}!${NC} $1"; }
log_info() { echo -e "  $1"; }

# Configuration
PORT="${1:-2376}"
CERT_DIR="/etc/docker/certs"
DAEMON_JSON="/etc/docker/daemon.json"
SYSTEMD_OVERRIDE="/etc/systemd/system/docker.service.d/override.conf"

echo "Docker TLS Host Setup"
echo "====================="
echo "Port: ${PORT}"
echo

# Check prerequisites
echo "1. Checking prerequisites..."

if [ "$(id -u)" -ne 0 ]; then
    log_fail "This script must be run as root (use sudo -E)"
fi

if ! command -v docker &>/dev/null; then
    log_fail "Docker is not installed"
fi
log_ok "Docker installed"

if ! command -v vault &>/dev/null; then
    log_fail "Vault CLI is not installed"
fi
log_ok "Vault CLI installed"

if [ -z "${VAULT_ADDR:-}" ]; then
    log_fail "VAULT_ADDR environment variable not set"
fi
log_ok "VAULT_ADDR=${VAULT_ADDR}"

if ! vault token lookup &>/dev/null; then
    log_fail "Not authenticated to Vault (run 'vault login' first)"
fi
log_ok "Vault authenticated"

echo

# Check port availability
echo "2. Checking port ${PORT}..."

if ss -tlnp | grep -q ":${PORT} "; then
    PROC=$(ss -tlnp | grep ":${PORT} " | head -1)
    if echo "${PROC}" | grep -q "dockerd"; then
        log_warn "Port ${PORT} already in use by Docker (may already be configured)"
    else
        log_fail "Port ${PORT} is in use by another process: ${PROC}"
    fi
else
    log_ok "Port ${PORT} is available"
fi

echo

# Get host identity
echo "3. Gathering host identity..."

HOST_IP=$(hostname -I | awk '{print $1}')
HOST_FQDN=$(hostname -f 2>/dev/null || hostname)

log_info "IP: ${HOST_IP}"
log_info "FQDN: ${HOST_FQDN}"

echo

# Create cert directory
echo "4. Setting up certificate directory..."

mkdir -p "${CERT_DIR}"
chmod 700 "${CERT_DIR}"
log_ok "Created ${CERT_DIR}"

echo

# Issue server certificate from Vault
echo "5. Issuing server certificate from Vault..."

CERT_JSON=$(mktemp)
trap 'rm -f "${CERT_JSON}"' EXIT

if ! vault write -format=json pki/issue/docker-server \
    common_name="docker-server" \
    ip_sans="${HOST_IP},127.0.0.1" \
    alt_names="${HOST_FQDN},localhost" \
    ttl="720h" > "${CERT_JSON}" 2>/dev/null; then
    log_fail "Failed to issue certificate from Vault. Ensure pki/roles/docker-server exists."
fi

jq -r '.data.certificate' "${CERT_JSON}" > "${CERT_DIR}/server.pem"
jq -r '.data.private_key' "${CERT_JSON}" > "${CERT_DIR}/server-key.pem"
jq -r '.data.issuing_ca' "${CERT_JSON}" > "${CERT_DIR}/ca.pem"

chmod 644 "${CERT_DIR}/ca.pem" "${CERT_DIR}/server.pem"
chmod 600 "${CERT_DIR}/server-key.pem"

log_ok "Server certificate installed"

# Verify cert
EXPIRY=$(openssl x509 -in "${CERT_DIR}/server.pem" -noout -enddate | cut -d= -f2)
log_info "Certificate expires: ${EXPIRY}"

echo

# Configure daemon.json
echo "6. Configuring Docker daemon..."

# Backup existing config if present
if [ -f "${DAEMON_JSON}" ]; then
    cp "${DAEMON_JSON}" "${DAEMON_JSON}.bak.$(date +%Y%m%d%H%M%S)"
    log_warn "Backed up existing ${DAEMON_JSON}"
fi

cat > "${DAEMON_JSON}" << EOF
{
  "hosts": ["unix:///var/run/docker.sock", "tcp://0.0.0.0:${PORT}"],
  "tlsverify": true,
  "tlscacert": "${CERT_DIR}/ca.pem",
  "tlscert": "${CERT_DIR}/server.pem",
  "tlskey": "${CERT_DIR}/server-key.pem"
}
EOF

log_ok "Created ${DAEMON_JSON}"

echo

# Create systemd override
echo "7. Creating systemd override..."

mkdir -p "$(dirname "${SYSTEMD_OVERRIDE}")"

cat > "${SYSTEMD_OVERRIDE}" << 'EOF'
[Service]
ExecStart=
ExecStart=/usr/bin/dockerd
EOF

log_ok "Created ${SYSTEMD_OVERRIDE}"

echo

# Restart Docker
echo "8. Restarting Docker daemon..."

systemctl daemon-reload
log_ok "Reloaded systemd"

if systemctl restart docker; then
    log_ok "Docker restarted"
else
    log_fail "Failed to restart Docker. Check: journalctl -u docker"
fi

# Give Docker a moment to start
sleep 2

echo

# Verify
echo "9. Verifying configuration..."

if ss -tlnp | grep -q ":${PORT}.*dockerd"; then
    log_ok "Docker listening on port ${PORT}"
else
    log_fail "Docker not listening on port ${PORT}"
fi

if docker ps &>/dev/null; then
    log_ok "Local Docker socket still works"
else
    log_warn "Local Docker socket test failed"
fi

echo
echo "====================="
echo -e "${GREEN}Setup complete!${NC}"
echo
echo "Host Docker is now listening on tcp://0.0.0.0:${PORT} with TLS."
echo
echo "Next steps:"
echo "  1. Ensure firewall allows port ${PORT} from devcontainer network"
echo "  2. Issue client certificates using: vault write pki/issue/docker-client ..."
echo "  3. Set DOCKER_TLS_HOST=${HOST_IP} before starting devcontainer"
echo
echo "CA certificate for clients:"
echo "  ${CERT_DIR}/ca.pem"
