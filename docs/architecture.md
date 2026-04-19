# Ruby Core Architecture

Ruby Core is an event-driven control plane for home automation, built on NATS JetStream. All environments run on a single Debian utility node using Docker Compose. Several infrastructure services on that node are pre-existing and shared across projects — they are not managed in this repository.

| Scope | Managed by |
|---|---|
| Gateway, Engine, Notifier, Presence, Audit-sink, NATS, nats-init | This repo (`deploy/`) |
| HashiCorp Vault | Foundation repo (`vault-ruby-core-prod` Docker network) |
| Traefik | Foundation repo (`traefik_proxy` / `traefik-public` Docker networks) |
| PostgreSQL | Foundation repo (`postgres` Docker network) |
| Observability stack (OTel Collector, Prometheus, Loki, Tempo, Grafana) | Foundation repo (per [ADR-0028](adr/0028-observability-stack-placement.md)) |

---

## Container Inventory

### nats-init (init container)

| | |
|---|---|
| Image | `hashicorp/vault:1.15` |
| Prod name | `ruby-core-prod-nats-init` |
| Restart | `no` — runs once, exits 0 |

Runs `scripts/fetch-nats-certs.sh` before NATS starts. Connects to Vault, fetches the NATS server TLS certificate/key/CA and each service's NKEY public key, generates `auth.conf` from those keys, and writes everything to the `nats-certs` shared volume. NATS depends on `service_completed_successfully` — it cannot start until this exits 0.

Auth config is generated inside the container rather than bind-mounted because editors and tooling write files atomically (temp file → rename), which creates a new inode that Docker bind mounts cannot follow. The named volume approach avoids stale content on SIGHUP reloads.

---

### NATS

| | |
|---|---|
| Image | `nats:2.10-alpine` |
| Prod name | `ruby-core-prod-nats` |
| Prod ports | `127.0.0.1:4223` (TLS client), `127.0.0.1:8223` (HTTP monitor) |
| Staging ports | `127.0.0.1:4224` (TLS client), `127.0.0.1:8224` (HTTP monitor) |

All service-to-service communication goes through NATS subjects. JetStream provides durable, at-least-once delivery for critical event streams ([ADR-0001](adr/0001-nats-strategy.md)).

**Auth:** NKEY per-service keypair. Seeds stored in Vault; public keys embedded in `auth.conf` at container startup ([ADR-0017](adr/0017-nats-auth.md)). Default-deny ACLs — each service has an explicit allow-list enforcing single-writer ownership ([ADR-0023](adr/0023-single-writer-enforcement.md)).

**Transport:** mTLS required on all client connections ([ADR-0018](adr/0018-transport-security.md)). TLS 1.3 minimum. Client certificates required (`verify: true`).

**Persistence:** JetStream data stored on a host bind mount (`/var/lib/ruby-core/nats`) in prod — included in automated backups ([ADR-0021](adr/0021-nats-ha-strategy.md)).

---

### Gateway

| | |
|---|---|
| Source | `services/gateway/` |
| Prod name | `ruby-core-prod-gateway` |
| HTTP | `:8080` (internal; routed via Traefik) |

Bridges Home Assistant and the NATS event bus in both directions.

**Ingress:** Subscribes to HA state changes over WebSocket. Normalizes events into CloudEvents ([ADR-0003](adr/0003-cloudevents-contract.md)), applies a lean projection (strips all attributes not in the passlist derived from engine rules — [ADR-0009](adr/0009-gateway-responsibilities.md)), and publishes to `ha.events.>` on the `HA_EVENTS` JetStream stream.

**Egress:** Subscribes to `ruby_engine.commands.>` and calls the HA REST API to actuate devices.

**Reconciliation:** Publishes a `gateway.health` heartbeat every 15 seconds (bare NATS publish, not JetStream). On HA reconnect, fetches current state of critical entities from the HA REST API and re-publishes any that have drifted ([ADR-0008](adr/0008-gateway-health-and-reconciliation.md)).

**Edge auth:** Traefik validates JWTs before forwarding requests to `:8080`. The gateway assumes pre-authenticated requests ([ADR-0020](adr/0020-gateway-api-auth.md)).

**NATS publish:** `ha.events.>`, `audit.ruby_gateway.>`
**NATS subscribe:** `ruby_engine.commands.>`, `config` KV (passlist + critical entities)
**KV write:** `gateway_state` bucket (last-seen timestamp per entity, for reconciliation)

---

### Engine

| | |
|---|---|
| Source | `services/engine/` |
| Prod name | `ruby-core-prod-engine` |

Hosts multiple independent processors against the NATS event bus. Processors share a common interface ([ADR-0007](adr/0007-engine-decomposition.md)) and may be stateless or stateful ([ADR-0029](adr/0029-stateful-processors.md)).

