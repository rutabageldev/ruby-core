#!/usr/bin/env bash
# smoke-test.sh VERSION
#
# Verifies two delivery chains:
#   1. NATS → notifier → HA: publishes a synthetic COMMANDS message and waits
#      for the resulting audit event on audit.ruby_notifier.notification_sent.
#   2. Ada Postgres round-trip: publishes a synthetic diaper event to
#      ha.events.ada.diaper_logged and polls Postgres for a persisted row.
#
# Environment:
#   VAULT_TOKEN    (required) Vault token with read access to secret/ruby-core/*
#   VAULT_ADDR     (default: https://127.0.0.1:8200)
#   VAULT_CACERT   (default: /opt/foundation/vault/tls/vault-ca.crt)
#   ROLLBACK_FROM  (optional) If set, message reads "vX.X.X failed — rollback to VERSION successful"
#   SMOKE_CONTEXT  (optional) Set to "staging" for staging validation notification
#   SMOKE_TIMEOUT  (default: 30) Seconds to wait for notifier audit confirmation
#   ADA_TIMEOUT    (default: 10) Seconds to poll Postgres for the ada round-trip row
#
# Exit codes:
#   0  all smoke checks passed
#   1  one or more smoke checks failed
set -euo pipefail

VERSION="${1:?Usage: smoke-test.sh VERSION}"
ROLLBACK_FROM="${ROLLBACK_FROM:-}"
SMOKE_CONTEXT="${SMOKE_CONTEXT:-}"
SMOKE_TIMEOUT="${SMOKE_TIMEOUT:-30}"
ADA_TIMEOUT="${ADA_TIMEOUT:-30}"
VAULT_ADDR="${VAULT_ADDR:-https://127.0.0.1:8200}"
VAULT_CACERT="${VAULT_CACERT:-/opt/foundation/vault/tls/vault-ca.crt}"
NATS_SERVER="${NATS_SERVER:-tls://127.0.0.1:4223}"
VAULT_SECRET_PREFIX="${VAULT_SECRET_PREFIX:-secret/ruby-core}"

if [ -z "${VAULT_TOKEN:-}" ]; then
  echo "ERROR: VAULT_TOKEN is required" >&2
  exit 1
fi

# Temp dir for credentials — always cleaned up on exit.
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

_vault() {
  VAULT_ADDR="$VAULT_ADDR" VAULT_CACERT="$VAULT_CACERT" VAULT_TOKEN="$VAULT_TOKEN" vault "$@"
}

echo "  [smoke] fetching admin credentials from Vault"

if ! _vault kv get -field=seed "${VAULT_SECRET_PREFIX}/nats/admin" > "$TMPDIR/seed.nk" 2>&1; then
  echo "ERROR: cannot fetch admin NKEY seed from Vault (check VAULT_TOKEN and Vault availability)" >&2
  exit 1
fi
chmod 600 "$TMPDIR/seed.nk"

_vault kv get -field=cert "${VAULT_SECRET_PREFIX}/tls/admin" > "$TMPDIR/client.crt"
_vault kv get -field=key  "${VAULT_SECRET_PREFIX}/tls/admin" > "$TMPDIR/client.key"
_vault kv get -field=ca   "${VAULT_SECRET_PREFIX}/tls/admin" > "$TMPDIR/ca.crt"
chmod 600 "$TMPDIR/client.key"

# Millisecond-unique smoke ID so grep matches only this run's audit event.
SMOKE_ID="smoke-$(date +%s%3N)"

# Build notification message based on context.
if [ -n "$ROLLBACK_FROM" ]; then
  TITLE="Deployment failed"
  MSG="ruby-core ${ROLLBACK_FROM} failed — rollback to ${VERSION} successful at $(date +%H:%M)"
elif [ "$SMOKE_CONTEXT" = "staging" ]; then
  TITLE="Staging validated"
  MSG="ruby-core ${VERSION} validated in staging at $(date +%H:%M)"
else
  TITLE="Deployment successful"
  MSG="ruby-core ${VERSION} deployed at $(date +%H:%M)"
fi

NATS_OPTS=(
  --server  "$NATS_SERVER"
  --tlscert "$TMPDIR/client.crt"
  --tlskey  "$TMPDIR/client.key"
  --tlsca   "$TMPDIR/ca.crt"
  --nkey    "$TMPDIR/seed.nk"
)

# Build CloudEvent payload.
# device is "phone_michael" — notifier prepends "mobile_app_" internally (handler.go).
PAYLOAD=$(printf '{"specversion":"1.0","type":"command.notify","source":"smoke","id":"%s","time":"%s","correlationid":"","causationid":"","data":{"title":"%s","message":"%s","device":"phone_michael"}}' \
  "$SMOKE_ID" \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  "$TITLE" \
  "$MSG")

