#!/usr/bin/env bash
#
# Ruby Core - Credentials Setup
#
# Generates and stores all credentials needed for dev and prod environments
# in compliance with ADR-0015 (Vault for secrets), ADR-0017 (NKEY auth), and
# ADR-0018 (TLS everywhere).
#
# What this script does:
#   1. Verifies the general-purpose Vault is reachable
#   2. Generates NKEY pairs for each service (if missing), stores seeds in Vault
#   3. Generates auth.conf from Vault-stored public NKEYs (always regenerated)
#   4. Generates TLS certificates (server certs to disk, client certs to Vault)
#   5. Validates the setup
#
# Prerequisites:
#   - General-purpose Vault running on the node
#   - nk tool: go install github.com/nats-io/nkeys/nk@latest
#   - mkcert: brew install mkcert (macOS) or see https://github.com/FiloSottile/mkcert
#   - jq: for JSON parsing
#   - vault: HashiCorp Vault CLI
#
# Usage:
#   ./scripts/setup-credentials.sh
#
# Environment variables:
#   VAULT_ADDR    - Vault address (default: http://127.0.0.1:8200)
#   VAULT_TOKEN   - Vault token (default: root)
#   FORCE_REGEN   - Set to "true" to regenerate all credentials
#

set -euo pipefail

# =============================================================================
# Configuration
# =============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
AUTH_CONF="${PROJECT_ROOT}/deploy/base/nats/auth.conf"
CERTS_DIR="${PROJECT_ROOT}/deploy/base/nats/certs"

# Vault configuration — points to the general-purpose Vault on this node
export VAULT_ADDR="${VAULT_ADDR:-http://127.0.0.1:8200}"
export VAULT_TOKEN="${VAULT_TOKEN:-root}"

# Services that need NKEYs
SERVICES=("gateway" "engine" "notifier" "presence" "admin")

# Associative array to hold public keys (populated by generate_nkeys)
declare -A PUBKEYS

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
    log_info "Processing NKEYs for services..."

    for service in "${SERVICES[@]}"; do
        local vault_path="secret/ruby-core/nats/${service}"

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

            PUBKEYS[$service]="${pubkey}"
            log_success "  ${service}: new key stored in Vault at ${vault_path}"
        else
            PUBKEYS[$service]="${existing_pubkey}"
            log_info "  ${service}: using existing key from Vault (use FORCE_REGEN=true to regenerate)"
        fi
    done
}

# =============================================================================
# auth.conf Generation (ADR-0017)
# =============================================================================

generate_auth_conf() {
    log_info "Generating auth.conf..."

    cat > "${AUTH_CONF}" <<EOF
# AUTO-GENERATED by scripts/setup-credentials.sh — do not edit manually.
# Re-run the script to update: make setup-creds
# NKEYs sourced from Vault (ADR-0015, ADR-0017)
#
# Subject naming follows ADR-0027: {source}.{class}.{type}[.{id}][.{action}]
# Classes: events, commands, audit, metrics, logs

authorization {
  # Default permissions - deny all (defense in depth)
  default_permissions: {
    publish: {
      deny: ">"
    }
    subscribe: {
      deny: ">"
    }
  }

  users: [
    # Gateway service
    # Responsibilities: Ingest events from Home Assistant, execute commands, publish audit
    {
      nkey: "${PUBKEYS[gateway]}"
      permissions: {
        publish: {
          allow: [
            "ha.events.>",
            "ruby_gateway.audit.>",
            "ruby_gateway.metrics.>"
          ]
        }
        subscribe: {
          allow: [
            "ruby_engine.commands.>"
          ]
        }
      }
    },

    # Engine service
    # Responsibilities: Process events, evaluate rules, publish commands
    {
      nkey: "${PUBKEYS[engine]}"
      permissions: {
        publish: {
          allow: [
            "ruby_engine.commands.>",
            "ruby_engine.audit.>",
            "ruby_engine.metrics.>"
          ]
        }
        subscribe: {
          allow: [
            "ha.events.>"
          ]
        }
      }
    },

    # Notifier service
    # Responsibilities: Send notifications based on commands
    {
      nkey: "${PUBKEYS[notifier]}"
      permissions: {
        publish: {
          allow: [
            "ruby_notifier.audit.>",
            "ruby_notifier.metrics.>"
          ]
        }
        subscribe: {
          allow: [
            "ruby_engine.commands.notification.>"
          ]
        }
      }
    },

    # Presence service
    # Responsibilities: Track presence based on network events
    {
      nkey: "${PUBKEYS[presence]}"
      permissions: {
        publish: {
          allow: [
            "ruby_presence.events.>",
            "ruby_presence.audit.>",
            "ruby_presence.metrics.>"
          ]
        }
        subscribe: {
          allow: [
            "unifi.events.>"
          ]
        }
      }
    },

    # Admin/operator account (for debugging and maintenance)
    {
      nkey: "${PUBKEYS[admin]}"
      permissions: {
        publish: {
          allow: ">"
        }
        subscribe: {
          allow: ">"
        }
      }
    }
  ]
}
EOF

    log_success "auth.conf generated at ${AUTH_CONF}"
}

