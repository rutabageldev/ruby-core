#!/usr/bin/env bash
#
# Ruby Core - Verify Vault-Sourced mTLS Stack
#
# Builds gateway + engine containers and verifies they connect to NATS
# via mTLS using certificates fetched exclusively from Vault (ADR-0015, ADR-0018).
#
# Prerequisites:
#   - Dev infrastructure running (make dev-up)
#   - Vault seeded with credentials (make setup-dev-creds)
#
# Usage:
#   ./scripts/verify-tls-stack.sh
#

set -euo pipefail

# =============================================================================
# Configuration
# =============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
COMPOSE_FILE="${PROJECT_ROOT}/deploy/dev/compose.dev.yaml"
TIMEOUT=60
POLL_INTERVAL=3
SERVICES=("gateway" "engine")

# Expected log messages (from services/gateway/main.go and services/engine/main.go)
EXPECTED_LOGS=(
    "vault: fetched NATS seed from"
    "vault: fetched TLS material from"
    "connected to NATS at"
)

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# =============================================================================
# Helper Functions
# =============================================================================

log_info()    { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[PASS]${NC} $1"; }
log_warn()    { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error()   { echo -e "${RED}[FAIL]${NC} $1"; }

compose() {
    docker compose -f "${COMPOSE_FILE}" "$@"
}

# =============================================================================
# Cleanup
# =============================================================================

cleanup() {
    log_info "Stopping services (leaving infrastructure running)..."
    compose --profile services stop gateway engine 2>/dev/null || true
}

# =============================================================================
# Pre-flight Checks
# =============================================================================

preflight() {
    log_info "Checking dev infrastructure..."

    local missing=0

    for container in ruby-core-vault ruby-core-nats; do
        if docker ps --format '{{.Names}}' | grep -q "^${container}$"; then
            local health
            health=$(docker inspect --format='{{.State.Health.Status}}' "${container}" 2>/dev/null || echo "unknown")
            if [ "${health}" = "healthy" ]; then
                log_success "${container} is running (healthy)"
            else
                log_warn "${container} is running but status: ${health}"
            fi
        else
            log_error "${container} is not running"
            missing=1
        fi
    done

    if [ ${missing} -eq 1 ]; then
        echo ""
        log_error "Dev infrastructure not running. Start it with: make dev-up"
        exit 1
    fi
}

# =============================================================================
# Build and Start Services
# =============================================================================

start_services() {
    log_info "Building and starting services..."
    compose --profile services up -d --build gateway engine
}

# =============================================================================
# Verify Connections
# =============================================================================

verify() {
    log_info "Waiting for services to connect (timeout: ${TIMEOUT}s)..."
    echo ""

    local elapsed=0
    declare -A svc_pass
    for svc in "${SERVICES[@]}"; do
        svc_pass[$svc]=false
    done

    while [ ${elapsed} -lt ${TIMEOUT} ]; do
        local all_pass=true

        for svc in "${SERVICES[@]}"; do
            if [ "${svc_pass[$svc]}" = "true" ]; then
                continue
            fi

            local logs
            logs=$(compose --profile services logs "${svc}" 2>/dev/null || echo "")
            local svc_ok=true

            for expected in "${EXPECTED_LOGS[@]}"; do
                if ! echo "${logs}" | grep -q "${expected}"; then
                    svc_ok=false
                    break
                fi
            done

            if [ "${svc_ok}" = "true" ]; then
                for expected in "${EXPECTED_LOGS[@]}"; do
                    log_success "${svc}: ${expected}"
                done
                svc_pass[$svc]=true
            else
                all_pass=false
            fi
        done

        if [ "${all_pass}" = "true" ]; then
            break
        fi

        sleep ${POLL_INTERVAL}
        elapsed=$((elapsed + POLL_INTERVAL))
    done

    # Report results
    echo ""
    echo "============================================================================="
    echo "                         Verification Results"
    echo "============================================================================="

    local failed=0
    for svc in "${SERVICES[@]}"; do
        if [ "${svc_pass[$svc]}" = "true" ]; then
            log_success "${svc}: connected to NATS via Vault-sourced mTLS"
        else
            log_error "${svc}: did NOT connect within ${TIMEOUT}s"
            failed=$((failed + 1))
        fi
    done

    echo "============================================================================="

    if [ ${failed} -gt 0 ]; then
        echo ""
        log_error "${failed} service(s) failed. Dumping logs for diagnosis:"
        echo ""
        for svc in "${SERVICES[@]}"; do
            if [ "${svc_pass[$svc]}" != "true" ]; then
                echo "--- ${svc} logs ---"
                compose --profile services logs "${svc}" 2>/dev/null || true
                echo ""
            fi
        done
        echo "--- nats logs ---"
        compose logs nats 2>/dev/null || true
        echo ""
        echo "--- vault logs (last 20 lines) ---"
        compose logs --tail=20 vault 2>/dev/null || true
        return 1
    fi

    return 0
}

# =============================================================================
# Main
# =============================================================================

main() {
    echo ""
    echo "============================================================================="
    echo "         Ruby Core - Vault-Sourced mTLS Verification"
    echo "============================================================================="
    echo ""

    preflight
    echo ""

    trap cleanup EXIT

    start_services
    echo ""

    if verify; then
        echo ""
        log_success "All services verified. Vault-sourced mTLS is working."
        echo ""
    else
        echo ""
        log_error "Verification failed."
        exit 1
    fi
}

main "$@"
