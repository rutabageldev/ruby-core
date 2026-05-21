#!/bin/sh
#
# Ruby Core - Fetch NATS Server Certificates and Generate auth.conf from Vault
#
# Used as the entrypoint for the nats-init container. Fetches the NATS server
# TLS material and the service NKEY public keys from Vault, then writes them
# to a shared Docker volume that the NATS container reads from.
#
# Two cert paths supported (PLAN-0008, Phase 17.6):
#
#   1. Direct-PKI (preferred). When VAULT_PKI_ROLE is set, the container
#      AppRole-logs in via the mounted role-id + secret-id files and issues
#      a fresh server cert from pki_int/issue/<role>. Output mirrors path 2's
#      file layout (server-cert.pem / server-key.pem / ca.pem). Requires jq
#      (installed inline below — vault:1.15 Alpine image doesn't ship it).
#
#   2. Legacy KV bundle. When VAULT_PKI_ROLE is unset, the script reads the
#      pre-PKI mkcert bundle from secret/ruby-core/tls/nats-server. Retained
#      as the rollback target until Phase 17.7's decommission.
#
# Environment variables:
#   VAULT_ADDR             - Vault address (required, set by compose)
#   VAULT_TOKEN            - Vault token (required for path 2; ignored on path 1)
#   VAULT_PKI_ROLE         - When set, switches to direct-PKI path
#   VAULT_ROLE_ID_PATH     - Path to AppRole role-id file (default /vault/role-id)
#   VAULT_SECRET_ID_PATH   - Path to AppRole secret-id file (default /vault/secret-id)
#   VAULT_PKI_TTL          - Cert TTL (default 720h — matches pki_int role default)
#   VAULT_PKI_IP_SANS      - IP SANs (default 127.0.0.1)
#   CERTS_DIR              - Output directory (default /certs)
#
# Vault paths:
#   pki_int/issue/<VAULT_PKI_ROLE>    — direct-PKI issuance (path 1)
#   secret/ruby-core/tls/nats-server  — legacy KV bundle (path 2)
#   secret/ruby-core/nats/<service>   — NKEY public keys (both paths; KV-only)
#

set -eu

CERTS_DIR="${CERTS_DIR:-/certs}"
TLS_PATH="${TLS_PATH:-secret/ruby-core/tls/nats-server}"
NKEY_BASE="${NKEY_BASE:-secret/ruby-core/nats}"
PKI_ROLE="${VAULT_PKI_ROLE:-}"
ROLE_ID_PATH="${VAULT_ROLE_ID_PATH:-/vault/role-id}"
SECRET_ID_PATH="${VAULT_SECRET_ID_PATH:-/vault/secret-id}"
PKI_TTL="${VAULT_PKI_TTL:-720h}"
PKI_IP_SANS="${VAULT_PKI_IP_SANS:-127.0.0.1}"
MAX_RETRIES=5
RETRY_DELAY=2

echo "[nats-init] Vault: ${VAULT_ADDR}"
if [ -n "${PKI_ROLE}" ]; then
    echo "[nats-init] mode: direct-PKI (role=${PKI_ROLE})"
else
    echo "[nats-init] mode: legacy KV (PLAN-0008 rollback path)"
fi

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

# Fetch all files into a temp directory first to prevent partial writes
TMP_DIR=$(mktemp -d)
trap 'rm -rf "${TMP_DIR}"' EXIT

# =============================================================================
# TLS Certificates
# =============================================================================

