# presence

Fused presence detection for a single tracked person. Subscribes to phone device tracker state changes from the `HA_EVENTS` stream, corroborates uncertain states with WiFi entity presence via the HA REST API, applies a configurable debounce, and publishes the resolved state to the `PRESENCE` stream (`ruby_presence.events.state.{personID}`).

One service instance runs per tracked person. The current deployment tracks `katie` (`phone.katie`).

## Configuration

**Required:**

| Variable | Notes |
|---|---|
| `PRESENCE_PERSON_ID` | Identifier for the tracked person (e.g. `katie`). Used as the durable consumer name and published subject suffix. |
| `PRESENCE_PHONE_ENTITY` | HA entity ID in `domain.name` format (e.g. `phone.katie`). |
| `PRESENCE_WIFI_ENTITY` | HA entity ID for the person's WiFi tracker (e.g. `network.phone.katie`). |

**Optional:**

| Variable | Default | Notes |
|---|---|---|
| `PRESENCE_TRUSTED_WIFI` | *(unset)* | Comma-separated list of trusted SSIDs for WiFi corroboration (e.g. `RubyGues,RubyNet,RIoT`). |
| `PRESENCE_DEBOUNCE_SECONDS` | `120` | Seconds to wait before publishing a state change after an uncertain phone state. |
| `PRESENCE_UNCERTAIN_STATES` | `unknown,unavailable,none` | Comma-separated phone states that trigger WiFi corroboration. |
| `VAULT_HA_PATH` | `secret/data/ruby-core/ha` | HA base URL and long-lived access token for WiFi corroboration REST calls. |
| `VAULT_ADDR` | `http://127.0.0.1:8200` | Vault server address |
| `VAULT_TOKEN` | *(required)* | Read-only token scoped to `secret/ruby-core/*` |
| `VAULT_NKEY_PATH` | `secret/data/ruby-core/nats/presence` | NATS NKEY seed |
| `VAULT_TLS_PATH` | `secret/data/ruby-core/tls/presence` | NATS mTLS cert, key, CA |
| `NATS_URL` | `tls://localhost:4222` | NATS server URL |
| `NATS_REQUIRE_MTLS` | `false` | Force mTLS even if NATS_URL is not `tls://` |
| `ENVIRONMENT` | *(unset)* | Set to `production` to enforce HTTPS Vault |
| `VAULT_ALLOW_HTTP` | `false` | Override HTTPS enforcement for co-located Vault |

## Health check

No HTTP endpoint. Liveness is inferred from NATS pull consumer activity in the logs.

## Known failure modes

**Missing required env vars** (`PRESENCE_PERSON_ID`, `PRESENCE_PHONE_ENTITY`, `PRESENCE_WIFI_ENTITY`) — exits 1 at boot with a descriptive error.

**HA config unavailable** — WiFi corroboration is disabled. Uncertain phone states (`unknown`, `unavailable`, `none`) are treated as not-home without consulting the WiFi entity. Phone state changes still publish to the `PRESENCE` stream; the fused state will be less reliable during this window. Logged at `WARN` level.

**NATS or Vault unreachable** — exits 1 immediately.
