# gateway

Ingests Home Assistant state events via WebSocket and publishes them to the `HA_EVENTS` JetStream stream (`ha.events.>`). Normalizes raw HA payloads using a passlist compiled by the engine. Reconciles critical entity state on reconnect. Publishes a `gateway.health` heartbeat every 15 seconds (ADR-0008).

Also handles Ada baby tracking events via two paths:

- **HA WebSocket** (primary): subscribes to `ada_event` on the HA event bus alongside `state_changed`. Dashboard cards fire `hass.fireEvent('ada_event', payload)` — the gateway receives it over the existing authenticated WebSocket connection and publishes to `HA_EVENTS`. Works on LAN and remotely via Nabu Casa.
- **HTTP** (server-side tooling): `POST /ada/events` accepts the same payload and is used by smoke tests and scripts. Not called by the dashboard.

Both paths produce identical CloudEvents on `ha.events.ada.>`. See `services/gateway/ada/publish.go` for the event type → subject mapping.

## `ruby_home_event` — domain-neutral write path (ROADMAP-0012)

Alongside `ada_event`, the gateway subscribes to a domain-neutral `ruby_home_event` on the HA event bus. New home-automation write contracts (calendar, childcare, …) ride this single event type instead of getting a bespoke HA event type per domain. The NATS subject is derived from the payload `event` string; see `services/gateway/rubyhome/publish.go` and the shared subject constants in `pkg/schemas/homecal.go`.

### Home Assistant producer contract

Fire `ruby_home_event` with the caller's payload wrapped under a `payload` key (same convention as `ada_event`'s `script.fire_ada_event` intermediary), and an `event` field set to one of the route keys:

| `event` | NATS subject | Consumer |
|---|---|---|
| `calendar.event.upsert` | `ha.events.calendar.event_upsert` | calendar processor (Slice C) |
| `calendar.event.delete` | `ha.events.calendar.event_delete` | calendar processor (Slice C) |
| `ruby_home.childcare.provider.upsert` | `ha.events.ruby_home.childcare.provider_upsert` | overlay (Slice D) |
| `ruby_home.childcare.provider.delete` | `ha.events.ruby_home.childcare.provider_delete` | overlay (Slice D) |

Example HA event data: `{"payload": {"event": "calendar.event.upsert", "summary": "Dentist", "start": {...}, "idempotency_key": "…"}}`. Full payload field contracts are in ROADMAP-0012.

> The HA-side producer migration (firing `ruby_home_event`, and the eventual retirement of `ada_event` once all producers move over) is cross-repo work in the `homeassistant` repo and is **not** part of this repo. The gateway dual-subscribes so the cutover is non-breaking.

External access is routed through Traefik; the HTTP port is never published directly to the host (ADR-0020).

## Configuration

All secrets are fetched from Vault at startup. The service will not start if Vault or NATS are unreachable.

| Variable | Default | Notes |
|---|---|---|
| `VAULT_ADDR` | `http://127.0.0.1:8200` | Vault server address |
| `VAULT_TOKEN` | *(required)* | Read-only token scoped to `secret/ruby-core/*` |
| `VAULT_NKEY_PATH` | `secret/data/ruby-core/nats/gateway` | NATS NKEY seed |
| `VAULT_TLS_PATH` | `secret/data/ruby-core/tls/gateway` | NATS mTLS cert, key, CA |
| `VAULT_HA_PATH` | `secret/data/ruby-core/ha` | HA base URL and long-lived access token |
| `NATS_URL` | `tls://localhost:4222` | NATS server URL |
| `NATS_REQUIRE_MTLS` | `false` | Force mTLS even if NATS_URL is not `tls://` |
| `HTTP_ADDR` | `:8080` | Bind address for the health endpoint |
| `ENVIRONMENT` | *(unset)* | Set to `production` to enforce HTTPS Vault |
| `HA_INGEST_ENABLED` | *(unset → enabled)* | Set to `false` to disable Home Assistant ingestion (no WebSocket; degraded mode). All environments share one HA, so only prod should ingest — non-prod gateways set this to `false`. |
| `VAULT_ALLOW_HTTP` | `false` | Override HTTPS enforcement for co-located Vault |

## Health check

`GET /health` at `HTTP_ADDR` → `200 {"status":"ok"}`

## Known failure modes

**HA secret missing or Vault read fails at startup** — gateway starts in degraded mode: health endpoint is up, HA WebSocket client is disabled. State events will not be ingested until the service is restarted with a valid `VAULT_HA_PATH` secret. Logged at `WARN` level.

**Engine config KV absent at startup** — gateway starts with a pass-all passlist (no filtering) and an empty critical entities list (no reconciliation). This is the safe default for startup ordering; it self-corrects once the engine has published its compiled config.

**NATS or Vault unreachable** — exits 1 immediately. The container will restart per the compose `restart: unless-stopped` policy.
