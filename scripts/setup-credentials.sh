#!/usr/bin/env bash
#
# Ruby Core - Credentials Setup (NKEY-only as of PLAN-0008 Stage 4.B)
#
# Generates and stores NKEY seeds for each ruby-core service in compliance
# with ADR-0015 (Vault for secrets) and ADR-0017 (NKEY auth).
#
# What this script does:
#   1. Verifies the general-purpose Vault is reachable
#   2. Generates NKEY pairs for each service (if missing), stores seeds in Vault
#   3. Validates the setup
#
# Notes:
#   - TLS certificates are NO LONGER generated here. As of PLAN-0008
#     Stage 4.B, all ruby-core TLS material (5 services + nats-server +
#     admin) is issued directly from Vault PKI (pki_int) at runtime via
#     AppRole. See deploy/{dev,staging,prod}/compose.*.yaml for the
#     VAULT_PKI_ROLE wiring and ADR-0030 for the architectural decision.
#   - auth.conf is also not generated here. It is rendered at container
#     startup by nats-init (scripts/fetch-nats-certs.sh) which reads NKEY
#     public keys directly from Vault.
#
# Prerequisites:
#   - General-purpose Vault running on the node
#   - nk tool: go install github.com/nats-io/nkeys/nk@latest
#   - jq: for JSON parsing
#   - vault: HashiCorp Vault CLI
#
# Usage:
#   ./scripts/setup-credentials.sh
#
# Environment variables:
#   VAULT_ADDR    - Vault address (default: https://127.0.0.1:8200)
#   VAULT_CACERT  - Path to Vault CA cert (default: /opt/foundation/vault/tls/vault-ca.crt)
#   VAULT_TOKEN   - Vault token (default: root)
#   FORCE_REGEN   - Set to "true" to regenerate all NKEY seeds
#

set -euo pipefail

# =============================================================================
# Configuration
# =============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Vault configuration — points to the general-purpose Vault on this node
export VAULT_ADDR="${VAULT_ADDR:-https://127.0.0.1:8200}"
export VAULT_CACERT="${VAULT_CACERT:-/opt/foundation/vault/tls/vault-ca.crt}"
export VAULT_TOKEN="${VAULT_TOKEN:-root}"

# Vault secret path prefix — override for staging:
#   VAULT_SECRET_PREFIX=secret/ruby-core/staging ./scripts/setup-credentials.sh
VAULT_SECRET_PREFIX="${VAULT_SECRET_PREFIX:-secret/ruby-core}"

# Services that need NKEYs.
# TLS material is issued from Vault PKI directly at runtime (PLAN-0008
# Stage 4.B) — no TLS generation in this script anymore.
SERVICES=("gateway" "engine" "notifier" "presence" "admin" "audit-sink")

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# =============================================================================
# Helper Functions
# =============================================================================

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[OK]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

check_command() {
    if ! command -v "$1" &> /dev/null; then
        log_error "$1 is not installed."
        echo "  Install with: $2"
        return 1
    fi
    return 0
}

# =============================================================================
# Prerequisite Checks
# =============================================================================

check_prerequisites() {
    log_info "Checking prerequisites..."

    local missing=0

    check_command "nk" "go install github.com/nats-io/nkeys/nk@latest" || missing=1
    check_command "jq" "brew install jq (macOS) or apt-get install jq (Linux)" || missing=1
    check_command "vault" "brew install vault (macOS) or see https://developer.hashicorp.com/vault/downloads" || missing=1

    if [[ $missing -eq 1 ]]; then
        log_error "Missing prerequisites. Please install them and retry."
        exit 1
    fi

    log_success "All prerequisites installed"
}

# =============================================================================
# Vault Connectivity
# =============================================================================

ensure_vault_running() {
    log_info "Checking Vault connectivity at ${VAULT_ADDR}..."

    if ! vault status &> /dev/null; then
        log_error "Cannot reach Vault at ${VAULT_ADDR}"
        log_error "Ensure the general-purpose Vault container is running."
        echo "  Check with: docker ps --filter name=vault"
        exit 1
    fi

    log_success "Vault is reachable"

    # Verify we can authenticate
    if ! vault token lookup &> /dev/null; then
        log_error "Cannot authenticate with Vault. Check VAULT_ADDR and VAULT_TOKEN."
        echo "  VAULT_ADDR=${VAULT_ADDR}"
        echo "  VAULT_TOKEN is set: $([ -n "${VAULT_TOKEN}" ] && echo 'yes' || echo 'no')"
        exit 1
    fi

    log_success "Vault authentication successful"
}

enable_kv_engine() {
    log_info "Ensuring KV secrets engine is enabled..."

    # vault secrets list requires sys/mounts read access (root-level permission).
    # The ruby-core-writer token only has secret/data/ruby-core/* access, so this
    # check will fail with 403 when called with a scoped token.
    # Gracefully skip: if we can't list mounts, assume secret/ is already enabled
    # (any subsequent kv put will fail with a clear error if it isn't).
    local mounts_output
    if ! mounts_output=$(vault secrets list 2>&1); then
        if echo "$mounts_output" | grep -q "permission denied"; then
            log_info "Cannot list secrets engines (scoped token — no sys/mounts access). Assuming secret/ already enabled."
            return 0
        fi
        log_error "Unexpected error listing secrets engines: ${mounts_output}"
        exit 1
    fi

    if echo "$mounts_output" | grep -q "^secret/"; then
        log_success "KV secrets engine already enabled at secret/"
    else
        log_info "Enabling KV secrets engine..."
        vault secrets enable -path=secret kv-v2
        log_success "KV secrets engine enabled"
    fi
}

