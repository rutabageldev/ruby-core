# Ruby Core Architecture

Ruby Core is an event-driven control plane for home automation, built on NATS JetStream.
It runs on a single Debian utility node alongside a broader Docker ecosystem.

---

## Physical Deployment

All environments (dev and prod) run on one Debian utility node using Docker Compose. Several
infrastructure services on that node are **pre-existing and shared** across projects — they
are not managed in this repository.

| Scope | Managed by |
|-------|-----------|
| Gateway, Engine, NATS, nats-init | This repo (`deploy/`) |
| HashiCorp Vault | Pre-existing on node (`vault_default` Docker network) |
| Traefik | Pre-existing on node (planned integration — Phase 5) |

---

## Container Inventory

### nats-init (init container)

| | |
|-|-|
| Image | `hashicorp/vault:1.15` |
| Dev name | `ruby-core-dev-nats-init` |
| Prod name | `ruby-core-prod-nats-init` |
| Restart | `no` — runs once then exits 0 |

**What it does:** Runs `scripts/fetch-nats-certs.sh` at stack startup before NATS starts.
Connects to Vault, fetches the NATS server TLS certificate/key/CA and each service's NKEY
public key, then writes them to the `nats-certs` shared volume:

```
/certs/server-cert.pem
/certs/server-key.pem
/certs/ca.pem
/certs/auth.conf          ← generated from Vault NKEY public keys
```

NATS depends on `service_completed_successfully`, so it cannot start until this succeeds.

**Why an init container for auth.conf:** Most editors and the Claude Code Edit tool write
files atomically (write to temp file → rename), creating a new inode each time. Docker
file bind mounts pin the original inode at container-start time, so SIGHUP reloads would
silently re-read stale content. Generating `auth.conf` inside the init container and
placing it on a named volume avoids this entirely.

---

### NATS

| | |
|-|-|
| Image | `nats:2.10-alpine` |
| Dev name | `ruby-core-dev-nats` |
| Prod name | `ruby-core-prod-nats` |
| Dev ports | `127.0.0.1:4222` (client TLS), `127.0.0.1:8222` (HTTP monitoring) |
| Prod ports | `127.0.0.1:4223` (client TLS), `127.0.0.1:8223` (HTTP monitoring) |

**What it does:** NATS JetStream message broker. All service-to-service communication goes
through NATS subjects.

**Security:** mTLS required on all client connections (ADR-0018). Services authenticate
with NKEY pairs; seeds are stored in Vault, public keys are embedded in `auth.conf` at
container startup (ADR-0017). Default-deny ACLs — each service has an explicit allow-list.

**Persistence:** JetStream data is stored on a named volume (dev) or host bind mount at
`/var/lib/ruby-core/nats` (prod, included in automated backups per ADR-0021).

**Config files** (from `deploy/base/nats/`):

- `nats.conf` — server config; includes `./certs/auth.conf` from the nats-certs volume
- `validate-config.sh` — pre-deploy validator; checks Vault NKEY presence + nats.conf

---

### Gateway

| | |
|-|-|
| Source | `services/gateway/` |
| Dev name | `ruby-core-dev-gateway` |
| Prod name | `ruby-core-prod-gateway` |
| Compose profile | `services` (not started by default with `make dev-up`) |

**What it does (planned):** Ingest Home Assistant state-change events over WebSocket and
publish them to `ha.events.>`. Accept actuation commands from the engine over
`ruby_engine.commands.>` and call the HA REST API. Phase 2 delivers a skeleton that
connects to NATS; full WebSocket/actuation logic is Phase 5.

**Startup sequence:** Fetches its NKEY seed from `secret/ruby-core/nats/gateway` and
TLS client cert from `secret/ruby-core/tls/gateway` in Vault, then connects to NATS with
mTLS + NKEY auth.

**NATS ACL (publish):** `ha.events.>`, `ruby_gateway.audit.>`, `ruby_gateway.metrics.>`
**NATS ACL (subscribe):** `ruby_engine.commands.>`

---

### Engine

| | |
|-|-|
| Source | `services/engine/` |
| Dev name | `ruby-core-dev-engine` |
| Prod name | `ruby-core-prod-engine` |
| Compose profile | `services` |

**What it does:** Consumes `ha.events.>` from NATS, deduplicates events, processes them
through the rule engine (Phase 5 TODO), and publishes commands. Phase 3 delivers the full
reliability infrastructure:

- **Pull consumer** with a 20-worker pool and `MaxAckPending=128` backpressure (ADR-0024)
- **Idempotency** via a hybrid store: in-memory TTL cache (fast path) + NATS KV bucket
  `idempotency` (durable path, survives restarts) (ADR-0025)
- **DLQ forwarder**: subscribes to the NATS max-delivery advisory; after 5 failed attempts
  (backoff: 1s → 2s → 4s → 8s), routes the original message to `dlq.ha_events.engine_processor`
  (ADR-0022)

