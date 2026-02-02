#!/bin/bash
# NATS Configuration Validator
# Validates nats.conf before deployment to catch common issues
#
# Usage: ./validate-config.sh [config-file]
# Default: ./nats.conf

set -e

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

# Check for placeholder NKEYs
if grep -q "PLACEHOLDER_" "$CONFIG_FILE"; then
    echo "[FAIL] Placeholder NKEYs detected!"
    echo "       The following placeholders must be replaced with real keys:"
    grep -n "PLACEHOLDER_" "$CONFIG_FILE" | while read -r line; do
        echo "         $line"
    done
    echo ""
    echo "       Generate real keys using: nk -gen user -pubout"
    echo "       Store seed keys in Vault (ADR-0015)"
    ERRORS=$((ERRORS + 1))
else
    echo "[OK] No placeholder NKEYs found"
fi

# Check for TLS certificate files (referenced in config)
CERT_DIR="$(dirname "$CONFIG_FILE")/certs"
if [ -d "$CERT_DIR" ]; then
    for cert in server-cert.pem server-key.pem ca.pem; do
        if [ ! -f "$CERT_DIR/$cert" ]; then
            echo "[FAIL] Missing TLS certificate: $CERT_DIR/$cert"
            ERRORS=$((ERRORS + 1))
        else
            echo "[OK] Found: $CERT_DIR/$cert"
        fi
    done
else
    echo "[WARN] Certificate directory not found: $CERT_DIR"
    echo "       TLS certificates are required for mTLS (ADR-0018)"
    WARNINGS=$((WARNINGS + 1))
fi

# Check for JetStream storage directory reference
if grep -q "store_dir:" "$CONFIG_FILE"; then
    echo "[OK] JetStream storage directory configured"
else
    echo "[WARN] JetStream storage directory not found in config"
    WARNINGS=$((WARNINGS + 1))
fi

# Check for TLS configuration
if grep -q "^tls {" "$CONFIG_FILE" || grep -q "^tls{" "$CONFIG_FILE"; then
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

# Check for default deny permissions
if grep -q 'deny: ">"' "$CONFIG_FILE"; then
    echo "[OK] Default deny permissions configured"
else
    echo "[WARN] Default deny permissions not detected"
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
