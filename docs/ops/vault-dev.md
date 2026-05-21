# Vault Development Setup

This document describes how to set up HashiCorp Vault for local development per ADR-0015.

## Overview

Ruby Core uses Vault as the exclusive source of truth for secrets (ADR-0015). For local development, we run Vault in dev mode, which:

- Runs entirely in-memory (no persistent storage)
- Automatically unseals on startup
- Enables the UI at <http://localhost:8200/ui>
- Uses a predictable root token for easy access

**WARNING: Dev mode is for local development only. Never use dev mode in production.**

## Option 1: Using Docker Compose (Recommended)

The development compose file includes a pre-configured Vault container:

```bash
cd deploy/dev
docker compose -f compose.dev.yaml up vault
```

This starts Vault with:

- Address: `http://localhost:8201` (port 8201 to avoid conflicts)
- Root token: Set via `VAULT_DEV_TOKEN` env var in your `.env` file

## Option 2: Running Vault Directly

If you prefer to run Vault outside of Docker:

```bash
# Install Vault
# macOS: brew install vault
# Linux: See https://developer.hashicorp.com/vault/downloads

# Start Vault in dev mode (replace with your own token)
vault server -dev -dev-root-token-id="<your-dev-token>"
```

## Configuring Your Environment

Set these environment variables to interact with Vault:

```bash
export VAULT_ADDR='http://127.0.0.1:8201'
export VAULT_TOKEN='<your-dev-token>'
```

Add these to your shell profile (`.bashrc`, `.zshrc`) or to `deploy/dev/.env`:

```bash
# .env file (do NOT commit this file)
VAULT_ADDR=http://127.0.0.1:8201
VAULT_TOKEN=<your-dev-token>
```

**Tip:** Generate a random dev token with: `openssl rand -hex 16`

## Storing Secrets

### NATS Service NKEYs

Each service needs an NKEY seed stored in Vault. Generate keys using:

```bash
# Install nk tool
go install github.com/nats-io/nkeys/nk@latest

# Generate a key pair for a service
nk -gen user -pubout

# Output example (DO NOT USE these values — generate your own):
# SUA...YOUR_SEED_HERE...  (generate with: nk -gen user -pubout)  <- seed (secret)
# UDXU4RCSJNZOIQHZNWXHXORDPRTGNJAHAHFRGZNEEJCPQTT2M7NLCNF4   <- public key
```

Store the seed in Vault:

```bash
# Store gateway NKEY seed (paste your generated seed)
vault kv put secret/ruby-core/nats/gateway \
  seed="<your-generated-seed>"

# Store engine NKEY seed
vault kv put secret/ruby-core/nats/engine \
  seed="<your-generated-seed>"

# Or use the automated script (recommended):
# make setup-creds
```

### TLS Certificates

**Dev now uses direct-PKI issuance** (PLAN-0008 Stage 2; ADR-0030). At service
startup, `pkg/boot/pki.go` AppRole-logs in via mounted role-id + secret-id files
and issues a cert directly from `pki_int/issue/ruby-core-<svc>`. An in-process
goroutine renews at TTL/2. No mkcert dependency; no operator action for routine
rotation.

**NATS server cert is also auto-renewed** by the `nats-cert-renewer` sidecar
(PLAN-0008 follow-up). It's a long-running Vault Agent that uses the same
`foundation-agent-ruby-core-nats-server` AppRole, re-issues the server cert at
TTL/2, writes cert/key/ca atomically into the shared `nats-certs` volume, and
SIGHUPs NATS via the Docker API. NATS reloads its TLS config in-place (~40ms);
existing mTLS connections are unaffected — only new TLS handshakes use the
rotated cert. Config under `deploy/dev/vault-agent/`.

The Vault-side state (AppRoles + PKI roles + scoped policies + role-id files on
disk) is created by foundation's `make setup-pki-ruby-core-roles` +
`make setup-foundation-agent-ruby-core-roles` (PLAN-0008 Stage 1, foundation
PR #78). The compose file bind-mounts the resulting role-id + secret-id files
into each ruby-core container — including the `nats-cert-renewer` sidecar.

**Rollback path (legacy mkcert KV bundle):** still callable when `VAULT_PKI_ROLE`
is unset in compose. `make setup-creds` repopulates `secret/ruby-core/tls/*`
from mkcert. Retained as the durable rollback target until Phase 17.7.

**Historical note (Phase 2 design — superseded by Phase 17.6):** the original
plan was to use mkcert in dev and Vault's PKI engine only in prod, with a
forward-looking sketch like:

```bash
vault secrets enable pki
vault write pki/root/generate/internal \
  common_name="Ruby Core CA" \
  ttl=87600h
vault write pki/config/urls \
  issuing_certificates="http://vault:8200/v1/pki/ca" \
  crl_distribution_points="http://vault:8200/v1/pki/crl"
vault write pki/roles/ruby-core-service \
  allowed_domains="ruby-core.local" \
  allow_subdomains=true \
  max_ttl="720h"
```

## Reading Secrets

Services fetch secrets from Vault at startup:

```bash
# Read a secret
vault kv get secret/ruby-core/nats/gateway

# Read just the seed value
vault kv get -field=seed secret/ruby-core/nats/gateway
```

## Known Limitations (Phase 2 — partly resolved by Phase 17.6)

- **Static VAULT_TOKEN:** Services still authenticate via static token for the
  NKEY + HA + Postgres KV reads. The TLS path moved to AppRole in Phase 17.6
  (PLAN-0008 Stage 2; ADR-0030). The remaining KV reads will follow in
  Phase 17.7 or later as a separate effort.

## Security Notes

1. **Dev tokens are secrets** - The dev root token, while convenient, grants full access to Vault. Treat it as a secret:
   - Never commit it to Git
   - Use `.env` files that are git-ignored
   - Don't share dev tokens between developers

2. **Dev mode limitations**:
   - All data is lost when Vault restarts
   - No TLS by default
   - Root token has unlimited access

3. **Production requirements** (ADR-0015):
   - Use Vault in server mode with proper storage backend
   - Enable TLS
   - Use AppRole or Kubernetes auth instead of root tokens
   - Implement proper access policies

## Troubleshooting

### "connection refused" errors

Ensure Vault is running:

```bash
docker compose -f deploy/dev/compose.dev.yaml ps vault
```

### "permission denied" errors

Verify your token is set:

```bash
echo $VAULT_TOKEN
vault token lookup
```

### "path not found" errors

Enable the KV secrets engine (may be needed if not using -dev mode):

```bash
vault secrets enable -path=secret kv-v2
```

## References

- [ADR-0015: Secrets and Configuration Management](../adr/0015-secrets-config-management.md)
- [Vault Documentation](https://developer.hashicorp.com/vault/docs)
- [NATS NKEY Authentication](https://docs.nats.io/running-a-nats-service/configuration/securing_nats/auth_intro/nkey_auth)
