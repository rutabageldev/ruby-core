# Persist & compute Ada Medications and Emergency server-side

* **Status:** In Progress
* **Date:** 2026-06-20
* **Project:** ruby-core
* **Related ADRs:** ADR-0037 (persistence + contract), ADR-0038 (server-owned safety computations)
* **Linked Plan:** docs/plans/PLAN-0028-ada-medications-emergency.md

---

**Goal:** Make ruby-core the source of truth for Ada's Medication Management and Emergency Card — persisting the events the dashboard already fires, projecting the sensors it will read, and owning the safety computations a closed app cannot — so the dashboard can drop its client-local store and the dosing guard keeps working when no one has the app open.

---

## Efforts

Each effort is one branch/PR (single-concern). The dashboard surfaces shipped frontend-first (HA PR #103) on a client-local store; this roadmap is the backend half. The dashboard's read-seam repoint (local state → `attr('sensor.ada_…')`) is HA-side work, out of scope here.

### 0011.1 — Schema + registry/routines CRUD (`feat/ada-meds-schema-registry`)

Migration `000008` for all five tables (medications, medication_routines, medication_events, medication_temp_series, emergency_rows); gateway `eventRoutes` + schema constants/structs + processor handlers for `medication.upsert/delete` and `medication.routine.upsert/delete`; project `sensor.ada_medications` and `sensor.ada_med_routines`; wire the new tables into the snapshot/clear/birth tooling. Out of scope: dose events, computations, emergency.

### 0011.2 — Med events + history + edit/delete (`feat/ada-meds-events-history`)

`medication.given/skipped`, `series.start/end`, `event.update/delete`; dose snapshot on every `given`; project `sensor.ada_med_events`; full edit/delete parity with the existing tracking domains. Out of scope: derived/computed state.

### 0011.3 — Server-owned computations (`feat/ada-meds-computations`)

`next_due`/`earliest_safe`/`doses_in_24h` projection; system-emitted `missed` (supersession, never stacks); routine auto-complete (no phantom dose); temporary-series ~24h auto-expire; "Medication due" reminders via the existing caretaker push; safety-math parity tests against the client formulas.

### 0011.4 — Emergency (`feat/ada-emergency`)

`emergency.row.upsert/delete` + `emergency.reorder`; project `sensor.ada_emergency_card` (rows + order only — live fields resolve client-side); seed coverage for the new domains; docs + runbook.

---

## Done When

All thirteen `ada.medication.*` / `ada.emergency.*` events the dashboard fires are persisted by the engine and reflected in the four sensors (`sensor.ada_medications`, `sensor.ada_med_routines`, `sensor.ada_med_events`, `sensor.ada_emergency_card`); the engine independently computes the dosing guard (`earliest_safe`, `doses_in_24h`), emits `missed` and series-expiry without the app open, and reminds caretakers when a dose is due — all matching the client formulas in `adaMeds.ts`; and the new domains are covered by the snapshot/seed/clear/birth tooling.

---

## Acceptance Criteria

* [ ] Publishing each of the 13 events to NATS results in the expected row state in `ruby_core` Postgres (insert/upsert for writes; `deleted_at` set for deletes; `medication.delete` cascades to its routines + active series).
* [ ] `go test -tags=fast ./services/engine/...` includes parity tests proving server math equals `adaMeds.ts`: `earliest_safe` boundary (safe exactly at `lastGiven + min_interval`), `doses_in_24h` over `(now−24h, now]`, supersession emits exactly one `missed` (never stacks), auto-complete writes zero events.
* [ ] With no app open, a fixed-slot dose left unacted past a later due slot produces exactly one system `missed` row; an active temporary series past its ~24h backstop becomes `status='expired'`; a due dose triggers a caretaker `ha_push` (prod only).
* [ ] `make ada-db-snapshot ENV=prod` includes the five new tables; `make ada-db-seed ENV=dev` populates them; `make ada-db-clear-test ENV=dev CONFIRM=yes` removes all `test=true` rows across them (post-clear count = 0); the birth-watcher's `validate_nuke` counts `medication_events`.
* [ ] Pre-birth medication dose events are `test=true` (wiped at birth); registry/routines/emergency entered with `test=false` survive the birth clean-slate.
