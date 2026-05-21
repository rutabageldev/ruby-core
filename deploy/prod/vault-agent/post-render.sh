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

# ─────────────────────────────────────────────────────────────────────────
# TRANSITIONAL: deliberately DO NOT overwrite ca.pem on rotation.
# nats-init writes a BUNDLED ca.pem (pki_int issuing_ca + legacy mkcert CA)
# at startup; overwriting here with PKI-only would drop the mkcert anchor
# and break the admin smoke path. The renewer's AppRole has no KV read
# capability so it cannot reproduce the bundle on its own — letting
# nats-init's bundle persist is the simpler, correct behavior.
#
# Both CAs are stable for years (RubyNet Intermediate, mkcert root), so a
# static ca.pem across cert rotations is fine. The renewer just rotates
# cert+key.
#
# *** REMOVE the discard-CA behavior in PLAN-0008 Stage 4 ***
# Once admin migrates to PKI in Stage 4, the bundle becomes unnecessary and
# this script can resume writing ca.pem with just the PKI issuing_ca.
# ─────────────────────────────────────────────────────────────────────────
rm -f "$CA_TMP"
rm -f "$BUNDLE"

# SIGHUP NATS via the Docker API. /kill (not /restart) with signal=SIGHUP
# triggers NATS's config reload without dropping existing connections.
# NATS validates the new cert during reload; if it's malformed it keeps the
# old config (server/reload.go's validateOptions runs before Apply).
HTTP_CODE=$(curl --silent --unix-socket /var/run/docker.sock \
  -X POST --max-time 10 \
  -o /dev/null -w '%{http_code}' \
  "http://localhost/containers/${NATS_CONTAINER}/kill?signal=SIGHUP" || echo "000")

case "$HTTP_CODE" in
  204)
    echo "post-render: cert+key written to ${CERTS_DIR} (ca.pem preserved from nats-init); SIGHUP sent to ${NATS_CONTAINER}"
    ;;
  *)
    echo "post-render: write OK but SIGHUP call returned HTTP $HTTP_CODE" >&2
    exit 1
    ;;
esac
