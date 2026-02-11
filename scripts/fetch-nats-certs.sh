#!/bin/sh
#
# Ruby Core - Fetch NATS Server Certificates from Vault
#
# Used as the entrypoint for the nats-init container. Fetches server TLS
# certificates from Vault and writes them to a shared tmpfs volume that
# the NATS container reads from.
#
# Environment variables:
#   VAULT_ADDR   - Vault address (required, set by compose)
#   VAULT_TOKEN  - Vault token (required, set by compose)
#   CERTS_DIR    - Output directory (default: /certs)
#
# Vault path: secret/ruby-core/tls/nats-server
#   Fields: cert, key, ca
#

set -eu

CERTS_DIR="${CERTS_DIR:-/certs}"
VAULT_PATH="secret/ruby-core/tls/nats-server"

echo "[nats-init] Fetching NATS server certificates from Vault..."
echo "[nats-init] Vault: ${VAULT_ADDR}"
echo "[nats-init] Path:  ${VAULT_PATH}"

# Fetch each field from Vault KV v2
vault kv get -field=cert "${VAULT_PATH}" > "${CERTS_DIR}/server-cert.pem"
vault kv get -field=key  "${VAULT_PATH}" > "${CERTS_DIR}/server-key.pem"
vault kv get -field=ca   "${VAULT_PATH}" > "${CERTS_DIR}/ca.pem"

# Set restrictive permissions — key readable only by owner
chmod 600 "${CERTS_DIR}/server-key.pem"
chmod 644 "${CERTS_DIR}/server-cert.pem" "${CERTS_DIR}/ca.pem"

echo "[nats-init] Certificates written to ${CERTS_DIR}"
echo "[nats-init] Done."