# =============================================================================
# NKEY Generation
# =============================================================================

generate_nkeys() {
    log_info "Processing NKEYs for services..."

    for service in "${SERVICES[@]}"; do
        local vault_path="${VAULT_SECRET_PREFIX}/nats/${service}"

        # Check if key exists in Vault
        local existing_pubkey=""
        if vault kv get -field=public_key "${vault_path}" &> /dev/null; then
            existing_pubkey=$(vault kv get -field=public_key "${vault_path}")
        fi

        if [[ "${FORCE_REGEN:-false}" == "true" ]] || [[ -z "${existing_pubkey}" ]]; then
            log_info "  Generating NKEY for ${service}..."

            # Generate NKEY pair
            # nk -gen user -pubout outputs: line 1 = seed, line 2 = public key
            local nkey_output
            nkey_output=$(nk -gen user -pubout)
            local seed
            local pubkey
            seed=$(echo "${nkey_output}" | head -1)
            pubkey=$(echo "${nkey_output}" | tail -1)

            # Validate the keys look correct (don't log secrets on error)
            if [[ ! "${seed}" =~ ^SU ]]; then
                log_error "Generated seed has invalid format (expected prefix 'SU')"
                exit 1
            fi
            if [[ ! "${pubkey}" =~ ^U ]]; then
                log_error "Generated public key has invalid format (expected prefix 'U')"
                exit 1
            fi

            # Store seed in Vault
            vault kv put "${vault_path}" \
                seed="${seed}" \
                public_key="${pubkey}" \
                service="${service}" \
                created_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

            log_success "  ${service}: new key stored in Vault at ${vault_path} (prefix: ${VAULT_SECRET_PREFIX})"
        else
            log_info "  ${service}: using existing key from Vault (use FORCE_REGEN=true to regenerate)"
        fi
    done
}

# =============================================================================
# Validation
# =============================================================================

validate_setup() {
    log_info "Validating setup..."

    local errors=0

    # Check all NKEY seeds are in Vault
    for service in "${SERVICES[@]}"; do
        if ! vault kv get "${VAULT_SECRET_PREFIX}/nats/${service}" &> /dev/null; then
            log_error "Missing NKEY seed in Vault: ${VAULT_SECRET_PREFIX}/nats/${service}"
            errors=$((errors + 1))
        fi
    done

    # Run the NATS config validator
    log_info "Running NATS config validator..."
    if "${PROJECT_ROOT}/deploy/base/nats/validate-config.sh"; then
        log_success "NATS configuration is valid"
    else
        log_warn "NATS config validation reported issues (may be expected)"
    fi

    if [[ $errors -gt 0 ]]; then
        log_error "Validation failed with ${errors} error(s)"
        return 1
    fi

    log_success "All validations passed"
    return 0
}

# =============================================================================
# Summary
# =============================================================================

print_summary() {
    echo ""
    echo "============================================================================="
    echo "                       Credentials Setup Complete"
    echo "============================================================================="
    echo ""
    echo "Secrets in Vault (${VAULT_ADDR}) under prefix: ${VAULT_SECRET_PREFIX}"
    echo ""
    echo "  NKEY Seeds:"
    for service in "${SERVICES[@]}"; do
        echo "    ${VAULT_SECRET_PREFIX}/nats/${service}"
    done
    echo ""
    echo "  TLS Certificates:  issued from Vault PKI at runtime (pki_int/issue/ruby-core-<svc>)."
    echo "                     See deploy/{dev,staging,prod}/compose.*.yaml for the AppRole wiring."
    echo ""
    echo "Next steps:"
    echo "  1. Start dev environment:     make dev-up"
    echo "  2. Start dev services:        make dev-services-up"
    echo "  3. Deploy to production:      make deploy-prod"
    echo ""
    echo "To regenerate NKEY seeds:       make setup-creds-force"
    echo "============================================================================="
}

# =============================================================================
# Main
# =============================================================================

main() {
    echo ""
    echo "============================================================================="
    echo "                  Ruby Core - Credentials Setup"
    echo "============================================================================="
    echo ""
    echo "This script will:"
    echo "  1. Verify Vault connectivity"
    echo "  2. Generate NKEY pairs (if missing) and store seeds in Vault"
    echo "  3. Validate the setup"
    echo ""
    echo "Note: TLS certificates are issued from Vault PKI at runtime (PLAN-0008"
    echo "Stage 4.B). This script no longer generates or stores TLS material."
    echo "auth.conf is generated at container startup by nats-init from Vault."
    echo ""
    echo "Vault Address:  ${VAULT_ADDR}"
    echo "Secret Prefix:  ${VAULT_SECRET_PREFIX}"
    echo "Force Regen:    ${FORCE_REGEN:-false}"
    echo ""

    check_prerequisites
    ensure_vault_running
    enable_kv_engine
    generate_nkeys
    validate_setup
    print_summary
}

main "$@"
