# notifier

Pull consumer on the `COMMANDS` stream (`ruby_engine.commands.notify.>`). Dispatches push notifications to Home Assistant's `mobile_app` REST API (`POST /api/services/notify/mobile_app_{device}`). After each successful delivery, publishes an audit event to `audit.ruby_notifier.notification_sent` — this is the oracle that the smoke test (`scripts/smoke-test.sh`) polls to confirm end-to-end delivery.

Unlike the engine, the notifier does not use idempotency deduplication. JetStream redelivery backoff is the only retry mechanism.

## Configuration

| Variable | Default | Notes |
|---|---|---|
| `VAULT_ADDR` | `http://127.0.0.1:8200` | Vault server address |
| `VAULT_TOKEN` | *(required)* | Read-only token scoped to `secret/ruby-core/*` |
| `VAULT_NKEY_PATH` | `secret/data/ruby-core/nats/notifier` | NATS NKEY seed |
| `VAULT_TLS_PATH` | `secret/data/ruby-core/tls/notifier` | NATS mTLS cert, key, CA |
| `VAULT_HA_PATH` | `secret/data/ruby-core/ha` | HA base URL and long-lived access token |
| `NATS_URL` | `tls://localhost:4222` | NATS server URL |
| `NATS_REQUIRE_MTLS` | `false` | Force mTLS even if NATS_URL is not `tls://` |
| `ENVIRONMENT` | *(unset)* | Set to `production` to enforce HTTPS Vault |
| `VAULT_ALLOW_HTTP` | `false` | Override HTTPS enforcement for co-located Vault |

## Health check

No HTTP endpoint. Liveness is inferred from NATS pull consumer activity in the logs.

## Known failure modes

**HA config missing at startup** — service starts in degraded mode; notifications are ACKed and silently dropped (not retried) until the service is restarted with a valid `VAULT_HA_PATH` secret. Commands will not accumulate in the stream during this window.

**HA returns non-2xx** — NAK + JetStream backoff redelivery. Logged at `WARN` with `entity_id`, `device`, and `http_status` fields. Persistent failures exhaust `MaxDeliver` and land in the DLQ.

**Malformed command** — missing `device` field or unparseable JSON is ACKed and skipped. This is intentional: malformed messages cannot be retried into a valid state and must not block the consumer.
