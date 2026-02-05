#!/bin/bash
# validate-docker-tls.sh - Validates Docker TLS configuration
#
# Run from devcontainer to verify TLS connection to host Docker daemon.
# Exit codes: 0 = success, 1 = failure

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_ok() { echo -e "${GREEN}✓${NC} $1"; }
log_fail() { echo -e "${RED}✗${NC} $1"; }
log_warn() { echo -e "${YELLOW}!${NC} $1"; }

ERRORS=0

echo "Docker TLS Connection Validation"
echo "================================="
echo

# Check environment variables
echo "1. Checking environment variables..."

if [ -z "${DOCKER_HOST:-}" ]; then
    log_fail "DOCKER_HOST is not set"
    ((ERRORS++))
else
    log_ok "DOCKER_HOST=${DOCKER_HOST}"
fi

if [ "${DOCKER_TLS_VERIFY:-}" != "1" ]; then
    log_fail "DOCKER_TLS_VERIFY is not set to 1"
    ((ERRORS++))
else
    log_ok "DOCKER_TLS_VERIFY=1"
fi

if [ -z "${DOCKER_CERT_PATH:-}" ]; then
    log_fail "DOCKER_CERT_PATH is not set"
    ((ERRORS++))
else
    log_ok "DOCKER_CERT_PATH=${DOCKER_CERT_PATH}"
fi

echo

# Check certificate files
echo "2. Checking certificate files..."

CERT_PATH="${DOCKER_CERT_PATH:-/run/secrets/docker}"

for cert in ca.pem cert.pem key.pem; do
    if [ -f "${CERT_PATH}/${cert}" ]; then
        log_ok "${cert} exists"

        # Check if readable
        if [ -r "${CERT_PATH}/${cert}" ]; then
            log_ok "${cert} is readable"
        else
            log_fail "${cert} is not readable"
            ((ERRORS++))
        fi

        # Check validity for certs (not key)
        if [ "${cert}" != "key.pem" ]; then
            if openssl x509 -in "${CERT_PATH}/${cert}" -noout -checkend 0 2>/dev/null; then
                EXPIRY=$(openssl x509 -in "${CERT_PATH}/${cert}" -noout -enddate 2>/dev/null | cut -d= -f2)
                log_ok "${cert} is valid (expires: ${EXPIRY})"
            else
                log_fail "${cert} is expired or invalid"
                ((ERRORS++))
            fi
        fi
    else
        log_fail "${cert} not found at ${CERT_PATH}/${cert}"
        ((ERRORS++))
    fi
done

echo

# Check network connectivity
echo "3. Checking network connectivity..."

# Extract host and port from DOCKER_HOST
if [ -n "${DOCKER_HOST:-}" ]; then
    HOST_PORT="${DOCKER_HOST#tcp://}"
    HOST="${HOST_PORT%:*}"
    PORT="${HOST_PORT#*:}"

    if command -v nc &>/dev/null; then
        if nc -z -w 5 "${HOST}" "${PORT}" 2>/dev/null; then
            log_ok "Port ${PORT} on ${HOST} is reachable"
        else
            log_fail "Cannot reach ${HOST}:${PORT}"
            ((ERRORS++))
        fi
    else
        log_warn "nc not available, skipping port check"
    fi
fi

echo

# Test Docker connection
echo "4. Testing Docker connection..."

if docker version &>/dev/null; then
    log_ok "docker version succeeded"

    SERVER_VERSION=$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo "unknown")
    log_ok "Docker server version: ${SERVER_VERSION}"

    if docker ps &>/dev/null; then
        log_ok "docker ps succeeded"
    else
        log_fail "docker ps failed"
        ((ERRORS++))
    fi
else
    log_fail "docker version failed"
    ((ERRORS++))

    echo
    echo "Debug info:"
    docker version 2>&1 | head -20 || true
fi

echo
echo "================================="

if [ $ERRORS -eq 0 ]; then
    log_ok "All checks passed!"
    exit 0
else
    log_fail "${ERRORS} check(s) failed"
    exit 1
fi
