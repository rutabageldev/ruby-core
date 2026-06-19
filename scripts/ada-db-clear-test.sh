#!/usr/bin/env bash
# ada-db-clear-test.sh — DESTRUCTIVE. Delete ONLY test=true Ada rows for $ENV.
#
# Guards:
#   1. Dry-run by default — prints per-table test counts and deletes nothing
#      unless CONFIRM=yes.
#   2. ENV=prod requires typing the database name to proceed.
#   3. A pre-delete snapshot is taken first (ada-db-snapshot.sh).
#   4. Every DELETE carries WHERE test = true; real data (test=false) is untouched.
#
# Usage: ENV=<dev|staging|prod> [CONFIRM=yes] scripts/ada-db-clear-test.sh

set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ada-db-lib.sh
source "${DIR}/ada-db-lib.sh"

ada_resolve_env
ada_fetch_pg_creds

echo "=== Ada test-data CLEAR (ENV=${ENV} → ${PG_DBNAME}@${PG_HOST}) ==="
echo "Current test=true row counts:"
run_psql -tA -c "
    SELECT 'feedings='            || count(*) FROM feedings            WHERE test = true
    UNION ALL SELECT 'diapers='   || count(*) FROM diapers             WHERE test = true
    UNION ALL SELECT 'sleep='     || count(*) FROM sleep_sessions      WHERE test = true
    UNION ALL SELECT 'tummy='     || count(*) FROM tummy_time_sessions WHERE test = true
    UNION ALL SELECT 'growth='    || count(*) FROM growth_measurements WHERE test = true;"

if [[ "${CONFIRM:-}" != "yes" ]]; then
    echo ""
    echo "DRY RUN — no rows deleted. To delete ONLY the test=true rows above:"
    echo "  make ada-db-clear-test ENV=${ENV} CONFIRM=yes"
    exit 0
fi

if [[ "${ENV}" == "prod" && "${ASSUME_YES:-}" != "1" ]]; then
    echo ""
    echo "!! PRODUCTION CLEAR !! This permanently deletes test=true Ada rows from"
    echo "   ${PG_DBNAME}@${PG_HOST}. Real (test=false) data is NOT affected."
    read -r -p "Type the database name (${PG_DBNAME}) to proceed: " ans
    if [[ "${ans}" != "${PG_DBNAME}" ]]; then
        echo "aborted (input did not match)"
        exit 1
    fi
elif [[ "${ENV}" == "prod" ]]; then
    echo "ASSUME_YES=1 — skipping the prod confirmation prompt (non-interactive automation)."
fi

echo "=== Pre-delete snapshot ==="
"${DIR}/ada-db-snapshot.sh"

echo "=== Deleting test=true rows ==="
run_psql -c "
    DELETE FROM feedings            WHERE test = true;
    DELETE FROM diapers             WHERE test = true;
    DELETE FROM sleep_sessions      WHERE test = true;
    DELETE FROM tummy_time_sessions WHERE test = true;
    DELETE FROM growth_measurements WHERE test = true;"

echo "=== Verify (all must be 0) ==="
run_psql -tA -c "
    SELECT 'feedings='            || count(*) FROM feedings            WHERE test = true
    UNION ALL SELECT 'diapers='   || count(*) FROM diapers             WHERE test = true
    UNION ALL SELECT 'sleep='     || count(*) FROM sleep_sessions      WHERE test = true
    UNION ALL SELECT 'tummy='     || count(*) FROM tummy_time_sessions WHERE test = true
    UNION ALL SELECT 'growth='    || count(*) FROM growth_measurements WHERE test = true;"

if [[ "${ENV}" == "prod" ]]; then
    echo "=== Restarting prod engine to recompute sensors ==="
    docker restart ruby-core-prod-engine >/dev/null && echo "  prod engine restarted"
fi
echo "=== Clear complete ==="
