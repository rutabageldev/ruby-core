#!/usr/bin/env bash
#
# Ruby Core - Development Credentials Setup
#
# This script generates and stores all credentials needed for local development
# in compliance with ADR-0015 (Vault for secrets), ADR-0017 (NKEY auth), and
# ADR-0018 (TLS everywhere).
#
# What this script does:
#   1. Ensures Vault dev server is running
#   2. Generates NKEY pairs for each service, stores seeds in Vault
#   3. Updates nats.conf with public NKEYs
#   4. Generates TLS certificates (server certs to disk, client certs to Vault)
#   5. Validates the setup
#
# Prerequisites:
#   - Docker (for Vault)
#   - nk tool: go install github.com/nats-io/nkeys/nk@latest
#   - mkcert: brew install mkcert (macOS) or see https://github.com/FiloSottile/mkcert
#   - jq: for JSON parsing
#
# Usage:
#   ./scripts/setup-dev-credentials.sh
#
# Environment variables:
#   VAULT_ADDR    - Vault address (default: http://127.0.0.1:8200)
#   VAULT_TOKEN   - Vault token (default: dev-root-token)
#   FORCE_REGEN   - Set to "true" to regenerate all credentials
#

set -euo pipefail

# =============================================================================
# Configuration
# =============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
NATS_CONF="${PROJECT_ROOT}/deploy/base/nats/nats.conf"
CERTS_DIR="${PROJECT_ROOT}/deploy/base/nats/certs"
COMPOSE_FILE="${PROJECT_ROOT}/deploy/dev/compose.dev.yaml"

# Vault configuration
export VAULT_ADDR="${VAULT_ADDR:-http://127.0.0.1:8200}"
export VAULT_TOKEN="${VAULT_TOKEN:-dev-root-token}"

# Services that need NKEYs
SERVICES=("gateway" "engine" "notifier" "presence" "admin")

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

    check_command "docker" "Install Docker from https://docs.docker.com/get-docker/" || missing=1
    check_command "nk" "go install github.com/nats-io/nkeys/nk@latest" || missing=1
    check_command "mkcert" "brew install mkcert (macOS) or see https://github.com/FiloSottile/mkcert" || missing=1
    check_command "jq" "brew install jq (macOS) or apt-get install jq (Linux)" || missing=1
    check_command "vault" "brew install vault (macOS) or see https://developer.hashicorp.com/vault/downloads" || missing=1

    if [[ $missing -eq 1 ]]; then
        log_error "Missing prerequisites. Please install them and retry."
        exit 1
    fi

    log_success "All prerequisites installed"
}

# =============================================================================
# Vault Management
# =============================================================================

