#!/usr/bin/env bash
# ada-db-lib.sh — shared helpers for the Ada test-data lifecycle scripts
# (snapshot / seed / clear). Sourced, not executed directly.
#
# Resolves the target environment to its Vault Postgres path, fetches credentials,
# and runs psql / pg_dump in a throwaway postgres:16-alpine container on the
# `postgres` Docker network (the same path the existing seed tooling used).
#
# Required by callers: ENV must be one of dev | staging | prod.

set -euo pipefail

ADA_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# Exported so sourcing scripts (and shellcheck) see it used.
export ADA_REPO_ROOT
ADA_REPO_ROOT="$(cd "${ADA_LIB_DIR}/.." && pwd)"

# ada_resolve_env validates $ENV and sets ADA_VAULT_PG_PATH for it.
ada_resolve_env() {
    case "${ENV:-}" in
        prod)    ADA_VAULT_PG_PATH="secret/ruby-core/postgres" ;;
        staging) ADA_VAULT_PG_PATH="secret/ruby-core/staging/postgres" ;;
        dev)     ADA_VAULT_PG_PATH="secret/ruby-core/dev/postgres" ;;
        "")      echo "error: ENV is required (dev | staging | prod)" >&2; exit 2 ;;
        *)       echo "error: unknown ENV='${ENV}' (expected dev | staging | prod)" >&2; exit 2 ;;
    esac
    export ADA_VAULT_PG_PATH
}

# ada_fetch_pg_creds loads the VAULT_TOKEN (read-scoped to secret/ruby-core/*) from
# deploy/prod/.env and reads the target env's Postgres credentials into PG_* vars.
ada_fetch_pg_creds() {
    local env_file="${ADA_REPO_ROOT}/deploy/prod/.env"
    if [[ -z "${VAULT_TOKEN:-}" ]]; then
        if [[ ! -f "${env_file}" ]]; then
            echo "error: ${env_file} not found and VAULT_TOKEN unset" >&2
            exit 1
        fi
        VAULT_TOKEN="$(grep '^VAULT_TOKEN=' "${env_file}" | head -1 | cut -d= -f2-)"
        export VAULT_TOKEN
    fi

    local pg_json
    pg_json="$(VAULT_ADDR="${VAULT_ADDR:-https://127.0.0.1:8200}" \
               VAULT_CACERT="${VAULT_CACERT:-/opt/foundation/vault/tls/vault-ca.crt}" \
               VAULT_TOKEN="${VAULT_TOKEN}" \
               vault kv get -format=json "${ADA_VAULT_PG_PATH}")"

    PG_HOST="$(echo "${pg_json}" | jq -r '.data.data.host')"
    PG_PORT="$(echo "${pg_json}" | jq -r '.data.data.port')"
    PG_DBNAME="$(echo "${pg_json}" | jq -r '.data.data.dbname')"
    PG_USER="$(echo "${pg_json}" | jq -r '.data.data.user')"
    PG_PASSWORD="$(echo "${pg_json}" | jq -r '.data.data.password')"
    export PG_HOST PG_PORT PG_DBNAME PG_USER PG_PASSWORD
}

# run_psql executes psql against the resolved database. Extra args are passed through
# (e.g. -f /file.sql, -c "SQL", -v name=value). A file to mount must be passed via
# ADA_PSQL_MOUNT="host_path:container_path".
run_psql() {
    local mount_args=()
    if [[ -n "${ADA_PSQL_MOUNT:-}" ]]; then
        mount_args=(-v "${ADA_PSQL_MOUNT}")
    fi
    docker run --rm --network postgres \
        -e PGPASSWORD="${PG_PASSWORD}" \
        "${mount_args[@]}" \
        postgres:16-alpine \
        psql -h "${PG_HOST}" -p "${PG_PORT}" -U "${PG_USER}" -d "${PG_DBNAME}" \
             -v ON_ERROR_STOP=1 "$@"
}

# run_pg_dump writes a SQL dump of the Ada tables to stdout.
run_pg_dump() {
    docker run --rm --network postgres \
        -e PGPASSWORD="${PG_PASSWORD}" \
        postgres:16-alpine \
        pg_dump -h "${PG_HOST}" -p "${PG_PORT}" -U "${PG_USER}" -d "${PG_DBNAME}" \
            --no-owner --no-privileges \
            -t feedings -t feeding_segments -t feeding_bottle_detail \
            -t diapers -t sleep_sessions -t tummy_time_sessions -t growth_measurements \
            -t medications -t medication_routines -t medication_events \
            -t medication_temp_series -t emergency_rows
}
