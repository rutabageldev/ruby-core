# ADR-0033 - Ada projection integrity: single-writer live ingest and permanent growth retention

* **Status:** Accepted
* **Date:** 2026-06-18
* **Supersedes:** *(none)*
* **Superseded by:** *(none)*

---

## Context

Three ruby-core stacks (prod, staging, dev) run concurrently on one node and each gateway
subscribed to the **same** Home Assistant instance's WebSocket. A single dashboard tap was
therefore ingested by all three stacks, each minting a distinct CloudEvent id
(`services/gateway/ada/publish.go`), written to (originally) a shared Postgres database, and
projected back by three engines that all `PushState` to the **same** HA sensors. The dashboard
surfaced whichever stack wrote last (issue #63), producing 2–3× counts.

The same topology is the dominant cause of the reliability symptoms reported in the #80/§4
investigation: a staging or dev engine with a sparse growth table re-publishes
`sensor.ada_growth_history` with only its few rows, overwriting the prod engine's complete history
— which presents as growth points "rolling off" even though the `growth_measurements` table is
already permanent (no time window) and is already re-published on engine startup and HA reconnect
(`refreshAllSensors` → `pushGrowthSensors`). Combined with un-removable junk dated later than real
points, `latest_*` also went stale.

Separately, the growth-history projection dropped attribution: the entry structs in
`services/engine/processors/ada/growth.go` (`growthWeightEntry`, `growthLengthEntry`,
`growthHeadEntry`) had no `logged_by` field, so it was absent by the time records reached the sensor.

This ADR documents the decisions that govern these concerns. The single-writer ingest decision was
**implemented before this ADR was written** (PR #72) and had no ADR of its own; this records it.

## Alternatives Considered

**Per-env dedicated HA instances** — staging/dev each point at their own Home Assistant (the
homeassistant repo scaffolds a `sandbox/config/`). Cleanest long-term env separation, but requires
standing up and wiring a separate HA container in another repo; larger and outside this effort's
control.

**Shared single NATS with consumer-group semantics** — point all stacks at one prod-owned NATS so
exactly one engine handles each event. Rejected: contradicts the "three independent environments"
intent of the current topology and is the most complex option.

**Gate only the `ada_event` subscription** — leave `state_changed` ingestion enabled in non-prod
while disabling only Ada. Rejected: a non-prod gateway still consuming `state_changed` would keep
pushing other derived state to the shared prod HA, and the broader gate already solves it more
simply.

**Gate the whole HA WebSocket per environment (chosen, already implemented)** — a single
environment flag decides whether a gateway connects to Home Assistant at all.

## Decision

1. The gateway's Home Assistant ingestion MUST be gated by `HA_INGEST_ENABLED` (PR #72). When
   `false`, the gateway skips the Vault HA fetch and runs in degraded mode — no WebSocket, so
   neither `state_changed` nor `ada_event` is consumed and no derived state is pushed — while the
   HTTP/health endpoints stay up. It MUST be `false` in dev and staging compose and unset/true in
   prod, so exactly one stack ingests live HA events.
2. The **engine's** Ada HA push MUST also be gated by `HA_INGEST_ENABLED` — `false` in dev/staging,
   `true` in prod. When `false` the engine skips the Vault HA fetch and runs with an empty HA client
   whose pushes are no-ops, so a non-prod engine never projects its database to the shared HA on
   startup, HA-reconnect, or the 4-hour safety net (the gateway gate in #72 stopped non-prod
   *ingest* but not non-prod *projection* — this closes that gap).
3. Each environment MUST use its own Postgres database (dev isolated per the change accompanying
   #72), so non-prod engines never write to or read from the prod Ada store.
4. Growth measurements MUST be retained permanently (no rolling window); `sensor.ada_latest_*`,
   `sensor.ada_growth_curves`, and `sensor.ada_growth_history` MUST be derived from the complete
   set of non-deleted (`deleted_at IS NULL`) measurements.
5. The growth-history projection MUST preserve `logged_by` per entry.
6. Non-prod stacks (staging, dev) MUST be exercised via seeded data only (ROADMAP-0010.6); they do
   not receive live HA ingest.

## Consequences

### Positive

* Eliminates 2–3× duplication and the sparse-overwrite of growth history; the dashboard reflects a
  single authoritative source.
* Restores trustworthy growth data: complete history, correct `latest_*`, and intact `logged_by`.
* No new infrastructure; the gate is a single environment variable already shipped.

### Negative

* Staging and dev observe no live HA activity at all (not just no Ada) and depend entirely on the
  seed target for data.

### Neutral

* Formalizes prod as the sole live-ingest stack until (and unless) per-env HA instances are
  introduced.
* Relates to ADR-0023 (single-writer enforcement): this extends the single-writer principle from
  the NATS/DB layer up to the live-ingest edge.
