#!/bin/bash
# NATS Configuration Validator
# Validates nats.conf and auth.conf before deployment to catch common issues
#
# Usage: ./validate-config.sh [config-file]
# Default: ./nats.conf

set -e

# Vault configuration — foundation Vault (TLS-only since foundation Phase 5)
export VAULT_ADDR="${VAULT_ADDR:-https://127.0.0.1:8200}"
export VAULT_CACERT="${VAULT_CACERT:-/opt/foundation/vault/tls/vault-ca.crt}"

# Load VAULT_TOKEN from prod .env if not already set in the caller's environment.
# The prod .env holds the scoped ruby-core token; do not default to "root" (stale dev token).
if [ -z "${VAULT_TOKEN:-}" ]; then
    _PROD_ENV="$(dirname "$0")/../../prod/.env"
    if [ -f "${_PROD_ENV}" ]; then
        VAULT_TOKEN=$(grep '^VAULT_TOKEN=' "${_PROD_ENV}" | cut -d= -f2-)
        export VAULT_TOKEN
    fi
fi

CONFIG_FILE="${1:-$(dirname "$0")/nats.conf}"

echo "=== NATS Configuration Validator ==="
echo "Checking: $CONFIG_FILE"
echo ""

ERRORS=0
WARNINGS=0

# Check if config file exists
if [ ! -f "$CONFIG_FILE" ]; then
    echo "[FAIL] Configuration file not found: $CONFIG_FILE"
    exit 1
fi

# auth.conf is generated at container startup by nats-init from Vault.
# Validate that all NKEY public keys are present in Vault (source of truth).
if command -v vault >/dev/null 2>&1; then
    echo "Checking NKEY public keys in Vault..."
    SERVICES="gateway engine notifier presence admin audit-sink"
    for service in $SERVICES; do
        if vault kv get -field=public_key "secret/ruby-core/nats/${service}" >/dev/null 2>&1; then
            echo "[OK] NKEY public key found in Vault for ${service}"
        else
            echo "[FAIL] NKEY public key not found in Vault: secret/ruby-core/nats/${service}"
            echo "       Run: make setup-creds"
            ERRORS=$((ERRORS + 1))
        fi
    done
else
    echo "[WARN] vault CLI not available — skipping NKEY public key check"
    WARNINGS=$((WARNINGS + 1))
fi

# Check NATS server TLS certificate prerequisites (ADR-0018, ADR-0030).
# The server cert is no longer a Vault KV bundle — since the direct-PKI migration
# (PLAN-0008 Stage 4 / ADR-0030) nats-init issues it at container start from
# pki_int/issue/<role> using the nats-server AppRole, and the legacy KV path
# secret/ruby-core/tls/nats-server was decommissioned (see cleanup-mkcert-kv-bundles).
# Validate the AppRole material that issuance depends on, not the removed bundle.
NATS_ROLE_ID="${NATS_PKI_ROLE_ID:-/opt/foundation/vault/role-id-foundation-agent-ruby-core-nats-server}"
NATS_SECRET_ID="${NATS_PKI_SECRET_ID:-/opt/foundation/vault/.secret-id-foundation-agent-ruby-core-nats-server}"
if [ -s "$NATS_ROLE_ID" ] && [ -s "$NATS_SECRET_ID" ]; then
    echo "[OK] NATS server PKI AppRole material present (cert is direct-PKI issued at startup)"
else
    echo "[FAIL] NATS server PKI AppRole material missing — cert issuance will fail at startup"
    echo "       Expected non-empty: $NATS_ROLE_ID"
    echo "                       and: $NATS_SECRET_ID"
    echo "       (NATS server cert is PKI-issued at container start per ADR-0030, not a Vault KV bundle.)"
    ERRORS=$((ERRORS + 1))
fi

# Check for JetStream storage directory reference
if grep -q "store_dir:" "$CONFIG_FILE"; then
    echo "[OK] JetStream storage directory configured"
else
    echo "[WARN] JetStream storage directory not found in config"
    WARNINGS=$((WARNINGS + 1))
fi

# Check for TLS configuration
if grep -qE "^[[:space:]]*tls[[:space:]]*\{" "$CONFIG_FILE"; then
    echo "[OK] TLS configuration block found"

    # Check for verify: true (mTLS)
    if grep -q "verify: true" "$CONFIG_FILE"; then
        echo "[OK] mTLS enabled (verify: true)"
    else
        echo "[WARN] mTLS may not be enabled - verify 'verify: true' is set"
        WARNINGS=$((WARNINGS + 1))
    fi
else
    echo "[FAIL] TLS configuration block not found (required by ADR-0018)"
    ERRORS=$((ERRORS + 1))
fi

# Check that nats.conf includes auth.conf from the certs volume
if grep -q "include.*certs/auth.conf" "$CONFIG_FILE"; then
    echo "[OK] nats.conf includes certs/auth.conf"
else
    echo "[WARN] nats.conf does not include certs/auth.conf"
    WARNINGS=$((WARNINGS + 1))
fi

echo ""
echo "=== Validation Complete ==="
echo "Errors: $ERRORS"
echo "Warnings: $WARNINGS"

if [ $ERRORS -gt 0 ]; then
    echo ""
    echo "FAILED: Fix errors before deployment"
    exit 1
elif [ $WARNINGS -gt 0 ]; then
    echo ""
    echo "PASSED with warnings - review before production deployment"
    exit 0
else
    echo ""
    echo "PASSED: Configuration is valid"
    exit 0
fi
