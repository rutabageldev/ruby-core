# engine

Automation rules engine. Consumes the `HA_EVENTS` stream (`ha.events.>`) and the `PRESENCE` stream (`ruby_presence.events.>`), evaluates YAML-defined rules (`configs/rules/*.yaml`), and publishes command events to the `COMMANDS` stream and fused presence events to the `PRESENCE` stream.

On startup the engine:

- Ensures all JetStream streams exist: `HA_EVENTS`, `DLQ`, `AUDIT_EVENTS`, `COMMANDS`, `PRESENCE`
- Publishes compiled config (passlist, critical entities) to the `config` NATS KV bucket for the gateway to consume
- Initialises the idempotency deduplication store (hybrid memory + NATS KV, 24h TTL)
- If any registered processor requires storage (ADR-0029): fetches Postgres credentials from Vault, runs schema migrations, connects a connection pool

## Processors

| Processor | Stateful | Subscriptions |
|---|---|---|
| `presence_notify` | No | `ha.events.>`, `ruby_presence.events.>` |
| `ada` | Yes (Postgres) | `ha.events.ada.>`, `ha.events.input_number.ada_alert_threshold_h` |

The `ada` processor persists feeding, diaper, sleep, and tummy time events to PostgreSQL and pushes derived sensor state to Home Assistant after each event. It also subscribes to the bare `gateway.health` subject to restore HA sensor state after a gateway reconnect. A background ticker runs every 60 seconds to push `sensor.ada_sleep_session_min` while a session is active, refresh daily aggregates at midnight rollover, and perform a full sensor restore every 4 hours as a safety net against HA state loss.

## Configuration

| Variable | Default | Notes |
|---|---|---|
| `VAULT_ADDR` | `http://127.0.0.1:8200` | Vault server address |
| `VAULT_TOKEN` | *(required)* | Read-only token scoped to `secret/ruby-core/*` |
| `VAULT_NKEY_PATH` | `secret/data/ruby-core/nats/engine` | NATS NKEY seed |
| `VAULT_TLS_PATH` | `secret/data/ruby-core/tls/engine` | NATS mTLS cert, key, CA |
| `VAULT_PG_PATH` | `secret/data/ruby-core/postgres` | Postgres credentials (host, port, dbname, user, password) — only read if a stateful processor is registered |
| `VAULT_HA_PATH` | `secret/data/ruby-core/ha` | HA base URL and access token — only read if a stateful processor is registered |
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

**Postgres unreachable or credentials missing at boot** — exits 1 if any stateful processor is registered and the Postgres connection cannot be established. Check that `secret/data/ruby-core/postgres` exists in Vault and that `foundation-postgres` is reachable from the engine container.
