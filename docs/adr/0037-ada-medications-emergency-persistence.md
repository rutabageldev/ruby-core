# ADR-0037 - Ada Medications & Emergency: persistence and event/sensor contract

* **Status:** Accepted
* **Date:** 2026-06-20
* **Supersedes:** *(none)*
* **Superseded by:** *(none)*

---

## Context

The Ada dashboard shipped two new surfaces frontend-first (HA PR #103): a Medication Management suite (a medication registry, dosing routines, a dose timeline with an always-on safety guard) and an Emergency Card. Both run today on a **client-local store with no persistence** — every action already fires an `ada.medication.*` / `ada.emergency.*` event over the existing `script.fire_ada_event` → `ada_event` → gateway → `HA_EVENTS` path, and every read sits behind a thin seam still bound to local state.

This is the same split every other Ada domain already uses: the dashboard writes only by firing events and reads only from sensors ruby-core projects; **ruby-core owns truth** (ingest `ha.events.ada.*`, persist to Postgres, project derived sensors). Feeds, diapers, sleep, tummy, growth, the feed-claim, and Trends all work this way (ADR-0029 stateful processors, ADR-0031 test-data, ADR-0032 trends, ADR-0033 single-stack projection). The backend half for Medications and Emergency does not yet exist; without it the data lives only in the browser and the dosing guard dies the moment the app is closed.

A subtlety unique to these domains: some of the data is **standing config** (the medication registry, dosing routines, emergency contacts) that a parent may legitimately enter *before birth* and expect to survive the birth clean-slate (ADR-0035/0036), exactly like caretakers; other data is **practice tracking** (fake dose events logged while testing) that must be wiped at birth like every other pre-birth event.

## Alternatives Considered

**A new dedicated processor/service for medications** — More moving parts (another stateful service, image, NKEY, consumer) for data that rides the identical `ha.events.ada.>` path and shares the same Postgres, projection gate, and lifecycle as the existing Ada domains. Rejected — it would fork conventions for no benefit.

**Treat all new data as tracking (pre-birth `test`-forced, wiped at birth)** — Simplest, but erases a pediatrician/poison-control contact or a vitamin-D routine entered before birth, forcing re-entry at the worst moment. Rejected — contradicts the "config survives birth" guarantee caretakers already enjoy.

**Store the routine `end` rule as flat typed columns per end-type** — The dashboard sends `end: { type, value? }` where `value` is a number (max_doses) or a date string (end_date) or absent (none). Flat per-type columns multiply nullable columns and drift from the wire shape. Rejected in favor of `end_type TEXT` + `end_value TEXT` (value stringified, re-parsed by type), which mirrors the payload losslessly.

## Decision

1. Medications and Emergency MUST be served by the **existing `ada` processor**, not a new service. New event types are added to the gateway allowlist (`services/gateway/ada/publish.go` `eventRoutes`), to `pkg/schemas/ada.go` as `ha.events.ada.*` type constants + payload structs, and to the `ProcessEvent` switch — the same four-leg path every Ada event already traverses. Unknown events remain rejected at the gateway.

2. Persistence MUST follow the established store conventions: one sqlc-managed migration (`000008`) creating `medications`, `medication_routines`, `medication_events`, `medication_temp_series`, `emergency_rows`; every table carries `logged_by`, `deleted_at` (soft-delete), `created_at`, and `test BOOLEAN` (ADR-0031) with a `WHERE test=true` partial index. Edits are full-resolution replacements; deletes set `deleted_at`; reads filter `deleted_at IS NULL`. `medication.delete` MUST cascade as an **app-level soft-delete** within one transaction (the medication and its routines and any active series). `medication_id` (and `series_id` / `anchor_dose_id`) are **loose refs, not foreign keys** (migration `000010`): the dashboard generates all ids client-side and fires entities as independent events that the worker pool processes concurrently, so a FK would reject a child whose parent has not yet been inserted. Referential integrity is the dashboard's responsibility; the app-level cascade above does not rely on a FK.

3. The four sensors MUST be `sensor.ada_medications`, `sensor.ada_med_routines`, `sensor.ada_med_events`, `sensor.ada_emergency_card`, projected via `PushState` and gated by `HA_INGEST_ENABLED` (prod only, ADR-0033). Their attribute payloads MUST mirror the `adaMeds.ts` / `adaEmergency.ts` field names (`{items:[…]}` / `{rows:[…]}`) so the future dashboard repoint is a pure binding swap, not a reshape. Emergency persists **rows + order only** — live fields resolve client-side off existing growth/age sensors; ruby-core adds no value service.

4. **Birth-wipe classification.** Medication **dose events** (`medication_events`: given/skipped/missed) are tracking — written with `eventTestOrPreBirth(evt)` so all pre-birth doses are `test=true` and wiped on the first `ada.born` (ADR-0035/0036). The medication **registry + routines** and **emergency rows** are standing config — written with `eventTest(evt)` only (no pre-birth forcing), so real (`test=false`) entries survive the clean-slate like caretakers. All five tables still participate in the snapshot/seed/clear tooling; `medication_events` MUST be added to the birth-watcher's `validate_nuke` count.

5. "Medication due" reminders MUST reuse the engine's existing caretaker push — `GetActivePeopleWithChannels` → `p.ha.Notify` to each active `ha_push` channel, the same mechanism `dispatchFeedingAlert` uses — **not** the COMMANDS/notifier service (reserved for engine rule commands). This keeps one reminder mechanism for all Ada surfaces, already `HA_INGEST_ENABLED`-gated.

## Consequences

### Positive

* The two surfaces gain durable truth and survive app closure with zero new services; the dashboard repoint becomes a one-line binding swap per surface.
* Pre-birth setup (real meds, routines, emergency contacts) survives birth; practice doses do not — matching parent expectations and the caretaker precedent.
* Snapshot/seed/clear/birth tooling extends to the new tables with no new mechanisms.

### Negative

* `pkg/schemas/ada.go`, the gateway allowlist, and the `ProcessEvent` switch keep growing; the `ada` processor accretes more surface area in one place.
* The `test`-flag split (events forced pre-birth, config not) is a per-handler discipline that must be applied correctly and is easy to get wrong silently — covered by the birth ACs.

### Neutral

* Formalizes the `ada` processor as the single home for all Ada-domain persistence and projection.
* The medication safety computations themselves (the authority for `missed`, due, expiry) are a distinct decision — see ADR-0038.
