#!/usr/bin/env bash
# seed-db.sh — populate the Ada database with representative test data.
# All rows are tagged logged_by='seed' and can be removed with db-seed-clear.
#
# Usage: scripts/seed-db.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

ENV_FILE="${REPO_ROOT}/deploy/prod/.env"
if [[ ! -f "$ENV_FILE" ]]; then
    echo "error: $ENV_FILE not found — run from repo root or check deploy/prod/.env" >&2
    exit 1
fi

# Load VAULT_TOKEN from prod .env (gitignored)
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

echo "=== Seeding database at ${PG_HOST}:${PG_PORT}/${PG_DBNAME} ==="

docker run --rm \
    --network postgres \
    -e PGPASSWORD="$PG_PASSWORD" \
    -v "${SCRIPT_DIR}/seed-data.sql:/seed-data.sql:ro" \
    postgres:16-alpine \
    psql -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d "$PG_DBNAME" \
         -f /seed-data.sql

echo "=== Seed complete ==="
echo "  9 feedings (all source types + supplement combination)"
echo "  8 diapers  (wet, dirty, mixed)"
echo "  3 sleep sessions (1 night, 2 naps)"
echo "  2 tummy time sessions"
echo "  Run 'make db-seed-clear' to remove seeded rows."
