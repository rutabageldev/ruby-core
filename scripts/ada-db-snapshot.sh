#!/usr/bin/env bash
# ada-db-snapshot.sh — pg_dump the Ada tables for $ENV to a timestamped local file.
# A lightweight, on-demand pre-destructive backup (full automated backups: ROADMAP-0011).
#
# Usage: ENV=<dev|staging|prod> scripts/ada-db-snapshot.sh
#        Override destination with ADA_SNAPSHOT_DIR (default ~/ada-snapshots).

set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ada-db-lib.sh
source "${DIR}/ada-db-lib.sh"

ada_resolve_env
ada_fetch_pg_creds

SNAP_DIR="${ADA_SNAPSHOT_DIR:-${HOME}/ada-snapshots}"
mkdir -p "${SNAP_DIR}"
TS="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="${SNAP_DIR}/ada-${ENV}-${TS}.sql"

echo "=== Snapshot: Ada tables (ENV=${ENV} → ${PG_DBNAME}@${PG_HOST}) ==="
run_pg_dump > "${OUT}"
echo "Wrote ${OUT} ($(wc -c < "${OUT}") bytes)"
echo "Restore (DANGER — overwrites): cat ${OUT} | docker run -i --rm --network postgres \\"
echo "  -e PGPASSWORD=... postgres:16-alpine psql -h ${PG_HOST} -p ${PG_PORT} -U ${PG_USER} -d ${PG_DBNAME}"
echo "See docs/runbooks/ada-test-data.md for the full restore procedure."
