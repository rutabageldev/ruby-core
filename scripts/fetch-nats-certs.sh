#!/bin/sh
#
# Ruby Core - Fetch NATS Server Certificates from Vault
#
# Used as the entrypoint for the nats-init container. Fetches server TLS
# certificates from Vault and writes them to a shared Docker volume that
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
MAX_RETRIES=5
RETRY_DELAY=2

echo "[nats-init] Fetching NATS server certificates from Vault..."
echo "[nats-init] Vault: ${VAULT_ADDR}"
echo "[nats-init] Path:  ${VAULT_PATH}"

# Wait for Vault to become available
attempt=1
while [ "${attempt}" -le "${MAX_RETRIES}" ]; do
    if vault status >/dev/null 2>&1; then
        echo "[nats-init] Vault is reachable"
        break
    fi
    if [ "${attempt}" -eq "${MAX_RETRIES}" ]; then
        echo "[nats-init] ERROR: Vault not reachable after ${MAX_RETRIES} attempts"
        exit 1
    fi
    echo "[nats-init] Vault not ready, retrying in ${RETRY_DELAY}s (${attempt}/${MAX_RETRIES})..."
    sleep "${RETRY_DELAY}"
    attempt=$((attempt + 1))
done

# Fetch all fields into temp files first to prevent partial writes
TMP_DIR=$(mktemp -d)
trap 'rm -rf "${TMP_DIR}"' EXIT

vault kv get -field=cert "${VAULT_PATH}" > "${TMP_DIR}/server-cert.pem"
vault kv get -field=key  "${VAULT_PATH}" > "${TMP_DIR}/server-key.pem"
vault kv get -field=ca   "${VAULT_PATH}" > "${TMP_DIR}/ca.pem"

# Validate all files were written (non-empty)
for f in server-cert.pem server-key.pem ca.pem; do
    if [ ! -s "${TMP_DIR}/${f}" ]; then
        echo "[nats-init] ERROR: ${f} is empty after fetch"
        exit 1
    fi
done

# Set permissions before moving into place
chmod 644 "${TMP_DIR}/server-cert.pem" "${TMP_DIR}/ca.pem"
chmod 644 "${TMP_DIR}/server-key.pem"

# Move atomically into the target directory
mv "${TMP_DIR}/server-cert.pem" "${CERTS_DIR}/server-cert.pem"
mv "${TMP_DIR}/server-key.pem"  "${CERTS_DIR}/server-key.pem"
mv "${TMP_DIR}/ca.pem"          "${CERTS_DIR}/ca.pem"

echo "[nats-init] Certificates written to ${CERTS_DIR}"
echo "[nats-init] Done."
