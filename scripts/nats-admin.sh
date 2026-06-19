#!/usr/bin/env bash
# nats-admin.sh — run the `nats` CLI against a ruby-core NATS server as the admin
# user, with credentials fetched from Vault (NKEY seed + PKI-issued client cert,
# the same path scripts/smoke-test.sh uses). All arguments are passed through to
# `nats`, so this is a thin authenticated wrapper for ad-hoc admin operations.
#
# Usage:  ENV=<dev|staging|prod> scripts/nats-admin.sh <nats args...>
# Example: ENV=prod scripts/nats-admin.sh stream report
#          ENV=prod scripts/nats-admin.sh stream edit HA_EVENTS --max-age=48h --max-bytes=512MB --force
#
# Requires: the `nats` CLI, `vault`, `jq`, and the admin AppRole material on the
# host (/opt/foundation/vault/...-ruby-core-admin). Read VAULT_TOKEN from
# deploy/prod/.env if not already set.

set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${DIR}/.." && pwd)"

NATS_CLI="${NATS_CLI:-${HOME}/go/bin/nats}"
command -v "${NATS_CLI}" >/dev/null 2>&1 || { echo "error: nats CLI not found at ${NATS_CLI}" >&2; exit 1; }

case "${ENV:-}" in
    prod)    NATS_SERVER="tls://127.0.0.1:4223"; VAULT_SECRET_PREFIX="secret/ruby-core" ;;
    staging) NATS_SERVER="tls://127.0.0.1:4224"; VAULT_SECRET_PREFIX="secret/ruby-core/staging" ;;
    dev)     NATS_SERVER="tls://127.0.0.1:4222"; VAULT_SECRET_PREFIX="secret/ruby-core/dev" ;;
    "")      echo "error: ENV is required (dev | staging | prod)" >&2; exit 2 ;;
    *)       echo "error: unknown ENV='${ENV}'" >&2; exit 2 ;;
esac

export VAULT_ADDR="${VAULT_ADDR:-https://127.0.0.1:8200}"
export VAULT_CACERT="${VAULT_CACERT:-/opt/foundation/vault/tls/vault-ca.crt}"
if [[ -z "${VAULT_TOKEN:-}" ]]; then
    VAULT_TOKEN="$(grep '^VAULT_TOKEN=' "${REPO_ROOT}/deploy/prod/.env" | head -1 | cut -d= -f2-)"
    export VAULT_TOKEN
fi

TMPDIR="$(mktemp -d)"
trap 'rm -rf "${TMPDIR}"' EXIT

# Admin NKEY seed (KV; NKEY auth is orthogonal to TLS).
vault kv get -field=seed "${VAULT_SECRET_PREFIX}/nats/admin" > "${TMPDIR}/seed.nk"
chmod 600 "${TMPDIR}/seed.nk"

# Admin client cert: AppRole-login then issue from pki_int (PLAN-0008 Stage 4.B).
ADMIN_ROLE_ID="$(tr -d '[:space:]' < "${ADMIN_ROLE_ID_PATH:-/opt/foundation/vault/role-id-foundation-agent-ruby-core-admin}")"
ADMIN_SECRET_ID="$(tr -d '[:space:]' < "${ADMIN_SECRET_ID_PATH:-/opt/foundation/vault/.secret-id-foundation-agent-ruby-core-admin}")"
ADMIN_VAULT_TOKEN="$(vault write -field=token auth/approle/login role_id="${ADMIN_ROLE_ID}" secret_id="${ADMIN_SECRET_ID}")"
VAULT_TOKEN="${ADMIN_VAULT_TOKEN}" vault write -format=json pki_int/issue/ruby-core-admin \
    common_name=admin ttl=1h > "${TMPDIR}/issue.json"
jq -r '.data.certificate' "${TMPDIR}/issue.json" > "${TMPDIR}/client.crt"
jq -r '.data.private_key' "${TMPDIR}/issue.json" > "${TMPDIR}/client.key"
jq -r '.data.issuing_ca'  "${TMPDIR}/issue.json" > "${TMPDIR}/ca.crt"
chmod 600 "${TMPDIR}/client.key"

exec "${NATS_CLI}" \
    --server  "${NATS_SERVER}" \
    --tlscert "${TMPDIR}/client.crt" \
    --tlskey  "${TMPDIR}/client.key" \
    --tlsca   "${TMPDIR}/ca.crt" \
    --nkey    "${TMPDIR}/seed.nk" \
    "$@"
