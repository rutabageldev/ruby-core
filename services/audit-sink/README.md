# audit-sink

Sole consumer of the `AUDIT_EVENTS` stream (`audit.>`). Appends every audit event as NDJSON to a local file for long-term archival. Runs as root because it writes to a host-mounted volume (`/var/lib/ruby-core/audit`) that requires root access.

The audit file location defaults to `/data/audit/audit.ndjson` (mapped to the host path in prod compose).

Write failures are logged but the message is still ACKed to avoid infinite retry loops on persistent filesystem errors. The `AUDIT_EVENTS` stream retains messages for 72 hours as a recovery window.

See `docs/ops/jetstream-backup.md` for backup and restore procedures covering this data directory.

## Configuration

| Variable | Default | Notes |
|---|---|---|
| `VAULT_ADDR` | `http://127.0.0.1:8200` | Vault server address |
| `VAULT_TOKEN` | *(required)* | Read-only token scoped to `secret/ruby-core/*` |
| `VAULT_NKEY_PATH` | `secret/data/ruby-core/nats/audit-sink` | NATS NKEY seed |
| `VAULT_TLS_PATH` | `secret/data/ruby-core/tls/audit-sink` | NATS mTLS cert, key, CA |
| `AUDIT_DATA_DIR` | `/data/audit` | Directory where `audit.ndjson` is written |
| `NATS_URL` | `tls://localhost:4222` | NATS server URL |
| `NATS_REQUIRE_MTLS` | `false` | Force mTLS even if NATS_URL is not `tls://` |
| `ENVIRONMENT` | *(unset)* | Set to `production` to enforce HTTPS Vault |
| `VAULT_ALLOW_HTTP` | `false` | Override HTTPS enforcement for co-located Vault |

## Health check

No HTTP endpoint. Liveness is inferred from NATS pull consumer activity in the logs (`audit-sink: event archived`).

## Known failure modes

**Write failure (disk full, permissions)** — event is ACKed and logged at `WARN`. The NDJSON file will have gaps. Investigate disk usage on the host at `/var/lib/ruby-core/audit`. Events missed during a short outage can be replayed from the stream if the sink is down for less than 72 hours.

**Service down for > 72 hours** — NATS drops unconsumed messages per the stream retention policy. Audit events published during the outage window are permanently lost. Requires manual investigation of what occurred during the gap.

**NATS or Vault unreachable** — exits 1 immediately.
