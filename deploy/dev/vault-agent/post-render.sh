#!/bin/sh
# Vault Agent post-render hook for the NATS server cert bundle.
#
# Splits /rendered/nats-bundle.tmp (cert + ==CA== + ca + ==KEY== + key) into
# the three files NATS reads from its shared `nats-certs` volume, then sends
# SIGHUP to the NATS container via the Docker API. NATS reloads its TLS
# config in-place (~40ms); existing mTLS connections survive — only new
# TLS handshakes use the rotated cert.
#
# Runs inside the foundation/vault-agent:1.21.4 image (hashicorp/vault + curl).
# busybox tools only otherwise (sh, sed, grep, mv).
#
# Atomic-write pattern: writes to /rendered/.*.tmp first, validates PEM
# markers, then `mv` into the shared volume. Under cap_drop: ALL the agent
# (container root) can't overwrite a file in place but `mv` only needs write
# permission on the target directory, which it has.
set -eu

BUNDLE=/rendered/nats-bundle.tmp
CERTS_DIR=/certs
NATS_CONTAINER="${NATS_CONTAINER:-ruby-core-dev-nats}"

CERT_TMP=/rendered/.server-cert.pem.tmp
CA_TMP=/rendered/.ca.pem.tmp
KEY_TMP=/rendered/.server-key.pem.tmp

if [ ! -s "$BUNDLE" ]; then
  echo "post-render: bundle empty or missing — abort" >&2
  exit 1
fi

# Split on ==CA== and ==KEY== separators.
#   - Lines 1..==CA== (exclusive) → cert
#   - Lines ==CA==..==KEY== (exclusive on both ends) → CA
#   - Lines ==KEY==..end (exclusive) → key
sed -n '1,/^==CA==$/p'           "$BUNDLE" | sed '/^==CA==$/d'                  > "$CERT_TMP"
sed -n '/^==CA==$/,/^==KEY==$/p' "$BUNDLE" | sed -e '/^==CA==$/d' -e '/^==KEY==$/d' > "$CA_TMP"
sed -n '/^==KEY==$/,$p'          "$BUNDLE" | sed '/^==KEY==$/d'                 > "$KEY_TMP"

# Validate all three have the expected PEM markers. If anything is wrong,
# abort BEFORE moving into the shared volume — NATS keeps using its last-good
# cert and the Vault Agent retries on the next template tick.
if ! grep -q 'BEGIN CERTIFICATE' "$CERT_TMP"; then
  echo "post-render: $CERT_TMP missing BEGIN CERTIFICATE — abort" >&2
  rm -f "$CERT_TMP" "$CA_TMP" "$KEY_TMP"
  exit 1
fi
if ! grep -q 'BEGIN CERTIFICATE' "$CA_TMP"; then
  echo "post-render: $CA_TMP missing BEGIN CERTIFICATE — abort" >&2
  rm -f "$CERT_TMP" "$CA_TMP" "$KEY_TMP"
  exit 1
fi
if ! grep -qE 'BEGIN .* PRIVATE KEY' "$KEY_TMP"; then
  echo "post-render: $KEY_TMP missing BEGIN .* PRIVATE KEY — abort" >&2
  rm -f "$CERT_TMP" "$CA_TMP" "$KEY_TMP"
  exit 1
fi

# Mode 0644 — same as nats-init writes. Volume scope is bounded to this
# sidecar + nats-init + NATS, so "world-readable" is just those three.
chmod 0644 "$CERT_TMP" "$CA_TMP" "$KEY_TMP"

mv "$CERT_TMP" "$CERTS_DIR/server-cert.pem"
mv "$KEY_TMP"  "$CERTS_DIR/server-key.pem"
mv "$CA_TMP"   "$CERTS_DIR/ca.pem"
rm -f "$BUNDLE"

# Reload or recover NATS via the Docker API, depending on its current state:
#   - running → POST /kill?signal=SIGHUP: NATS reloads TLS in place without
#     dropping existing connections (server/reload.go validates the new cert
#     before Apply; a malformed cert keeps the old config).
#   - stopped → POST /start: bring NATS up with the freshly written cert. This
#     is the boot-recovery path (ADR-0039) — a SIGHUP cannot start a stopped
#     container, so the renewer must start it rather than deadlock retrying.
# Only a genuine Docker API/transport error is fatal; NATS being down is an
# expected, recoverable state and must not fail the render.
SOCK=/var/run/docker.sock
API="http://localhost/containers/${NATS_CONTAINER}"

RUNNING=$(curl --silent --unix-socket "$SOCK" --max-time 10 "${API}/json" |
  grep -o '"Running":[a-z]*' | head -n1 | cut -d: -f2)

if [ "$RUNNING" = "true" ]; then
  HTTP_CODE=$(curl --silent --unix-socket "$SOCK" -X POST --max-time 10 \
    -o /dev/null -w '%{http_code}' "${API}/kill?signal=SIGHUP" || echo "000")
  case "$HTTP_CODE" in
    204) echo "post-render: cert written to ${CERTS_DIR}; SIGHUP reloaded ${NATS_CONTAINER}" ;;
    *) echo "post-render: cert written but SIGHUP of ${NATS_CONTAINER} returned HTTP $HTTP_CODE" >&2; exit 1 ;;
  esac
elif [ "$RUNNING" = "false" ]; then
  HTTP_CODE=$(curl --silent --unix-socket "$SOCK" -X POST --max-time 10 \
    -o /dev/null -w '%{http_code}' "${API}/start" || echo "000")
  case "$HTTP_CODE" in
    204 | 304) echo "post-render: cert written to ${CERTS_DIR}; ${NATS_CONTAINER} was stopped — started it (ADR-0039 recovery)" ;;
    *) echo "post-render: cert written but start of ${NATS_CONTAINER} returned HTTP $HTTP_CODE" >&2; exit 1 ;;
  esac
else
  echo "post-render: cert written but could not read ${NATS_CONTAINER} state from Docker API" >&2
  exit 1
fi
