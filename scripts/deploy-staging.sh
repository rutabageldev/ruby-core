#!/usr/bin/env bash
# deploy-staging.sh VERSION
#
# Deploys the staging stack, runs a smoke test, then tears down.
# Designed to be idempotent: always runs `docker compose down -v` on exit
# so the next run starts from a clean state.
#
# Exit codes:
#   0  smoke test passed
#   1  smoke test failed (stack has been torn down)
#
# Environment:
#   VAULT_TOKEN    (required) — read from deploy/staging/.env if not set
#   VAULT_ADDR     (default: https://127.0.0.1:8200)
#   VAULT_CACERT   (default: /opt/foundation/vault/tls/vault-ca.crt)
#   SMOKE_TIMEOUT  (default: 30) Seconds to wait for audit confirmation
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
STAGING_COMPOSE="$REPO_ROOT/deploy/staging/compose.staging.yaml"
ENV_FILE="$REPO_ROOT/deploy/staging/.env"

# ---------------------------------------------------------------------------
# Load deploy/staging/.env for defaults (caller env vars take precedence).
# ---------------------------------------------------------------------------
if [ -f "$ENV_FILE" ]; then
  while IFS= read -r line || [ -n "$line" ]; do
    [[ "$line" =~ ^[[:space:]]*# ]] && continue
    [[ -z "${line// }" ]] && continue
    key="${line%%=*}"
    if [ -z "${!key+x}" ]; then
      export "${line?}"
    fi
  done < "$ENV_FILE"
fi

VERSION="${1:?Usage: deploy-staging.sh VERSION}"
VAULT_TOKEN="${VAULT_TOKEN:?ERROR: VAULT_TOKEN must be set in deploy/staging/.env or the environment}"
VAULT_ADDR="${VAULT_ADDR:-https://127.0.0.1:8200}"
VAULT_CACERT="${VAULT_CACERT:-/opt/foundation/vault/tls/vault-ca.crt}"

echo "=== Deploying to staging ==="
echo "    Version: ${VERSION}"

# Always tear down on exit (clean state for next run).
cleanup() {
  echo "=== Tearing down staging stack ==="
  docker compose -f "$STAGING_COMPOSE" down -v 2>/dev/null || true
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
# Pull images and start services.
# ---------------------------------------------------------------------------
docker compose -f "$STAGING_COMPOSE" pull
docker compose -f "$STAGING_COMPOSE" up -d

# ---------------------------------------------------------------------------
# Wait for NATS to become healthy (polls /healthz on staging monitor port).
# ---------------------------------------------------------------------------
echo "=== Waiting for staging NATS to become healthy ==="
NATS_HEALTH_URL="http://127.0.0.1:8224/healthz"
MAX_WAIT=30
elapsed=0
until curl -sf "$NATS_HEALTH_URL" > /dev/null 2>&1; do
  if [ "$elapsed" -ge "$MAX_WAIT" ]; then
    echo "ERROR: staging NATS did not become healthy within ${MAX_WAIT}s" >&2
    exit 1
  fi
  sleep 2
  elapsed=$((elapsed + 2))
done
echo "    NATS healthy after ${elapsed}s"

# ---------------------------------------------------------------------------
# Reload NATS auth configuration.
# ---------------------------------------------------------------------------
echo "=== Reloading staging NATS auth configuration ==="
docker wait ruby-core-staging-nats-init 2>/dev/null || true
docker kill --signal=SIGHUP ruby-core-staging-nats

# ---------------------------------------------------------------------------
# Smoke test: confirm NATS → notifier → HA delivery chain.
# ---------------------------------------------------------------------------
echo "=== Running smoke test for ${VERSION} against staging ==="
smoke_exit=0
VAULT_TOKEN="$VAULT_TOKEN" \
VAULT_ADDR="$VAULT_ADDR" \
VAULT_CACERT="$VAULT_CACERT" \
NATS_SERVER="tls://127.0.0.1:4224" \
VAULT_SECRET_PREFIX="secret/ruby-core/staging" \
SMOKE_CONTEXT="staging" \
  "$SCRIPT_DIR/smoke-test.sh" "$VERSION" || smoke_exit=$?

if [ "$smoke_exit" -eq 0 ]; then
  echo "=== Staging smoke test PASSED for ${VERSION} ==="
else
  echo "!!! Staging smoke test FAILED for ${VERSION} — Release will not be created." >&2
  echo "=== Engine logs (last 50 lines) ===" >&2
  docker logs ruby-core-staging-engine --tail 50 2>&1 >&2 || true
fi

# cleanup trap runs on exit
exit "$smoke_exit"
