#!/usr/bin/env bash
# clear-seed-db.sh — remove all rows inserted by seed-db.sh.
# Targets logged_by='seed' across all Ada tables. FK cascades automatically
# remove feeding_segments and feeding_bottle_detail rows.
#
# Usage: scripts/clear-seed-db.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

ENV_FILE="${REPO_ROOT}/deploy/prod/.env"
if [[ ! -f "$ENV_FILE" ]]; then
    echo "error: $ENV_FILE not found — run from repo root or check deploy/prod/.env" >&2
    exit 1
fi

# shellcheck disable=SC1090
. "$ENV_FILE"

echo "=== Fetching Postgres credentials from Vault ==="
PG_JSON=$(VAULT_ADDR=https://127.0.0.1:8200 \
          VAULT_CACERT=/opt/foundation/vault/tls/vault-ca.crt \
          VAULT_TOKEN="$VAULT_TOKEN" \
          vault kv get -format=json secret/ruby-core/postgres)

PG_HOST=$(echo "$PG_JSON" | jq -r '.data.data.host')
PG_PORT=$(echo "$PG_JSON" | jq -r '.data.data.port')
PG_DBNAME=$(echo "$PG_JSON" | jq -r '.data.data.dbname')
PG_USER=$(echo "$PG_JSON" | jq -r '.data.data.user')
PG_PASSWORD=$(echo "$PG_JSON" | jq -r '.data.data.password')

echo "=== Clearing seeded data from ${PG_HOST}:${PG_PORT}/${PG_DBNAME} ==="

docker run --rm \
    --network postgres \
    -e PGPASSWORD="$PG_PASSWORD" \
    postgres:16-alpine \
    psql -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d "$PG_DBNAME" \
    -c "
        DELETE FROM feedings           WHERE logged_by = 'seed';
        DELETE FROM diapers            WHERE logged_by = 'seed';
        DELETE FROM sleep_sessions     WHERE logged_by = 'seed';
        DELETE FROM tummy_time_sessions WHERE logged_by = 'seed';
    " \
    -c "SELECT 'feedings remaining (seed):', COUNT(*) FROM feedings WHERE logged_by = 'seed'
        UNION ALL
        SELECT 'diapers remaining (seed):',  COUNT(*) FROM diapers  WHERE logged_by = 'seed';"

echo "=== Clear complete ==="