echo "  [smoke] publishing COMMANDS event (id=${SMOKE_ID})"
nats pub "${NATS_OPTS[@]}" "ruby_engine.commands.notify.${SMOKE_ID}" "$PAYLOAD"

echo "  [smoke] waiting up to ${SMOKE_TIMEOUT}s for audit.ruby_notifier.notification_sent"

# Subscribe and wait for the audit event containing our smoke ID.
# grep -m1 exits 0 on first match, causing SIGPIPE to kill nats sub.
# timeout fires if no match within SMOKE_TIMEOUT, nats sub dies, grep gets EOF → exits 1.
# set +o pipefail so grep's exit code (not nats sub's SIGPIPE 141) drives the result.
if ! (set +o pipefail
      timeout "$SMOKE_TIMEOUT" \
        nats sub "${NATS_OPTS[@]}" "audit.ruby_notifier.notification_sent" \
        | grep -m1 "$SMOKE_ID"
); then
  echo "ERROR: smoke test FAILED — no audit confirmation for ${SMOKE_ID} within ${SMOKE_TIMEOUT}s" >&2
  # Purge the unconsumed smoke message from COMMANDS so the rollback notifier
  # doesn't process it and send a spurious "successful" notification.
  nats stream purge "${NATS_OPTS[@]}" COMMANDS \
    --subject "ruby_engine.commands.notify.${SMOKE_ID}" -f 2>/dev/null || true
  exit 1
fi
echo "  [smoke] PASSED: notifier delivery confirmed for ${SMOKE_ID}"

# =============================================================================
# Ada Postgres round-trip check
# Publishes a synthetic diaper event to HA_EVENTS and polls Postgres to confirm
# the ada processor persisted the row. Validates the HA_EVENTS → engine → Postgres
# path independently of the HTTP gateway path (which is covered by manual Step 15).
# =============================================================================

ADA_SMOKE_ID="ada-smoke-$(date +%s%3N)"
NOW_RFC3339="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

ADA_PAYLOAD=$(printf \
  '{"specversion":"1.0","type":"ha.events.ada.diaper_logged","source":"smoke","id":"%s","time":"%s","correlationid":"","causationid":"","data":{"type":"wet","timestamp":"%s","logged_by":"smoke"}}' \
  "$ADA_SMOKE_ID" "$NOW_RFC3339" "$NOW_RFC3339")

echo "  [smoke/ada] publishing diaper event (id=${ADA_SMOKE_ID})"
nats pub "${NATS_OPTS[@]}" "ha.events.ada.diaper_logged" "$ADA_PAYLOAD"

echo "  [smoke/ada] fetching Postgres credentials from Vault"
PG_PASS=$(_vault kv get -field=password "${VAULT_SECRET_PREFIX}/postgres")
PG_USER=$(_vault kv get -field=user     "${VAULT_SECRET_PREFIX}/postgres")
PG_DB=$(_vault kv get   -field=dbname   "${VAULT_SECRET_PREFIX}/postgres")

echo "  [smoke/ada] polling Postgres for up to ${ADA_TIMEOUT}s"
ADA_PASSED=0
for _i in $(seq 1 "$ADA_TIMEOUT"); do
  COUNT=$(docker exec foundation-postgres \
    env PGPASSWORD="$PG_PASS" \
    psql -U "$PG_USER" -d "$PG_DB" -t -A -c \
    "SELECT COUNT(*) FROM diapers WHERE logged_by = 'smoke' AND created_at > NOW() - INTERVAL '30 seconds'" \
    2>/dev/null || echo "0")
  if [ "${COUNT:-0}" -gt 0 ]; then
    ADA_PASSED=1
    break
  fi
  sleep 1
done

# Clean up smoke rows regardless of outcome.
docker exec foundation-postgres \
  env PGPASSWORD="$PG_PASS" \
  psql -U "$PG_USER" -d "$PG_DB" -c \
  "DELETE FROM diapers WHERE logged_by = 'smoke'" > /dev/null 2>&1 || true

if [ "$ADA_PASSED" -ne 1 ]; then
  echo "ERROR: smoke test FAILED — no diaper row in Postgres within ${ADA_TIMEOUT}s" >&2
  exit 1
fi
echo "  [smoke/ada] PASSED: diaper row confirmed and cleaned up"

echo "  [smoke] ALL CHECKS PASSED"
exit 0