**Consumers:**

- `engine_processor` — pull consumer on `HA_EVENTS` stream, subject `ha.events.>` (batch 20, worker pool, `MaxAckPending: 128`, 5 retries with exponential backoff, DLQ routing on exhaustion — [ADR-0024](adr/0024-backpressure-flow-control.md), [ADR-0022](adr/0022-poison-message-dlq-strategy.md))
- `engine_presence_processor` — pull consumer on `PRESENCE` stream, subject `ruby_presence.events.>`

**Idempotency:** Two-layer check — in-memory TTL cache (fast path) + `idempotency` KV bucket (durable, 24h TTL). Both written on successful processing ([ADR-0025](adr/0025-idempotency-tracking-store.md)).

**NATS publish:** `ruby_engine.commands.>`, `audit.ruby_engine.>`
**KV write:** `config` bucket (passlist, critical entities), `presence` bucket (sensor state)

#### Processor: presence_notify (stateless)

Subscribes to: `ha.events.>`, `ruby_presence.events.>`

Translates presence events into HA sensor state, written to the `presence` KV bucket.

#### Processor: ada (stateful — PostgreSQL)

Subscribes to: `ha.events.ada.>`, `ha.events.input_number.ada_alert_threshold_h`

Baby tracking processor. Persists feeding, diaper, sleep, and tummy time events to PostgreSQL (via sqlc-generated queries). After each event, pushes derived sensor state to Home Assistant over the HA REST API. Full sensor list:

| Category | Sensors |
|---|---|
| Feeding | `ada_last_feeding_time`, `ada_last_feeding_source`, `ada_next_feeding_target`, `ada_today_feeding_count`, `ada_today_feeding_oz` |
| Diaper | `ada_last_diaper_time`, `ada_last_diaper_type`, `ada_today_diaper_count`, `ada_today_diaper_wet/dirty/mixed` |
| Sleep | `ada_sleep_state`, `ada_last_sleep_change`, `ada_today_sleep_hours`, `ada_today_sleep_night_hours`, `ada_today_sleep_nap_hours`, `ada_today_sleep_nap_count`, `ada_today_sleep_night_count`, `ada_sleep_session_min` |
| Tummy time | `ada_today_tummy_time_min`, `ada_today_tummy_time_sessions`, `ada_tummy_time_target_min` |
| History (24h window) | `ada_feeding_history`, `ada_diaper_history`, `ada_sleep_history` |
| Boundary | `ada_today_boundary` (state = RFC3339 UTC; attributes include `bedtime_hhmm`, `daytime_hhmm`, `grace_min`, `boundary_local`) |

**Daily rollover** is driven by a configurable bedtime boundary (`bedtime_hhmm`, default `19:00` ET). A background ticker fires at bedtime each day and triggers a full aggregate refresh. Today aggregates are anchored to this boundary; history sensors use a fixed 24-hour sliding window. Sleep sessions are auto-categorized as `night` or `nap` based on the bedtime/daytime window with a configurable grace period.

Config keys (`ada_config` KV namespace via Postgres): `feed_interval_hours`, `next_feeding_target`, `bedtime_hhmm`, `daytime_hhmm`, `bedtime_grace_min`, `tummy_time_target_min`.

Postgres migrations are embedded in the processor package and run at engine startup.

---

### Notifier

| | |
|---|---|
| Source | `services/notifier/` |
| Prod name | `ruby-core-prod-notifier` |

Pull consumer on the `COMMANDS` stream (`ruby_engine.commands.notify.>`). For each command, calls the HA service API to deliver a push notification. Publishes `audit.ruby_notifier.notification_sent` on success (used as the smoke test oracle in CI).

**NATS subscribe:** `ruby_engine.commands.notify.>` (COMMANDS stream)
**NATS publish:** `audit.ruby_notifier.>`

---

### Presence

| | |
|---|---|
| Source | `services/presence/` |
| Prod name | `ruby-core-prod-presence` |

Multi-source presence fusion with debounce. Subscribes to HA phone entity state changes. On each change, corroborates against WiFi entity state via HA REST to reduce false transitions. Applies a configurable debounce window before publishing the fused result.

**NATS subscribe:** `ha.events.{phone_entity}` (HA_EVENTS stream, filtered)
**NATS publish:** `ruby_presence.events.state.{person_id}` (PRESENCE stream)

Configuration is environment-variable driven: `PRESENCE_PERSON_ID`, `PRESENCE_PHONE_ENTITY`, `PRESENCE_WIFI_ENTITY`, `PRESENCE_TRUSTED_WIFI`, `PRESENCE_DEBOUNCE_SECONDS`.

---

### Audit-sink

| | |
|---|---|
| Source | `services/audit-sink/` |
| Prod name | `ruby-core-prod-audit-sink` |