**NATS ACL (publish):** `ruby_engine.commands.>`, `ruby_engine.audit.>`,
`ruby_engine.metrics.>`, `$JS.API.>`, `$JS.ACK.>`, `$KV.idempotency.>`, `dlq.>`
**NATS ACL (subscribe):** `ha.events.>`, `_INBOX.>`,
`$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.HA_EVENTS.engine_processor`

---

### Vault (external)

| | |
|-|-|
| Image | `hashicorp/vault` (pre-existing on node) |
| Network | `vault_default` |

Not managed by this repo. Ruby Core connects to Vault at startup to fetch secrets and at
stack startup (nats-init) to generate `auth.conf`. Services use a static `VAULT_TOKEN`
passed via environment variable (ADR-0015 notes this should move to AppRole in a future
phase).

**Secret paths used by this project:**

| Path | Contents |
|------|----------|
| `secret/ruby-core/tls/nats-server` | NATS server TLS cert/key/CA |
| `secret/ruby-core/tls/<service>` | Client TLS cert/key/CA per service |
| `secret/ruby-core/nats/<service>` | NKEY seed + public_key per service |

---

### Traefik (external, planned)

Pre-existing general-purpose reverse proxy on the node. Not yet wired to ruby-core.
Phase 5 will configure Traefik as the edge authentication layer for any HTTP endpoints
exposed by the gateway (JWT validation middleware + mTLS between Traefik and gateway).
See ADR-0020.

---

## Networks

| Network | Type | Purpose |
|---------|------|---------|
| `default` (dev/prod) | Internal | Service-to-service + NATS client connections |
| `vault_default` | External | Connects all containers that need Vault access |

NATS ports are bound to `127.0.0.1` only — not reachable from external hosts without an
explicit tunnel or Traefik rule.

---

## Volumes

| Volume | Env | Purpose |
|--------|-----|---------|
| `nats-certs` | Dev + Prod | TLS certs + `auth.conf` written by nats-init |
| `ruby-core-dev-nats-data` | Dev | JetStream persistent storage |
| `/var/lib/ruby-core/nats` | Prod | JetStream persistent storage (host bind mount, backup target) |

---

## NATS Streams (Phase 3+)

| Stream | Subjects | Retention | Purpose |
|--------|----------|-----------|---------|
| `HA_EVENTS` | `ha.events.>` | Limits (no MaxAge) | Raw Home Assistant events |
| `DLQ` | `dlq.>` | 7-day MaxAge | Dead-lettered messages after MaxDeliver exhaustion |

KV bucket `idempotency` (TTL: 24h) stores processed event IDs for deduplication.

---

## Message Flow (current)

```
Home Assistant
    │  WebSocket / REST
    ▼
[gateway]  →  ha.events.<area>.<entity>  →  [HA_EVENTS stream]
                                                    │
                                             [engine] pull consumer
                                                    │
                                        idempotency check (mem → KV)
                                                    │
                                        processEvent() [Phase 5 TODO]
                                                    │
                                          Ack / NakWithDelay
                                                    │
                              ┌─────────────────────┘
                              │ after MaxDeliver (5 attempts)
                              ▼
                    NATS max-delivery advisory
                              │
                    [DLQForwarder] → dlq.ha_events.engine_processor → [DLQ stream]
```

---

## Startup Sequence

```
docker compose up
  1. nats-init starts
       → waits for Vault
       → fetches TLS certs → /certs/
       → fetches NKEY public keys → generates /certs/auth.conf
       → exits 0
  2. nats starts (depends on nats-init: service_completed_successfully)
       → reads nats.conf (includes certs/auth.conf)
       → JetStream ready
  3. gateway / engine start (depend on nats: healthy)
       → each fetches NKEY seed + TLS client cert from Vault
       → connect to NATS with mTLS + NKEY
       → engine: EnsureHAEventsStream, EnsureDLQStream, CreateOrBindKVBucket
       → engine: pull consumer + DLQ forwarder start
```

---

## Dev vs. Prod Differences

| Aspect | Dev | Prod |
|--------|-----|------|
| Service images | Built from source (`build:`) | GHCR images (`ghcr.io/.../...`) |
| JetStream storage | Named Docker volume | Host bind mount (`/var/lib/ruby-core/nats`) |
| NATS client port | 4222 | 4223 |
| NATS monitor port | 8222 | 8223 |
| Container hardening | None | `read_only`, `no-new-privileges`, `cap_drop: ALL` |
| `VAULT_TOKEN` | `${VAULT_TOKEN:-root}` (default `root`) | Required env var (no default) |

---

## Planned Services (not yet implemented)

| Service | Phase | Purpose |
|---------|-------|---------|
| Notifier | 5+ | Send notifications from engine commands |
| Presence | 5+ | Track device presence via UniFi events |
| Adapters | 5+ | UniFi, Zigbee2MQTT integration |
| Audit sink | 4 | Persist audit events from `audit.events` stream |
| OTel Collector / Jaeger / Prometheus / Loki | 7 | Full observability stack |
