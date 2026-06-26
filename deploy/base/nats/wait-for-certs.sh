#!/bin/sh
# wait-for-certs.sh — NATS entrypoint guard (ADR-0039 / PLAN-0029).
#
# Blocks until the Vault-issued TLS material is present in the shared
# nats-certs volume, then exec's nats-server with the supplied args. This
# decouples NATS startup from the nats-init one-shot's completion state,
# which is NOT re-established on host reboot — so NATS auto-recovers after a
# cold boot using the already-persisted cert, instead of being held down by a
# stale `depends_on: service_completed_successfully` gate.
#
# Bounded wait (~120s). On timeout the script exits non-zero so the
# `restart: unless-stopped` policy retries it: a flapping container is a
# louder signal to monitoring than one silently hung forever.
set -eu

CERTS_DIR="${CERTS_DIR:-/etc/nats/certs}"
TIMEOUT="${CERT_WAIT_TIMEOUT:-120}"
INTERVAL=2
elapsed=0

while :; do
	if [ -s "${CERTS_DIR}/server-cert.pem" ] &&
		[ -s "${CERTS_DIR}/server-key.pem" ] &&
		[ -s "${CERTS_DIR}/ca.pem" ]; then
		echo "wait-for-certs: TLS material present in ${CERTS_DIR} — starting nats-server"
		break
	fi

	if [ "${elapsed}" -ge "${TIMEOUT}" ]; then
		echo "wait-for-certs: timed out after ${TIMEOUT}s waiting for certs in ${CERTS_DIR} — exiting for restart" >&2
		exit 1
	fi

	echo "wait-for-certs: waiting for TLS material in ${CERTS_DIR} (${elapsed}s/${TIMEOUT}s)…"
	sleep "${INTERVAL}"
	elapsed=$((elapsed + INTERVAL))
done

exec nats-server "$@"
