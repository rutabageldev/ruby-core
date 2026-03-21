# engine

Automation rules engine. Consumes the `HA_EVENTS` stream (`ha.events.>`) and the `PRESENCE` stream (`ruby_presence.events.>`), evaluates YAML-defined rules (`configs/rules/*.yaml`), and publishes command events to the `COMMANDS` stream and fused presence events to the `PRESENCE` stream.

On startup the engine:

- Ensures all JetStream streams exist: `HA_EVENTS`, `DLQ`, `AUDIT_EVENTS`, `COMMANDS`, `PRESENCE`
- Publishes compiled config (passlist, critical entities) to the `config` NATS KV bucket for the gateway to consume
- Initialises the idempotency deduplication store (hybrid memory + NATS KV, 24h TTL)

## Configuration

| Variable | Default | Notes |
|---|---|---|
| `VAULT_ADDR` | `http://127.0.0.1:8200` | Vault server address |
| `VAULT_TOKEN` | *(required)* | Read-only token scoped to `secret/ruby-core/*` |
| `VAULT_NKEY_PATH` | `secret/data/ruby-core/nats/engine` | NATS NKEY seed |
| `VAULT_TLS_PATH` | `secret/data/ruby-core/tls/engine` | NATS mTLS cert, key, CA |
| `NATS_URL` | `tls://localhost:4222` | NATS server URL |
| `NATS_REQUIRE_MTLS` | `false` | Force mTLS even if NATS_URL is not `tls://` |
| `RULES_DIR` | `configs/rules` | Directory of `*.yaml` rule files |
| `ENVIRONMENT` | *(unset)* | Set to `production` to enforce HTTPS Vault |
| `VAULT_ALLOW_HTTP` | `false` | Override HTTPS enforcement for co-located Vault |
| `ENGINE_FORCE_FAIL` | `false` | **Test hook only.** Forces all events to NAK → DLQ. Never set in production. |

## Health check

No HTTP endpoint. Liveness is inferred from NATS pull consumer activity in the logs.

## Known failure modes

**No rule files found or invalid schema** — exits 1 at boot. Ensure `configs/rules/*.yaml` is present and mounted correctly in the container; all files must declare `schemaVersion: v1`.

**Stream or KV setup failure** — exits 1. Usually indicates NATS is unavailable or the service NKEY lacks the necessary JetStream permissions.

**`ENGINE_FORCE_FAIL=true`** — all events are rejected and routed to the DLQ. This is a deliberate test hook for DLQ verification (see `docs/ops/phase3-verification.md`). If you see this in production logs, the variable was set unintentionally.
