#!/usr/bin/env bash
# stability-watch.sh VERSION DURATION_SECONDS
#
# Watches the prod stack for DURATION_SECONDS after a deploy. Polls container
# health and error rates every 30s. Sends a direct HA push notification on
# degradation or on clean completion. Does NOT auto-rollback in v1 — thresholds
# need tuning against real signal before automated rollback is safe.
#
# Started via systemd-run --no-block from the release pipeline so it survives
# GHA runner process-group cleanup. Logs go to the systemd journal.
#
# Exit codes:
#   0  stability window passed clean
#   1  degradation detected (HA notified)
set -euo pipefail

VERSION="${1:?Usage: stability-watch.sh VERSION DURATION_SECONDS}"
DURATION="${2:-600}"
START=$(date +%s)
DEADLINE=$((START + DURATION))

ERROR_THRESHOLD=10
SERVICES=(gateway engine notifier presence audit-sink)

VAULT_ADDR="${VAULT_ADDR:-https://127.0.0.1:8200}"
VAULT_CACERT="${VAULT_CACERT:-/opt/foundation/vault/tls/vault-ca.crt}"

# VAULT_TOKEN may not be inherited when launched via systemd-run --user.
# Fall back to the prod .env file so the script works in both contexts.
if [ -z "${VAULT_TOKEN:-}" ]; then
  PROD_ENV="/opt/ruby-core/deploy/prod/.env"
  if [ -f "$PROD_ENV" ]; then
    VAULT_TOKEN="$(grep '^VAULT_TOKEN=' "$PROD_ENV" 2>/dev/null | cut -d= -f2-)"
  fi
fi
VAULT_TOKEN="${VAULT_TOKEN:?VAULT_TOKEN is required — set in environment or deploy/prod/.env}"
export VAULT_ADDR VAULT_CACERT VAULT_TOKEN

HA_URL=$(vault kv get -field=url secret/ruby-core/ha)
HA_TOKEN=$(vault kv get -field=token secret/ruby-core/ha)
NOTIFY_SVC=$(vault kv get -field=notify_service secret/ruby-core/ha)

notify_ha() {
  local title="$1" message="$2"
  curl -s -o /dev/null -X POST \
    "${HA_URL}/api/services/notify/${NOTIFY_SVC}" \
    -H "Authorization: Bearer ${HA_TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{\"title\":\"${title}\",\"message\":\"${message}\"}" || true
}

echo "$(date -Iseconds) stability-watch: ${VERSION} — watching for ${DURATION}s"

while [ "$(date +%s)" -lt "$DEADLINE" ]; do
  for svc in "${SERVICES[@]}"; do
    status=$(docker inspect -f '{{.State.Health.Status}}' \
      "ruby-core-prod-${svc}" 2>/dev/null || echo "missing")
    if [ "$status" != "healthy" ]; then
      msg="${svc} health: ${status}"
      echo "$(date -Iseconds) DEGRADED: ${msg}"
      notify_ha "ruby-core ${VERSION} degraded" "$msg"
      exit 1
    fi
  done

  for svc in "${SERVICES[@]}"; do
    errs=$(docker logs --since 1m "ruby-core-prod-${svc}" 2>&1 \
      | grep -c '"level":"error"' || true)
    if [ "$errs" -gt "$ERROR_THRESHOLD" ]; then
      msg="${svc}: ${errs} errors/min (threshold ${ERROR_THRESHOLD})"
      echo "$(date -Iseconds) ERROR SPIKE: ${msg}"
      notify_ha "ruby-core ${VERSION} error spike" "$msg"
      exit 1
    fi
  done

  sleep 30
done

echo "$(date -Iseconds) stability-watch: ${VERSION} — ${DURATION}s window clean"
echo "$(date -Iseconds) ${VERSION} stable" >> /var/log/ruby-core/stability.log
notify_ha "ruby-core ${VERSION} stable" "${DURATION}s post-deploy window clean"