Pull consumer on the `AUDIT_EVENTS` stream (`audit.>`). Appends each event as a line to `/data/audit/audit.ndjson` (host bind mount at `/var/lib/ruby-core/audit`, included in backups). Always ACKs — filesystem errors are logged but do not cause redelivery, since the 72-hour stream retention window is the recovery mechanism ([ADR-0019](adr/0019-security-audit-logging.md)).

**NATS subscribe:** `audit.>` (AUDIT_EVENTS stream)

---

## NATS Streams

| Stream | Subjects | Published by | Consumed by | Retention | Notes |
|---|---|---|---|---|---|
| `HA_EVENTS` | `ha.events.>` | Gateway | Engine, Presence | Storage limits | Raw HA state changes (lean-projected). High volume. |
| `COMMANDS` | `ruby_engine.commands.>` | Engine | Notifier | 1 hour | Stale commands not replayed. |
| `PRESENCE` | `ruby_presence.events.>` | Presence | Engine | 24 hours | Debounced, fused presence state. |
| `AUDIT_EVENTS` | `audit.>` | All services | Audit-sink | 72 hours | Security audit trail. Subject format: `audit.{source}.{type}` ([ADR-0027](adr/0027-subject-naming-convention.md)). |
| `DLQ` | `dlq.>` | NATS (on max-deliver) | Manual reprocessing | 7 days | Poison messages after 5 failed delivery attempts. Monitored for growth. |

---

## NATS KV Buckets

| Bucket | Single writer | Readers | Purpose | TTL |
|---|---|---|---|---|
| `idempotency` | Engine | Engine | Processed event IDs for deduplication | 24h per key |
| `config` | Engine | Gateway | Rule-derived passlist + critical entities for projection and reconciliation | Persistent |
| `presence` | Presence + Engine | Both | Fused presence state and derived sensor state | Persistent |
| `gateway_state` | Gateway | — | Last-seen CloudEvent timestamp per HA entity (reconciliation baseline) | Persistent |

Single-writer ownership enforced at the NATS ACL level per [ADR-0023](adr/0023-single-writer-enforcement.md).

---

## Subject Naming

Per [ADR-0027](adr/0027-subject-naming-convention.md): `{source}.{class}.{type}[.{id}][.{action}]` — lowercase, alphanumeric + underscores only.

Classes: `events`, `commands`, `audit`, `metrics`, `logs`.

Reserved: `dlq.<stream>.<consumer>` (DLQ routing), `$` prefix (NATS internals), `gateway.health` (bare publish, not JetStream).

---

## Networks

| Network | Type | Purpose |
|---|---|---|
| `ruby-core-prod` (default) | Internal bridge | Service-to-service + NATS client connections |
| `vault-ruby-core-prod` | External | Vault access (shared with Foundation) |
| `event-bus-prod` | External | Shared event bus (other projects may publish/subscribe to NATS) |
| `traefik_proxy` | External | Traefik → gateway internal routing |
| `traefik-public` | External | Traefik public overlay (gateway visibility for file-provider routing) |
| `postgres` | External | PostgreSQL access (engine; stateful processors only) |

NATS ports are bound to `127.0.0.1` only — not reachable externally without Traefik or an explicit tunnel.

---

## Volumes

| Volume / Mount | Environment | Purpose |
|---|---|---|
| `/var/lib/ruby-core/nats:/data/jetstream` | Prod | JetStream persistent storage (host bind mount, backed up) |
| `/var/lib/ruby-core/audit:/data/audit` | Prod | Audit NDJSON archive (host bind mount, backed up) |
| `nats-certs` (named) | All | TLS certs + `auth.conf` written by nats-init, read by NATS and all services |
| `/opt/foundation/vault/tls/vault-ca.crt` | All | Vault CA certificate from Foundation (read-only bind mount) |

---

## Secrets Management

All credentials fetched from Vault at service startup ([ADR-0015](adr/0015-secrets-config-management.md)). Services fail fast if Vault is unavailable. Current auth model uses static scoped tokens (`VAULT_TOKEN` in `.env`, never committed). `.env` files are gitignored.

| Secret path | Contents |
|---|---|
| `secret/ruby-core/tls/nats-server` | NATS server TLS cert/key/CA |
| `secret/ruby-core/tls/{service}` | Client TLS cert/key/CA per service |
| `secret/ruby-core/nats/{service}` | NKEY seed + public key per service |
| `secret/ruby-core/postgres` | PostgreSQL credentials (engine / ada) |
| `secret/ruby-core/ha` | Home Assistant URL + long-lived token |
| `secret/ruby-core/staging/*` | Staging-scoped equivalents of all the above |

---

## Message Flow