if [ -n "${PKI_ROLE}" ]; then
    # ── Path 1: direct-PKI via AppRole ────────────────────────────────────
    echo "[nats-init] Issuing NATS server cert from pki_int/issue/${PKI_ROLE}..."

    # jq is needed to extract three fields from one issuance response without
    # re-issuing. vault:1.15 Alpine doesn't ship jq; apk-add is idempotent and
    # cheap (~1s on first run, no-op afterward).
    if ! command -v jq >/dev/null 2>&1; then
        apk add --no-cache jq >/dev/null
    fi

    if [ ! -s "${ROLE_ID_PATH}" ]; then
        echo "[nats-init] ERROR: role-id file missing or empty at ${ROLE_ID_PATH}"
        exit 1
    fi
    if [ ! -s "${SECRET_ID_PATH}" ]; then
        echo "[nats-init] ERROR: secret-id file missing or empty at ${SECRET_ID_PATH}"
        exit 1
    fi

    ROLE_ID=$(tr -d '[:space:]' < "${ROLE_ID_PATH}")
    SECRET_ID=$(tr -d '[:space:]' < "${SECRET_ID_PATH}")

    # AppRole login — returns a short-lived token scoped to the
    # foundation-agent-ruby-core-nats-server policy.
    APPROLE_TOKEN=$(vault write -field=token auth/approle/login \
        role_id="${ROLE_ID}" secret_id="${SECRET_ID}")
    if [ -z "${APPROLE_TOKEN}" ]; then
        echo "[nats-init] ERROR: AppRole login returned empty token"
        exit 1
    fi

    # Issue once; capture full JSON so we can extract all three fields from
    # the same cert/key pair without triggering additional issuances.
    VAULT_TOKEN="${APPROLE_TOKEN}" vault write -format=json \
        "pki_int/issue/${PKI_ROLE}" \
        common_name=nats \
        ip_sans="${PKI_IP_SANS}" \
        ttl="${PKI_TTL}" \
        > "${TMP_DIR}/issue-resp.json"

    jq -r '.data.certificate' "${TMP_DIR}/issue-resp.json" > "${TMP_DIR}/server-cert.pem"
    jq -r '.data.private_key' "${TMP_DIR}/issue-resp.json" > "${TMP_DIR}/server-key.pem"
    jq -r '.data.issuing_ca'  "${TMP_DIR}/issue-resp.json" > "${TMP_DIR}/ca.pem"
    rm -f "${TMP_DIR}/issue-resp.json"
else
    # ── Path 2: legacy KV bundle (rollback target) ────────────────────────
    echo "[nats-init] Fetching NATS server certificates from Vault (${TLS_PATH})..."

    if ! vault kv get "${TLS_PATH}" >/dev/null 2>&1; then
        echo "[nats-init] ERROR: Secret not found at ${TLS_PATH}"
        echo "[nats-init] Run 'make setup-creds' first (with Vault running and VAULT_TOKEN set)."
        exit 2
    fi

    vault kv get -field=cert "${TLS_PATH}" > "${TMP_DIR}/server-cert.pem"
    vault kv get -field=key  "${TLS_PATH}" > "${TMP_DIR}/server-key.pem"
    vault kv get -field=ca   "${TLS_PATH}" > "${TMP_DIR}/ca.pem"
fi

for f in server-cert.pem server-key.pem ca.pem; do
    if [ ! -s "${TMP_DIR}/${f}" ]; then
        echo "[nats-init] ERROR: ${f} is empty after fetch"
        exit 1
    fi
done

chmod 644 "${TMP_DIR}/server-cert.pem" "${TMP_DIR}/ca.pem"
chmod 644 "${TMP_DIR}/server-key.pem"

# =============================================================================
# NKEY Public Keys → auth.conf
# =============================================================================

echo "[nats-init] Fetching service NKEY public keys from Vault..."

fetch_pubkey() {
    service="$1"
    path="${NKEY_BASE}/${service}"
    if ! vault kv get "${path}" >/dev/null 2>&1; then
        echo "[nats-init] ERROR: NKEY secret not found at ${path}"
        echo "[nats-init] Run 'make setup-creds' first."
        exit 2
    fi
    key=$(vault kv get -field=public_key "${path}")
    if [ -z "${key}" ]; then
        echo "[nats-init] ERROR: public_key field empty at ${path}"
        exit 1
    fi
    echo "${key}"
}

