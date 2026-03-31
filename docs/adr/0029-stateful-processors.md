# ADR-0029 — Extend Engine to Support Stateful Processors

* **Status:** Accepted
* **Date:** 2026-03-31

## Context

The engine is currently a pure rules processor: it consumes events from NATS, evaluates conditions via registered `Processor` implementations, and publishes commands. All processors are stateless — they use NATS KV buckets for lightweight state and have no external storage dependencies. The engine starts with two dependencies: Vault (for credentials) and NATS.

The Ada baby tracking feature requires a processor that writes to PostgreSQL: persisting feeding, diaper, sleep, and tummy time events, computing daily aggregates, and pushing sensor state to Home Assistant. The question is whether this logic should live in the engine or in a new standalone service.

### Why not a standalone service

A standalone `services/ada/` service was designed and then reconsidered for two reasons:

1. **Proliferation risk.** Every new domain feature would follow the same pattern — new service, new NKEY, new Dockerfile, new compose entry, new CI job. The system would accumulate micro-services distinguished primarily by their database schemas, not by principled service boundaries.

2. **The engine's responsibility is "something happened, now do something about it."** Whether the outcome is a push notification, a KV write, a Postgres insert, or a HA sensor update is an implementation detail of the processor, not a reason to break service boundaries. The engine already routes events to the right processor — that routing capability should not be duplicated in standalone services.

### Why this requires an explicit decision

The current `processor.Config` only carries `RuleCfg`, `NC`, and `JS`. Adding Postgres requires extending `Config` and the engine's boot sequence in a way that affects all processors and all future engine deployments. This is a deliberate evolution that warrants an explicit decision record.

### The optional dependency question

The engine should not fail to start if Postgres is unavailable when no stateful processors are registered. Conversely, if stateful processors are registered and Postgres is unavailable, the engine must fail fast (ADR-0015). This requires the engine to know at boot time whether any registered processor requires storage.

## Decision

The engine is extended to support two processor types:

**1. Stateless processors** (unchanged): implement `processor.Processor` as today. Receive `Config` with `RuleCfg`, `NC`, `JS`. No storage access. `presence_notify` remains stateless — zero changes required.

**2. Stateful processors** (new): implement `processor.StatefulProcessor`, which embeds `processor.Processor` and adds one marker method:

```go
// StatefulProcessor is a Processor that requires a PostgreSQL connection pool.
// Returning true from RequiresStorage signals the ProcessorHost to verify
// that Config.Pool is non-nil before calling Initialize.
type StatefulProcessor interface {
    Processor
    RequiresStorage() bool
}
```

**`processor.Config` gains two optional fields:**

```go
type Config struct {
    RuleCfg *config.CompiledConfig
    NC      *nats.Conn
    JS      nats.JetStreamContext
    Pool    *pgxpool.Pool   // nil when no stateful processors are registered
    HA      *boot.HAConfig  // fetched alongside Pool when any stateful processor is registered
}
```

**`ProcessorHost.Initialize` gains a storage check:**

Before calling `p.Initialize(cfg)`, the host checks whether `p` implements `StatefulProcessor` and `p.RequiresStorage()` returns true. If so, and `cfg.Pool` is nil, `Initialize` returns an error immediately. This makes the misconfiguration explicit and compiler-assisted.

**Engine boot sequence gains a conditional Postgres step:**

After NATS connects and before processors are initialized, the engine inspects registered processors for any that implement `StatefulProcessor` with `RequiresStorage() == true`. If found, it fetches Postgres credentials from Vault at `secret/data/ruby-core/postgres` using `boot.FetchPostgresConfig`, connects a `pgxpool.Pool`, and runs schema migrations via `pkg/store.MigrateUp`. Failure at any of these steps is fatal (ADR-0015). If no stateful processors are registered, the Postgres boot steps are skipped entirely — the engine's behavior for stateless-only deployments is unchanged.

## Alternatives Considered

* **Standalone `services/ada/` service:** Rejected. Adds service proliferation without a principled boundary. Every domain feature with storage needs would become its own service, accumulating duplicate boot patterns, NKEYs, Dockerfiles, and CI jobs. The engine's event-routing capability should not be duplicated.

* **Shared sidecar Postgres service alongside the engine:** Rejected. Adds operational complexity (two containers to manage, health check coupling) without architectural benefit. The engine is already the single event-driven runtime.

* **Embed Postgres credentials directly in engine env vars (no Vault):** Rejected. Violates ADR-0015 (all secrets in Vault). Inconsistent with existing NATS and HA credential patterns.

## Consequences

### Positive

* **No service proliferation.** New domain features with storage requirements become processors, not services.
* **Single failure domain.** All event-driven logic is colocated. Operational reasoning is simpler.
* **Postgres is a conditional engine dependency.** Stateless-only deployments are unaffected.
* **Existing processors unchanged.** `presence_notify` requires no modification.
* **Explicit interface contract.** `StatefulProcessor` makes storage requirements compiler-verified rather than documented convention.

### Negative

* **Postgres outage affects all engine processors.** If Postgres is down and a stateful processor is registered, the engine fails to start, taking down stateless processors too. This is the correct behavior per ADR-0015 but the blast radius is wider than a standalone service.
* **Engine boot sequence is more complex.** The conditional Postgres step adds branching logic to startup.

### Neutral

* `Config.Pool` is nil for stateless processors. A processor that accesses a nil pool panics — this surfaces misconfiguration immediately rather than silently.
* Adding fields to a Go struct is backward-compatible. Existing code constructing `processor.Config` with named fields compiles cleanly; `Pool` and `HA` default to nil.

## Implementation Notes

* Pool type: `*pgxpool.Pool` from `github.com/jackc/pgx/v5/pgxpool`.
* Engine compose service adds `VAULT_PG_PATH=secret/data/ruby-core/postgres`.
* Schema migrations run via `adastore.MigrateUp(ctx, pool)` called from `main.go` before processor initialization. The migration files are embedded in the ada store package via `//go:embed migrations/*.sql` — they are not passed from `main.go`.
* The smoke test is extended with a Postgres round-trip check: publish a synthetic Ada diaper event, verify the row appears in the `diapers` table within 10 seconds.
* `gateway.health` is published via bare `nc.Publish()` and is not on any JetStream stream. Stateful processors that need HA reconnection detection must set up a bare `nc.Subscribe("gateway.health", ...)` in `Initialize` rather than listing the subject in `Subscriptions()`, which only routes JetStream pull consumer messages.
* All volume amounts are stored and pushed in oz (the native unit of entry). No ml conversion is performed by the processor.

## First Implementation

The first stateful processor is `services/engine/processors/ada` — the Ada baby tracking processor. It implements `StatefulProcessor` with `RequiresStorage() bool { return true }`, subscribes to `ha.events.ada.>`, and uses `Config.Pool` for all database operations via sqlc-generated query functions in `services/engine/processors/ada/store/`.
