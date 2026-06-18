#!/usr/bin/env bash
# ada-db-seed.sh — clear-then-seed the representative Ada test dataset for $ENV.
#
# Writes a fully test-flagged (test=true, logged_by='seed') ~14-month dataset, then
# — for ENV=prod only — aligns the shared HA test helpers and restarts the prod
# engine so the seed is projected onto the dashboard. Only prod projects to the
# shared HA (non-prod engines run with HA_INGEST_ENABLED=false and do not push,
# ADR-0033), so non-prod seeds populate their DB but not the dashboard.
#
# Usage: ENV=<dev|staging|prod> DOB=<RFC3339> scripts/ada-db-seed.sh

set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ada-db-lib.sh
source "${DIR}/ada-db-lib.sh"

_ha_service() {
    local url="$1" token="$2" service="$3" body="$4"
    if curl -sf -X POST "${url}/api/services/${service}" \
        -H "Authorization: Bearer ${token}" -H "Content-Type: application/json" \
        -d "${body}" >/dev/null; then
        echo "  [OK] ${service} ${body}"
    else
        echo "  [WARN] ${service} call failed — set the helper manually (see runbook)"
    fi
}

# ada_set_ha_test_helpers sets the dashboard's effective test DOB so seeded growth
# ages/percentiles and the masthead render correctly: ada_test_dob = DOB, live_test
# on, born off (the dashboard uses ada_test_dob only while !born && live_test).
ada_set_ha_test_helpers() {
    local ha_json ha_token ha_url dt
    ha_json="$(VAULT_ADDR="${VAULT_ADDR:-https://127.0.0.1:8200}" \
               VAULT_CACERT="${VAULT_CACERT:-/opt/foundation/vault/tls/vault-ca.crt}" \
               VAULT_TOKEN="${VAULT_TOKEN}" \
               vault kv get -format=json secret/ruby-core/ha)"
    ha_token="$(echo "${ha_json}" | jq -r '.data.data.token')"
    ha_url="${ADA_HA_URL:-http://127.0.0.1:8123}"
    dt="$(date -d "${DOB}" '+%Y-%m-%d %H:%M:%S')"
    echo "=== Aligning HA test helpers (${ha_url}) ==="
    _ha_service "${ha_url}" "${ha_token}" input_datetime/set_datetime \
        "{\"entity_id\":\"input_datetime.ada_test_dob\",\"datetime\":\"${dt}\"}"
    _ha_service "${ha_url}" "${ha_token}" input_boolean/turn_on \
        "{\"entity_id\":\"input_boolean.ada_live_test\"}"
    _ha_service "${ha_url}" "${ha_token}" input_boolean/turn_off \
        "{\"entity_id\":\"input_boolean.ada_born\"}"
}

ada_resolve_env
: "${DOB:?DOB is required (RFC3339, e.g. 2025-04-01T08:00:00-05:00)}"
ada_fetch_pg_creds

echo "=== Seeding Ada test data (ENV=${ENV} → ${PG_DBNAME}@${PG_HOST}) DOB=${DOB} ==="
ADA_PSQL_MOUNT="${ADA_REPO_ROOT}/scripts/seed-ada-test-data.sql:/seed.sql:ro" \
    run_psql -v dob="'${DOB}'" -f /seed.sql

if [[ "${ENV}" == "prod" ]]; then
    ada_set_ha_test_helpers
    echo "=== Restarting prod engine to project the seed ==="
    docker restart ruby-core-prod-engine >/dev/null && echo "  prod engine restarted (projecting seed)"
else
    echo ""
    echo "NOTE: ${ENV} database seeded. Only the prod engine projects to the shared"
    echo "      HA (non-prod runs HA_INGEST_ENABLED=false, ADR-0033), so this seed"
    echo "      will NOT appear on the dashboard. Use ENV=prod to validate the"
    echo "      dashboard. The ${ENV} engine is not restarted (it would not project)."
fi
