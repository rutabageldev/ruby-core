#!/usr/bin/env bash
# ada-birth-watch.sh — host watcher that performs the Ada birth clean slate (ADR-0036).
#
# On the FIRST ada.born it runs the existing snapshot-then-nuke (ada-db-clear-test.sh,
# which pg_dumps the database BEFORE deleting and aborts the delete on snapshot failure),
# validates the result, then writes a sentinel and spins itself down so it can never
# re-fire. Run as a long-lived systemd service; idempotent across restarts.
#
# Usage: ENV=prod scripts/ada-birth-watch.sh
#
# Tunables: ADA_BIRTH_SENTINEL (default ~/.ada-birth-handled),
#           ADA_SNAPSHOT_DIR (default ~/ada-snapshots).

set -uo pipefail # not -e: failures are handled explicitly and retried

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export ENV="${ENV:-prod}"
SENTINEL="${ADA_BIRTH_SENTINEL:-${HOME}/.ada-birth-handled}"
STREAM="HA_EVENTS"
CONSUMER="ada-birth-watcher"
SUBJECT="ha.events.ada.born"

log() { echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) [ada-birth-watch] $*"; }

if [[ -f "${SENTINEL}" ]]; then
    log "birth already handled (${SENTINEL} present) — nothing to do, exiting"
    exit 0
fi

# Resolve PG creds once (used by the validation queries below).
# shellcheck source=scripts/ada-db-lib.sh
source "${DIR}/ada-db-lib.sh"
set +e # ada-db-lib.sh enables -e; restore our deliberate non-e mode (see line 14)
ada_resolve_env
ada_fetch_pg_creds

# validate_nuke returns 0 only if the clean slate truly took: no test=true rows remain
# in any tracking table, the engine is running, and a snapshot file was produced.
validate_nuke() {
    local remaining
    remaining="$(run_psql -tA -c "
        SELECT (SELECT count(*) FROM feedings WHERE test = true)
             + (SELECT count(*) FROM diapers WHERE test = true)
             + (SELECT count(*) FROM sleep_sessions WHERE test = true)
             + (SELECT count(*) FROM tummy_time_sessions WHERE test = true)
             + (SELECT count(*) FROM growth_measurements WHERE test = true);" 2>/dev/null | tr -d '[:space:]')"
    if [[ "${remaining}" != "0" ]]; then
        log "validate: ${remaining:-?} test=true rows remain — nuke incomplete"
        return 1
    fi
    if ! docker inspect -f '{{.State.Running}}' "ruby-core-${ENV}-engine" 2>/dev/null | grep -q true; then
        log "validate: ruby-core-${ENV}-engine is not running"
        return 1
    fi
    if ! ls -1 "${ADA_SNAPSHOT_DIR:-${HOME}/ada-snapshots}"/ada-"${ENV}"-*.sql >/dev/null 2>&1; then
        log "validate: no snapshot file found"
        return 1
    fi
    return 0
}

# handle_birth runs snapshot → clear → restart (the existing script, which snapshots
# first and aborts the delete on snapshot failure) and validates, with idempotent retries.
handle_birth() {
    local attempt
    for attempt in 1 2 3 4 5; do
        log "clean-slate attempt ${attempt}: snapshot -> clear -> restart engine"
        if ENV="${ENV}" CONFIRM=yes ASSUME_YES=1 "${DIR}/ada-db-clear-test.sh"; then
            if validate_nuke; then
                log "validated: pg_dump taken, test data cleared, engine running"
                return 0
            fi
        fi
        log "attempt ${attempt} did not validate; retrying in 30s"
        sleep 30
    done
    return 1
}

# Durable pull consumer filtered to ada.born (idempotent — fails harmlessly if it exists).
ENV="${ENV}" "${DIR}/nats-admin.sh" consumer add "${STREAM}" "${CONSUMER}" \
    --filter "${SUBJECT}" --pull --ack explicit --deliver all --replay instant \
    --max-deliver 1000 --defaults >/dev/null 2>&1 || true

log "watching ${STREAM} for ${SUBJECT} (consumer ${CONSUMER})"
while true; do
    if [[ -f "${SENTINEL}" ]]; then
        log "sentinel present — spinning down (will not re-fire)"
        exit 0
    fi
    # Block up to 50s for the next ada.born; the CLI acks it on receipt.
    if ENV="${ENV}" "${DIR}/nats-admin.sh" consumer next "${STREAM}" "${CONSUMER}" \
        --count 1 --timeout 50s --raw >/dev/null 2>&1; then
        log "ada.born received — running clean slate"
        if handle_birth; then
            touch "${SENTINEL}"
            log "birth clean-slate complete and validated — spinning down (will not re-fire)"
            exit 0
        fi
        log "CRITICAL: birth clean-slate failed after retries. The snapshot may have been taken but the wipe did not validate — investigate before relying on the dashboard. Leaving the watcher running."
    fi
done