PUBKEY_GATEWAY=$(fetch_pubkey gateway)
PUBKEY_ENGINE=$(fetch_pubkey engine)
PUBKEY_NOTIFIER=$(fetch_pubkey notifier)
PUBKEY_PRESENCE=$(fetch_pubkey presence)
PUBKEY_ADMIN=$(fetch_pubkey admin)
PUBKEY_AUDIT_SINK=$(fetch_pubkey audit-sink)
PUBKEY_NAVI_DEV=$(fetch_pubkey navi-dev)
PUBKEY_NAVI_STAGING=$(fetch_pubkey navi-staging)
PUBKEY_NAVI_PROD=$(fetch_pubkey navi-prod)

echo "[nats-init] Generating auth.conf..."

cat > "${TMP_DIR}/auth.conf" <<EOF
# AUTO-GENERATED by nats-init container at startup — do not edit manually.
# Source: Vault NKEY public keys (ADR-0015, ADR-0017)
#
# Subject naming follows ADR-0027: {source}.{class}.{type}[.{id}][.{action}]
# Exception: audit subjects use audit.{source}.{type} to avoid overlap with dlq.>
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
    # Responsibilities: Ingest HA events, publish to NATS, reconcile state on reconnect
    # Phase 5 additions:
    #   publish  \$JS.API.>         — JetStream API (KV create/bind, stream queries)
    #   publish  \$JS.ACK.>         — Message acknowledgements
    #   publish  \$KV.gateway_state.> — Gateway reconciler state (single-writer, ADR-0002)
    #   subscribe _INBOX.>          — Reply-to subjects for JetStream API responses
    #   subscribe \$KV.config.>     — Read compiled rule config (passlist + critical entities)
    {
      nkey: "${PUBKEY_GATEWAY}"
      permissions: {
        publish: {
          allow: [
            "ha.events.>",
            "audit.ruby_gateway.>",
            "ruby_gateway.metrics.>",
            "gateway.health",
            "\$JS.API.>",
            "\$JS.ACK.>",
            "\$KV.gateway_state.>"
          ]
        }
        subscribe: {
          allow: [
            "ruby_engine.commands.>",
            "_INBOX.>",
            "\$KV.config.>"
          ]
        }
      }
    },

    # Engine service
    # Responsibilities: Process events, evaluate rules, publish commands
    # Phase 3 additions:
    #   publish  \$JS.API.>     — JetStream API (stream/consumer setup, pull requests, KV)
    #   publish  \$JS.ACK.>     — Message acknowledgements to JetStream
    #   publish  \$KV.idempotency.> — Idempotency KV bucket (single-writer, ADR-0002)
    #   publish  dlq.>          — DLQ forwarder routes dead-lettered messages (ADR-0022)
    #   subscribe _INBOX.>      — Reply-to subjects for JetStream API responses
    #   subscribe \$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.HA_EVENTS.engine_processor
    #                           — max-delivery advisory triggers DLQ routing (ADR-0022)
    # Phase 5 additions:
    #   publish  \$KV.config.>    — Compiled rule config for gateway (single-writer, ADR-0002)
    #   publish  \$KV.presence.>  — Presence processor state (single-writer, ADR-0002)
    {
      nkey: "${PUBKEY_ENGINE}"
      permissions: {
        publish: {
          allow: [
            "ruby_engine.commands.>",
            "audit.ruby_engine.>",
            "ruby_engine.metrics.>",
            "\$JS.API.>",
            "\$JS.ACK.>",
            "\$KV.idempotency.>",
            "\$KV.config.>",
            "\$KV.presence.>",
            "dlq.>"
          ]
        }
        subscribe: {
          allow: [
            "ha.events.>",
            "gateway.health",
            "_INBOX.>",
            "\$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.HA_EVENTS.engine_processor"
          ]
        }
      }
    },

    # Notifier service
    # Responsibilities: Receive notify commands from COMMANDS stream, dispatch push notifications via HA REST API
    # Phase 5 additions:
    #   publish  \$JS.API.>         — JetStream API (consumer create/fetch/bind)
    #   publish  \$JS.ACK.>         — Message acknowledgements
    {
      nkey: "${PUBKEY_NOTIFIER}"
      permissions: {
        publish: {
          allow: [
            "audit.ruby_notifier.>",
            "ruby_notifier.metrics.>",
            "\$JS.API.>",
            "\$JS.ACK.>"
          ]
        }
        subscribe: {
          allow: [
            "ruby_engine.commands.notify.>",
            "_INBOX.>"
          ]
        }
      }
    },

    # Presence service
    # Responsibilities: Multi-source presence fusion; subscribes to HA phone events,
    #   publishes clean presence state events and audit trail.
    {
      nkey: "${PUBKEY_PRESENCE}"
      permissions: {
        publish: {
          allow: [
            "ruby_presence.events.>",
            "audit.ruby_presence.>",
            "ruby_presence.metrics.>",
            "\$JS.API.>",
            "\$JS.ACK.>",
            "\$KV.presence.>",
            "_INBOX.>"
          ]
        }
        subscribe: {
          allow: [
            "ha.events.phone.>",
            "_INBOX.>",
            "\$JS.API.>",
            "\$JS.ACK.>"
          ]
        }
      }
    },

    # Audit-Sink service
    # Responsibilities: Consume all *.audit.> events from AUDIT_EVENTS stream, archive to NDJSON file
    # Principle of least privilege: no publish to business subjects; only JetStream API/ACK
    {
      nkey: "${PUBKEY_AUDIT_SINK}"
      permissions: {
        publish: {
          allow: [
            "\$JS.API.>",
            "\$JS.ACK.>"
          ]
        }
        subscribe: {
          allow: [
            "_INBOX.>"
          ]
        }
      }
    },

    # Navi digest service (dev)
    {
      nkey: "${PUBKEY_NAVI_DEV}"
      permissions: {
        publish: {
          allow: ["navi.dev.>", "audit.navi.>", "\$JS.API.>", "\$JS.ACK.>"]
        }
        subscribe: {
          allow: ["navi.dev.>", "_INBOX.>"]
        }
      }
    },

    # Navi digest service (staging)
    {
      nkey: "${PUBKEY_NAVI_STAGING}"
      permissions: {
        publish: {
          allow: ["navi.staging.>", "audit.navi.>", "\$JS.API.>", "\$JS.ACK.>"]
        }
        subscribe: {
          allow: ["navi.staging.>", "_INBOX.>"]
        }
      }
    },

    # Navi digest service (prod)
    {
      nkey: "${PUBKEY_NAVI_PROD}"
      permissions: {
        publish: {
          allow: ["navi.prod.>", "audit.navi.>", "\$JS.API.>", "\$JS.ACK.>"]
        }
        subscribe: {
          allow: ["navi.prod.>", "_INBOX.>"]
        }
      }
    },

    # Admin/operator account (for debugging and maintenance)
    {
      nkey: "${PUBKEY_ADMIN}"
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

if [ ! -s "${TMP_DIR}/auth.conf" ]; then
    echo "[nats-init] ERROR: auth.conf is empty after generation"
    exit 1
fi

# =============================================================================
# Atomic write to shared volume
# =============================================================================

mv "${TMP_DIR}/server-cert.pem" "${CERTS_DIR}/server-cert.pem"
mv "${TMP_DIR}/server-key.pem"  "${CERTS_DIR}/server-key.pem"
mv "${TMP_DIR}/ca.pem"          "${CERTS_DIR}/ca.pem"
mv "${TMP_DIR}/auth.conf"       "${CERTS_DIR}/auth.conf"

echo "[nats-init] Certificates and auth.conf written to ${CERTS_DIR}"
echo "[nats-init] Done."