ensure_vault_running() {
    log_info "Checking Vault status..."

    # Check if Vault container is running
    if docker ps --format '{{.Names}}' | grep -q "ruby-core-vault"; then
        log_success "Vault container is running"
    else
        log_info "Starting Vault container..."
        docker compose -f "${COMPOSE_FILE}" up -d vault

        # Wait for Vault to be ready
        log_info "Waiting for Vault to be ready..."
        local retries=30
        while [[ $retries -gt 0 ]]; do
            if vault status &> /dev/null; then
                break
            fi
            sleep 1
            retries=$((retries - 1))
        done

        if [[ $retries -eq 0 ]]; then
            log_error "Vault failed to start. Check: docker logs ruby-core-vault"
            exit 1
        fi

        log_success "Vault is ready"
    fi

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

    # Check if secret/ path exists (dev mode usually has it)
    if vault secrets list | grep -q "^secret/"; then
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
    log_info "Generating NKEYs for services..."

    local nats_conf_updated=false

    for service in "${SERVICES[@]}"; do
        local vault_path="secret/ruby-core/nats/${service}"
        local placeholder="PLACEHOLDER_${service^^}_NKEY"

        # Check if we should skip (already exists and not forcing)
        if [[ "${FORCE_REGEN:-false}" != "true" ]]; then
            if vault kv get "${vault_path}" &> /dev/null; then
                log_info "  ${service}: credentials exist in Vault (use FORCE_REGEN=true to regenerate)"
                continue
            fi
        fi

        log_info "  Generating NKEY for ${service}..."

        # Generate NKEY pair
        # nk -gen user -pubout outputs: line 1 = seed, line 2 = public key
        local nkey_output
        nkey_output=$(nk -gen user -pubout)
        local seed=$(echo "${nkey_output}" | head -1)
        local pubkey=$(echo "${nkey_output}" | tail -1)

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

        log_success "  ${service}: seed stored in Vault at ${vault_path}"

        # Update nats.conf with public key
        if grep -q "${placeholder}" "${NATS_CONF}"; then
            # Use sed to replace the placeholder
            if [[ "$(uname)" == "Darwin" ]]; then
                sed -i '' "s|${placeholder}|${pubkey}|g" "${NATS_CONF}"
            else
                sed -i "s|${placeholder}|${pubkey}|g" "${NATS_CONF}"
            fi
            log_success "  ${service}: public key added to nats.conf"
            nats_conf_updated=true
        else
            # Check if a real key is already there
            if grep -q "nkey: \"U.*\"" "${NATS_CONF}"; then
                log_info "  ${service}: nats.conf already has a real NKEY (not updating)"
            else
                log_warn "  ${service}: placeholder not found in nats.conf"
            fi
        fi
    done

    if [[ "${nats_conf_updated}" == "true" ]]; then
        log_success "nats.conf updated with public NKEYs"
    fi
}

# =============================================================================
# TLS Certificate Generation
# =============================================================================

generate_tls_certs() {
    log_info "Generating TLS certificates..."

    # Ensure certs directory exists
    mkdir -p "${CERTS_DIR}"

    # Check if we should skip
    if [[ "${FORCE_REGEN:-false}" != "true" ]] && \
       [[ -f "${CERTS_DIR}/server-cert.pem" ]] && \
       [[ -f "${CERTS_DIR}/server-key.pem" ]] && \
       [[ -f "${CERTS_DIR}/ca.pem" ]]; then
        log_info "TLS certificates already exist (use FORCE_REGEN=true to regenerate)"
        return 0
    fi

    # Install mkcert CA if not already done
    log_info "  Installing mkcert local CA (may require sudo)..."
    mkcert -install 2>/dev/null || true

    # Get the CA root path
    local ca_root
    ca_root=$(mkcert -CAROOT)

    # Generate NATS server certificate
    log_info "  Generating NATS server certificate..."
    (
        cd "${CERTS_DIR}"
        mkcert -cert-file server-cert.pem -key-file server-key.pem \
            localhost 127.0.0.1 ::1 nats ruby-core-nats
    )
    log_success "  Server certificate generated"

    # Copy CA certificate
    cp "${ca_root}/rootCA.pem" "${CERTS_DIR}/ca.pem"
    log_success "  CA certificate copied"

    # Set restrictive permissions on sensitive files
    chmod 600 "${CERTS_DIR}/server-key.pem"
    chmod 644 "${CERTS_DIR}/server-cert.pem"
    chmod 644 "${CERTS_DIR}/ca.pem"
    log_success "  File permissions set (key: 600, certs: 644)"

    # Generate client certificates and store in Vault
    log_info "  Generating client certificates..."

    local tmp_dir
    tmp_dir=$(mktemp -d)
    trap "rm -rf ${tmp_dir}" EXIT

    for service in "${SERVICES[@]}"; do
        local vault_path="secret/ruby-core/tls/${service}"

        # Generate client cert
        (
            cd "${tmp_dir}"
            mkcert -client -cert-file "${service}-cert.pem" -key-file "${service}-key.pem" \
                "${service}" "${service}.ruby-core.local"
        )

        # Read cert and key
        local cert_content
        local key_content
        cert_content=$(cat "${tmp_dir}/${service}-cert.pem")
        key_content=$(cat "${tmp_dir}/${service}-key.pem")
        local ca_content
        ca_content=$(cat "${CERTS_DIR}/ca.pem")

        # Store in Vault
        vault kv put "${vault_path}" \
            cert="${cert_content}" \
            key="${key_content}" \
            ca="${ca_content}" \
            service="${service}" \
            created_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

        log_success "  ${service}: client certificate stored in Vault at ${vault_path}"
    done

    # Clean up temp files (trap handles this)
    log_success "TLS certificates generated and stored"
}