```
Home Assistant
    │  WebSocket (state changes)
    ▼
[gateway]
    │  lean projection (passlist filter)
    │  CloudEvent wrap
    ├─► ha.events.>  ──────────────────────► [HA_EVENTS stream]
    │                                               │
    │                              ┌────────────────┴───────────────────┐
    │                              │                                    │
    │                       [engine] pull consumer              [presence] pull consumer
    │                       engine_processor                    (filtered: phone entity)
    │                              │                                    │
    │                    idempotency check                      WiFi corroboration (HA REST)
    │                    (mem cache → KV)                       debounce window
    │                              │                                    │
    │                    ProcessEvent()                                 │
    │                    ┌─────────┴──────────┐              ruby_presence.events.>
    │               presence_notify          ada                       │
    │               (stateless)         (stateful)         [PRESENCE stream]
    │                    │               │                             │
    │               presence KV     PostgreSQL              [engine] pull consumer
    │               sensor state    persist event           engine_presence_processor
    │                              push HA sensors                     │
    │                                    │                       ProcessEvent()
    │                              ruby_engine.commands.>       presence_notify
    │                              [COMMANDS stream]
    │                                    │
    │                             [notifier] pull consumer
    │                             HA service call (push notification)
    │                                    │
    │                              audit.ruby_notifier.>
    │                              [AUDIT_EVENTS stream]
    │                                    │
    │                             [audit-sink]
    │                             NDJSON archive
    │
    │  REST (actuation)
    ◄──── ruby_engine.commands.> (gateway subscribes for HA calls)
```

**DLQ path:** After 5 failed delivery attempts (exponential backoff: 1s → 2s → 4s → 8s), NATS republishes the message to `dlq.{stream}.{consumer}` on the `DLQ` stream (7-day retention).

---

## Startup Sequence

```
docker compose up
  1. nats-init starts
       → connects to Vault (TLS)
       → fetches server TLS cert/key/CA → writes to nats-certs volume
       → fetches each service's NKEY public key
       → generates /certs/auth.conf
       → exits 0

  2. nats starts  (depends on: nats-init completed_successfully)
       → reads nats.conf (includes certs/auth.conf from volume)
       → JetStream enabled, store at /data/jetstream

  3. gateway, engine, notifier, presence, audit-sink start  (depend on: nats healthy)
       → each fetches NKEY seed + TLS client cert from Vault
       → connect to NATS with mTLS + NKEY auth

       engine additionally:
       → fetches Postgres credentials from Vault (if stateful processors registered)
       → runs embedded Postgres migrations (ada schema)
       → EnsureHAEventsStream, EnsureDLQStream, EnsureCommandsStream,
         EnsurePresenceStream, EnsureAuditStream
       → CreateOrBindKVBuckets (idempotency, config, presence, gateway_state)
       → starts pull consumers (engine_processor, engine_presence_processor)
       → initializes processors (presence_notify, ada)
       → ada: refreshAllSensors(), seedDefaultConfig(), startBoundaryTicker()
```

---

## Dev / Staging / Prod Differences

| Aspect | Dev | Staging | Prod |
|---|---|---|---|
| Images | Built from source (`build:`) | GHCR images (`ghcr.io/.../...`) | GHCR images |
| JetStream storage | Named Docker volume | Named volume (ephemeral, removed on `down -v`) | Host bind mount (`/var/lib/ruby-core/nats`) |
| Audit storage | Named Docker volume | Named volume (ephemeral) | Host bind mount (`/var/lib/ruby-core/audit`) |
| NATS client port | 4222 | 4224 | 4223 |
| NATS monitor port | 8222 | 8224 | 8223 |
| Container name prefix | `ruby-core-dev-*` | `ruby-core-staging-*` | `ruby-core-prod-*` |
| Container hardening | None | `read_only`, `no-new-privileges`, `cap_drop: ALL` | Same as staging |
| Traefik routing | None | None (not publicly routed) | Full (JWT auth middleware) |
| Vault prefix | `secret/ruby-core/` | `secret/ruby-core/staging/` | `secret/ruby-core/` |
| Purpose | Local development | Pre-release smoke test gate (CI-gated) | Live |

---

## Release & Deployment

Per [ADR-0016](adr/0016-release-promotion-policy.md): single monorepo version tag (`vX.Y.Z`). CI pipeline on tag push:

1. Lint + security scan
2. Unit tests (`-tags=fast`)
3. Integration tests (testcontainers NATS)
4. Docker build (all 5 services, matrix)
5. Push images to GHCR
6. Deploy to staging → run smoke test → gate
7. Create GitHub release (gated on staging passing)

Production deployment via `make deploy-prod VERSION=vX.Y.Z` — pulls images, starts stack, runs smoke test, auto-rolls back to previous version on failure.

Smoke test oracle: publish a synthetic command to `ruby_engine.commands.notify.{id}`, wait for `audit.ruby_notifier.notification_sent` containing the smoke ID within 30 seconds.
