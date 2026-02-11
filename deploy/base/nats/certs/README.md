# NATS TLS Certificates

TLS certificates for NATS mTLS authentication (ADR-0018) are stored exclusively
in Vault (ADR-0015). No certificate files are kept on the host filesystem.

## How It Works

1. `make setup-creds` generates certificates with mkcert and stores them in Vault
2. At container startup, the `nats-init` service fetches server certs from Vault
   into a RAM-backed (tmpfs) Docker volume
3. NATS reads certs from the shared volume — they never touch the host disk
4. Service containers (gateway, engine) fetch their client certs directly from Vault

## Vault Paths

| Path | Contents |
|------|----------|
| `secret/ruby-core/tls/nats-server` | Server cert, key, and CA |
| `secret/ruby-core/tls/gateway` | Gateway client cert and key |
| `secret/ruby-core/tls/engine` | Engine client cert and key |
| `secret/ruby-core/tls/notifier` | Notifier client cert and key |
| `secret/ruby-core/tls/presence` | Presence client cert and key |
| `secret/ruby-core/tls/admin` | Admin client cert and key |

## Commands

```bash
# Generate and store all certs in Vault
make setup-creds

# Force regeneration of all certs
make setup-creds-force

# Verify the cert is in Vault
vault kv get secret/ruby-core/tls/nats-server
```

## Security Notes

- Server private key is not stored in the repo directory
- Certs live in a Docker-managed named volume, refreshed from Vault on each startup
- Rotate certificates by running `make setup-creds-force` and restarting services
- All secrets managed via Vault (ADR-0015)
