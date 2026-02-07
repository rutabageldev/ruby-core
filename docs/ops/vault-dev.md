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

# Output example:
# SUAIBDPBAUTWCWBKIO6XHQNINK5FWJW4OHLXC3HQ2KFE4PEJUA44CNHTC4  <- seed (secret)
# UDXU4RCSJNZOIQHZNWXHXORDPRTGNJAHAHFRGZNEEJCPQTT2M7NLCNF4   <- public key
```

Store the seed in Vault:

```bash
# Store gateway NKEY seed
vault kv put secret/ruby-core/nats/gateway \
  seed="SUAIBDPBAUTWCWBKIO6XHQNINK5FWJW4OHLXC3HQ2KFE4PEJUA44CNHTC4"

# Store engine NKEY seed
vault kv put secret/ruby-core/nats/engine \
  seed="<generated-seed>"
```

### TLS Certificates

For development, generate certificates with mkcert (see `deploy/base/nats/certs/README.md`).

For production, use Vault's PKI engine:

```bash
# Enable PKI secrets engine
vault secrets enable pki

# Generate root CA
vault write pki/root/generate/internal \
  common_name="Ruby Core CA" \
  ttl=87600h

# Configure CA and CRL URLs
vault write pki/config/urls \
  issuing_certificates="http://vault:8200/v1/pki/ca" \
  crl_distribution_points="http://vault:8200/v1/pki/crl"

# Create a role for issuing certificates
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

## Known Limitations (Phase 2)

- **Static VAULT_TOKEN:** Services authenticate to Vault using a static token passed via environment variable. This is acceptable for Phase 2 but should be migrated to AppRole or Kubernetes auth in a future phase (ADR-0015).

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

- [ADR-0015: Secrets and Configuration Management](../../ADRs/0015-secrets-config-management.md)
- [Vault Documentation](https://developer.hashicorp.com/vault/docs)
- [NATS NKEY Authentication](https://docs.nats.io/running-a-nats-service/configuration/securing_nats/auth_intro/nkey_auth)