# =============================================================================
# Validation
# =============================================================================

validate_setup() {
    log_info "Validating setup..."

    local errors=0

    # Check NATS server certs exist on disk
    for cert_file in "server-cert.pem" "server-key.pem" "ca.pem"; do
        if [[ ! -f "${CERTS_DIR}/${cert_file}" ]]; then
            log_error "Missing: ${CERTS_DIR}/${cert_file}"
            errors=$((errors + 1))
        fi
    done

    # Check no placeholders remain in nats.conf
    if grep -q "PLACEHOLDER_" "${NATS_CONF}"; then
        log_error "nats.conf still contains placeholder NKEYs:"
        grep -n "PLACEHOLDER_" "${NATS_CONF}" | while read -r line; do
            echo "    ${line}"
        done
        errors=$((errors + 1))
    fi

    # Check all NKEY seeds are in Vault
    for service in "${SERVICES[@]}"; do
        if ! vault kv get "secret/ruby-core/nats/${service}" &> /dev/null; then
            log_error "Missing NKEY seed in Vault: secret/ruby-core/nats/${service}"
            errors=$((errors + 1))
        fi
    done

    # Check all client certs are in Vault
    for service in "${SERVICES[@]}"; do
        if ! vault kv get "secret/ruby-core/tls/${service}" &> /dev/null; then
            log_error "Missing client cert in Vault: secret/ruby-core/tls/${service}"
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
    echo "                    Development Credentials Setup Complete"
    echo "============================================================================="
    echo ""
    echo "TLS Certificates (on disk - git-ignored):"
    echo "  ${CERTS_DIR}/server-cert.pem"
    echo "  ${CERTS_DIR}/server-key.pem"
    echo "  ${CERTS_DIR}/ca.pem"
    echo ""
    echo "Secrets in Vault (${VAULT_ADDR}):"
    echo ""
    echo "  NKEY Seeds:"
    for service in "${SERVICES[@]}"; do
        echo "    secret/ruby-core/nats/${service}"
    done
    echo ""
    echo "  Client TLS Certificates:"
    for service in "${SERVICES[@]}"; do
        echo "    secret/ruby-core/tls/${service}"
    done
    echo ""
    echo "Next steps:"
    echo "  1. Start the dev environment:  make dev-up"
    echo "  2. Verify NATS is healthy:     make dev-ps"
    echo "  3. Check logs if needed:       make dev-logs SERVICE=nats"
    echo ""
    echo "To regenerate all credentials:   FORCE_REGEN=true ./scripts/setup-dev-credentials.sh"
    echo "============================================================================="
}

# =============================================================================
# Main
# =============================================================================

main() {
    echo ""
    echo "============================================================================="
    echo "             Ruby Core - Development Credentials Setup"
    echo "============================================================================="
    echo ""
    echo "This script will:"
    echo "  1. Start Vault (if not running)"
    echo "  2. Generate NKEY pairs and store seeds in Vault"
    echo "  3. Update nats.conf with public NKEYs"
    echo "  4. Generate TLS certs (server to disk, client to Vault)"
    echo "  5. Validate the setup"
    echo ""
    echo "Vault Address: ${VAULT_ADDR}"
    echo "Force Regen:   ${FORCE_REGEN:-false}"
    echo ""

    check_prerequisites
    ensure_vault_running
    enable_kv_engine
    generate_nkeys
    generate_tls_certs
    validate_setup
    print_summary
}

main "$@"
