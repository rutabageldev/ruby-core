#!/usr/bin/env bash
# deploy-prod.sh
#
# Orchestrates a production deployment:
#   1. Capture the currently-running version (for rollback).
#   2. Pull and start new images.
#   3. Reload NATS auth configuration.
#   4. Run smoke-test.sh to confirm end-to-end delivery.
#   5. On smoke test failure: rollback to the previous version and re-test.
#   6. On rollback failure: send a direct HA notification and exit 2.
#
# Exit codes:
#   0  deploy succeeded (smoke test passed)
#   1  deploy failed but rollback succeeded
#   2  deploy failed and rollback also failed (manual intervention required)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PROD_COMPOSE="$REPO_ROOT/deploy/prod/compose.prod.yaml"
VERSION_FILE="$REPO_ROOT/.last-deployed-version"
ENV_FILE="$REPO_ROOT/deploy/prod/.env"

# ---------------------------------------------------------------------------
# Load .env for default values; caller-set env vars take precedence.
# ---------------------------------------------------------------------------
if [ -f "$ENV_FILE" ]; then
  while IFS= read -r line || [ -n "$line" ]; do
    # Skip blank lines and comments.
    [[ "$line" =~ ^[[:space:]]*# ]] && continue
    [[ -z "${line// }" ]] && continue
    key="${line%%=*}"
    # Only export if not already set in the caller's environment.
    if [ -z "${!key+x}" ]; then
      export "${line?}"
    fi
  done < "$ENV_FILE"
fi

# Derive VERSION from the latest git tag if not already set by caller or .env.
if [ -z "${VERSION:-}" ]; then
  VERSION="$(git -C "$REPO_ROOT" describe --tags --abbrev=0 2>/dev/null || true)"
fi
VERSION="${VERSION:?ERROR: VERSION not set and no git tag found — pass VERSION=vX.Y.Z or tag the commit}"
VAULT_TOKEN="${VAULT_TOKEN:?ERROR: VAULT_TOKEN must be set in deploy/prod/.env or the environment}"
VAULT_ADDR="${VAULT_ADDR:-https://127.0.0.1:8200}"
VAULT_CACERT="${VAULT_CACERT:-/opt/foundation/vault/tls/vault-ca.crt}"

_vault() {
  VAULT_ADDR="$VAULT_ADDR" VAULT_CACERT="$VAULT_CACERT" VAULT_TOKEN="$VAULT_TOKEN" vault "$@"
}

# ---------------------------------------------------------------------------
# Capture the previous version before pulling new images.
# ---------------------------------------------------------------------------
PREV_VERSION="unknown"
if PREV_IMG=$(docker inspect ruby-core-prod-engine --format '{{.Config.Image}}' 2>/dev/null); then
  VER=$(echo "$PREV_IMG" | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' || true)
  [ -n "$VER" ] && PREV_VERSION="$VER"
fi
if [ "$PREV_VERSION" = "unknown" ] && [ -f "$VERSION_FILE" ]; then
  PREV_VERSION="$(cat "$VERSION_FILE")"
fi

echo "=== Deploying to production ==="
echo "    Previous: ${PREV_VERSION}  →  Target: ${VERSION}"

# ---------------------------------------------------------------------------
# Pull images and start services.
# ---------------------------------------------------------------------------
docker compose -f "$PROD_COMPOSE" pull
docker compose -f "$PROD_COMPOSE" up -d

# ---------------------------------------------------------------------------
# Reload NATS auth configuration.
# nats-init may have refreshed auth.conf on the named volume; SIGHUP applies it.
# ---------------------------------------------------------------------------
echo "=== Reloading NATS auth configuration ==="
docker wait ruby-core-prod-nats-init 2>/dev/null || true
docker kill --signal=SIGHUP ruby-core-prod-nats

# ---------------------------------------------------------------------------
# Smoke test: confirm NATS → notifier → HA delivery chain.
# ---------------------------------------------------------------------------
echo "=== Running smoke test for ${VERSION} ==="
if VAULT_TOKEN="$VAULT_TOKEN" VAULT_ADDR="$VAULT_ADDR" VAULT_CACERT="$VAULT_CACERT" \
     "$SCRIPT_DIR/smoke-test.sh" "$VERSION"; then
  echo "$VERSION" > "$VERSION_FILE"
  echo "=== Deploy complete ==="
  docker compose -f "$PROD_COMPOSE" ps
  exit 0
fi

# ---------------------------------------------------------------------------
# Smoke test failed: attempt rollback to the previous known-good version.
# ---------------------------------------------------------------------------
echo ""
echo "!!! Smoke test FAILED for ${VERSION} — rolling back to ${PREV_VERSION} ==="

if [ "$PREV_VERSION" = "unknown" ]; then
  echo "!!! Cannot rollback: no previous version recorded. Manual intervention required." >&2
  docker compose -f "$PROD_COMPOSE" ps
  exit 2
fi

# Roll back by re-deploying with the old version tag (uses cached images; no pull).
VERSION="$PREV_VERSION" docker compose -f "$PROD_COMPOSE" up -d
docker wait ruby-core-prod-nats-init 2>/dev/null || true
docker kill --signal=SIGHUP ruby-core-prod-nats

echo "=== Running smoke test for rollback to ${PREV_VERSION} ==="
FAILED_VERSION="$VERSION"  # capture before VERSION changes scope

if VAULT_TOKEN="$VAULT_TOKEN" VAULT_ADDR="$VAULT_ADDR" VAULT_CACERT="$VAULT_CACERT" \
     ROLLBACK_FROM="$FAILED_VERSION" \
     "$SCRIPT_DIR/smoke-test.sh" "$PREV_VERSION"; then
  echo "=== Rollback to ${PREV_VERSION} successful ==="
  docker compose -f "$PROD_COMPOSE" ps
  exit 1  # non-zero: deploy failed even though rollback succeeded
fi

# ---------------------------------------------------------------------------
# Rollback also failed: send a direct HA notification (bypass broken notifier).
# ---------------------------------------------------------------------------
echo ""
echo "!!! CRITICAL: Rollback to ${PREV_VERSION} ALSO FAILED !!!"
echo "!!! Manual intervention required !!!"

HA_URL=$(_vault kv get -field=url   secret/ruby-core/ha 2>/dev/null || echo "")
HA_TOKEN=$(_vault kv get -field=token secret/ruby-core/ha 2>/dev/null || echo "")

if [ -n "$HA_URL" ] && [ -n "$HA_TOKEN" ]; then
  curl -s -o /dev/null -X POST \
    "${HA_URL}/api/services/notify/mobile_app_phone_michael" \
    -H "Authorization: Bearer ${HA_TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{\"title\":\"Deploy+Rollback FAILED\",\"message\":\"ruby-core ${FAILED_VERSION} failed — rollback to ${PREV_VERSION} also failed. Manual intervention required.\"}" \
  && echo "Direct HA notification sent." \
  || echo "WARNING: direct HA notification also failed." >&2
fi

docker compose -f "$PROD_COMPOSE" ps
exit 2