# =============================================================================
# TLS Certificate Generation
# =============================================================================

generate_tls_certs() {
    log_info "Generating TLS certificates..."

    # Ensure certs directory exists
    mkdir -p "${CERTS_DIR}"

    # Check if we should skip — server certs on disk AND client certs in Vault
    if [[ "${FORCE_REGEN:-false}" != "true" ]] && \
       [[ -f "${CERTS_DIR}/server-cert.pem" ]] && \
       [[ -f "${CERTS_DIR}/server-key.pem" ]] && \
       [[ -f "${CERTS_DIR}/ca.pem" ]]; then
        # Also verify client certs exist in Vault
        local all_in_vault=true
        for service in "${SERVICES[@]}"; do
            if ! vault kv get "secret/ruby-core/tls/${service}" &> /dev/null; then
                all_in_vault=false
                break
            fi
        done
        if [[ "${all_in_vault}" == "true" ]]; then
            log_info "TLS certificates already exist (use FORCE_REGEN=true to regenerate)"
            return 0
        fi
        log_info "Server certs exist on disk but client certs missing from Vault — regenerating client certs"
    fi

    # Install mkcert CA if not already done
    log_info "  Installing mkcert local CA (may require sudo)..."
    mkcert -install 2>/dev/null || true

    # Get the CA root path
    local ca_root
    ca_root=$(mkcert -CAROOT)

    # Generate NATS server certificate
    # Include both dev and prod container names as SANs
    log_info "  Generating NATS server certificate..."
    (
        cd "${CERTS_DIR}"
        mkcert -cert-file server-cert.pem -key-file server-key.pem \
            localhost 127.0.0.1 ::1 nats ruby-core-dev-nats ruby-core-prod-nats
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
    # shellcheck disable=SC2064  # We want immediate expansion here since tmp_dir is local
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

    # Check auth.conf was generated
    if [[ ! -f "${AUTH_CONF}" ]]; then
        log_error "auth.conf not found at ${AUTH_CONF}"
        errors=$((errors + 1))
    elif grep -q "PLACEHOLDER_" "${AUTH_CONF}"; then
        log_error "auth.conf still contains placeholder NKEYs"
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
    echo "                       Credentials Setup Complete"
    echo "============================================================================="
    echo ""
    echo "Generated files (git-ignored):"
    echo "  ${AUTH_CONF}"
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
    echo "  1. Start dev environment:     make dev-up"
    echo "  2. Start dev services:        make dev-services-up"
    echo "  3. Deploy to production:      make deploy-prod"
    echo ""
    echo "To regenerate all credentials:  make setup-creds-force"
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
    echo "  3. Generate auth.conf with public NKEYs for NATS"
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
    generate_auth_conf
    generate_tls_certs
    validate_setup
    print_summary
}

main "$@"
