# NATS TLS Certificates

This directory contains TLS certificates for NATS mTLS authentication (ADR-0018).

## Required Files

- `ca.pem` - Certificate Authority certificate
- `server-cert.pem` - NATS server certificate
- `server-key.pem` - NATS server private key

## Development Setup

For local development, use `mkcert` to generate certificates:

```bash
# Install mkcert (if not already installed)
# macOS: brew install mkcert
# Linux: See https://github.com/FiloSottile/mkcert#installation

# Install the local CA
mkcert -install

# Generate NATS server certificate
cd deploy/base/nats/certs
mkcert -cert-file server-cert.pem -key-file server-key.pem localhost 127.0.0.1 nats

# Copy the CA certificate
cp "$(mkcert -CAROOT)/rootCA.pem" ca.pem
```

## Production Setup

For production, use Vault's PKI Secrets Engine:

```bash
# Enable PKI secrets engine
vault secrets enable pki

# Configure CA (see docs/ops/vault-dev.md for full setup)
vault write pki/root/generate/internal \
  common_name="Ruby Core CA" \
  ttl=87600h

# Generate NATS server certificate
vault write pki/issue/nats-server \
  common_name="nats.ruby-core.local" \
  ttl="720h"
```

## Security Notes

- **Never commit real certificates or keys to Git**
- Server keys must be readable only by the NATS process
- Rotate certificates before expiration
- Store all secrets in Vault (ADR-0015)

## Pre-Deployment Validation

Before deploying NATS, run the configuration validator to catch common issues:

```bash
cd deploy/base/nats
./validate-config.sh

# Or specify a different config file:
./validate-config.sh /path/to/nats.conf
```

The validator checks for:
- Unreplaced placeholder NKEYs
- Missing TLS certificate files
- Required TLS configuration
- Default deny permissions

## Client Certificates

Each service needs its own client certificate for mTLS. Generate using:

```bash
# For development (using mkcert)
mkcert -client -cert-file gateway-cert.pem -key-file gateway-key.pem gateway

# For production (using Vault)
vault write pki/issue/service-client \
  common_name="gateway.ruby-core.local" \
  ttl="720h"
```
