#!/usr/bin/env bash
# smoke-test.sh VERSION
#
# Verifies the prod NATS → notifier → HA delivery chain by publishing a
# synthetic COMMANDS message and waiting for the resulting audit event on
# audit.ruby_notifier.notification_sent.
#
# Environment:
#   VAULT_TOKEN    (required) Vault token with read access to secret/ruby-core/*
#   VAULT_ADDR     (default: https://127.0.0.1:8200)
#   VAULT_CACERT   (default: /opt/foundation/vault/tls/vault-ca.crt)
#   ROLLBACK_FROM  (optional) If set, message reads "vX.X.X failed — rollback to VERSION successful"
#   SMOKE_TIMEOUT  (default: 30) Seconds to wait for audit confirmation
#
# Exit codes:
#   0  smoke test passed (push notification delivered and confirmed)
#   1  smoke test failed (Vault unreachable, NATS connection failed, or timeout)
set -euo pipefail

VERSION="${1:?Usage: smoke-test.sh VERSION}"
ROLLBACK_FROM="${ROLLBACK_FROM:-}"
SMOKE_TIMEOUT="${SMOKE_TIMEOUT:-30}"
VAULT_ADDR="${VAULT_ADDR:-https://127.0.0.1:8200}"
VAULT_CACERT="${VAULT_CACERT:-/opt/foundation/vault/tls/vault-ca.crt}"

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

if ! _vault kv get -field=seed secret/ruby-core/nats/admin > "$TMPDIR/seed.nk" 2>&1; then
  echo "ERROR: cannot fetch admin NKEY seed from Vault (check VAULT_TOKEN and Vault availability)" >&2
  exit 1
fi
chmod 600 "$TMPDIR/seed.nk"

_vault kv get -field=cert secret/ruby-core/tls/admin > "$TMPDIR/client.crt"
_vault kv get -field=key  secret/ruby-core/tls/admin > "$TMPDIR/client.key"
_vault kv get -field=ca   secret/ruby-core/tls/admin > "$TMPDIR/ca.crt"
chmod 600 "$TMPDIR/client.key"

# Millisecond-unique smoke ID so grep matches only this run's audit event.
SMOKE_ID="smoke-$(date +%s%3N)"

# Build notification message based on whether this is a normal deploy or rollback.
if [ -n "$ROLLBACK_FROM" ]; then
  TITLE="Deployment failed"
  MSG="ruby-core ${ROLLBACK_FROM} failed — rollback to ${VERSION} successful at $(date +%H:%M)"
else
  TITLE="Deployment successful"
  MSG="ruby-core ${VERSION} deployed at $(date +%H:%M)"
fi

NATS_OPTS=(
  --server  "tls://127.0.0.1:4223"
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
if (set +o pipefail
    timeout "$SMOKE_TIMEOUT" \
      nats sub "${NATS_OPTS[@]}" "audit.ruby_notifier.notification_sent" \
      | grep -m1 "$SMOKE_ID"
); then
  echo "  [smoke] PASSED: delivery confirmed for ${SMOKE_ID}"
  exit 0
fi

echo "ERROR: smoke test FAILED — no audit confirmation for ${SMOKE_ID} within ${SMOKE_TIMEOUT}s" >&2
# Purge the unconsumed smoke message from COMMANDS so the rollback notifier
# doesn't process it and send a spurious "successful" notification.
nats stream purge "${NATS_OPTS[@]}" COMMANDS \
  --subject "ruby_engine.commands.notify.${SMOKE_ID}" -f 2>/dev/null || true
exit 1
