# PLAN-0028 — Ada Medications & Emergency: backend persistence

* **Status:** Complete
* **Date:** 2026-06-20
* **Project:** ruby-core
* **Roadmap Item:** ROADMAP-0011 (to be drafted alongside — see “Deliverables created by this plan”)
* **Branch(es):** one per phase (single-concern) — `feat/ada-meds-schema-registry`, `feat/ada-meds-events-history`, `feat/ada-meds-computations`, `feat/ada-emergency`
* **Related ADRs:** ADR-0027 (subjects), ADR-0029 (stateful processors), ADR-0031 (test data), ADR-0032 (trends req/resp), ADR-0033 (single-stack projection), ADR-0035/0036 (birth clean-slate). **New:** ADR-0037, ADR-0038 (drafted as Step 0).
* **Final committed path:** `docs/plans/PLAN-0028-ada-medications-emergency.md`

---

## Context

Ada is split across two halves: the HA dashboard (Svelte `rh-ada` card) writes only by firing `ada_event` over one HA script and reads only from HA sensors ruby-core projects; **ruby-core owns truth** — it ingests `ha.events.ada.*`, persists to Postgres, and projects derived sensors back. Feeds, diapers, sleep, tummy, growth, the feed-claim, and Trends all already work this way.

PR #103 shipped two new dashboard surfaces **frontend-first**, running on a client-local store with **no persistence**: a **Medication Management** suite (registry + routines + a dosing timeline with an always-on safety guard) and an **Emergency Card**. Every action already fires the events in §3 of the spec; every read sits behind a thin seam. This plan is the backend half: persist the events, project the sensors, and own the safety computations a closed app can’t do. When it lands, the dashboard flips its read-seam from local state to `attr('sensor.ada_…')` — a tiny, isolated consumer change owned on the HA side (out of scope here).

The dashboard computes everything client-side for instant feedback; the server math must match the spec’s formulas **exactly** so the two never disagree (same local/remote mirror discipline as the feed-claim).

### Decisions confirmed before drafting (these become ADR-0037/0038)

1. **Birth-wipe classification.** Medication **dose events** (`given`/`skipped`/`missed`) are *tracking* — pre-birth-forced to `test=true` and wiped on the first `ada.born` (fake practice doses). The medication **registry + routines** and **emergency rows** are *standing config* — written with the event’s own `test` flag only (no pre-birth forcing), so anything entered for real before birth (`test=false`) survives the clean-slate, exactly like caretakers. (All new tables still participate in the seed/clear/snapshot tooling; the distinction is purely the `test`-flag assignment at write time.)
2. **“Medication due” reminder delivery.** Reuse the engine’s **existing caretaker push** — `dispatchFeedingAlert` → `p.ha.Notify(channel.Address, title, body)` to each active `ha_push` channel (`processor.go:1844`). **Correction to the planning question:** feeding alerts do *not* go through the COMMANDS/notifier service; they use this direct HA-notify path, which is `HA_INGEST_ENABLED`-gated like every other Ada push. Medication-due mirrors it. (The notifier/COMMANDS path stays reserved for engine *rule* commands; pulling meds through it would diverge from the established Ada reminder pattern.)
3. **Doc structure.** ROADMAP-0011 (capability + Done-When + ACs) plus this single phased PLAN-0028; each phase lands as its own branch/PR.

---

## Architecture grounding (verified, with file:line)

End-to-end path a new Ada event must traverse — **all four legs are required**, and leg 1 is the one an engine-only view misses:

| Leg | Where | What to add |
|---|---|---|
| 1. Gateway allowlist | `services/gateway/ada/publish.go:17` `eventRoutes` map | Map each dashboard `payload.event` string (e.g. `"ada.medication.given"`) → a new `schemas.AdaEvent…` constant. **Unknown events are rejected** (`publish.go:55-58`). |
| 2. Type constants + payloads | `pkg/schemas/ada.go:4-38` (consts), `:40-` (structs) | `AdaEventMedication… = "ha.events.ada.medication_…"` (dots→underscores in the type slot, per ADR-0027 + existing `config_tummy_target`); payload structs `Ada…Data` with `LoggedBy` + optional fields. |
| 3. Schema + queries | `store/migrations/000008_*.up.sql`, `store/queries/*.sql`, then `sqlc generate` from `store/` | New tables + queries; regenerate `*.sql.go`. |
| 4. Processor dispatch + projection | `processor.go:195-271` switch on `evt.Type`; new files `medications.go`, `emergency.go`; `p.ha.PushState(...)` | Handlers + sensor projection. |

Reused patterns (do **not** reinvent):

* **Dispatch** routes on `evt.Type` (CloudEvent type), not subject; subscription is `ha.events.ada.>` (`processor.go:120-129`). Decode via `remarshal(evt.Data, &d)` (`processor.go:1977`).
* **Test flag**: `eventTest(evt)` (`edit_delete.go:20`) and `p.eventTestOrPreBirth(evt)` (`edit_delete.go:29`, = `eventTest || !p.born.Load()`). **Events** use `eventTestOrPreBirth`; **registry/routines/emergency** use `eventTest`.
* **Soft-delete + edit**: `UPDATE … SET deleted_at = NOW() WHERE id=@id AND deleted_at IS NULL`; full-resolution update mirrors `handleDiaperUpdate` (`edit_delete.go:162`) / transactional `handleFeedingUpdate` (`edit_delete.go:69`) for multi-table. Reads filter `WHERE deleted_at IS NULL`.
* **Projection**: `Client.PushState(entityID, state, attributes)` (`ha/client.go:41`), no-op when `baseURL==""` i.e. `HA_INGEST_ENABLED=false` (`ha/client.go:48`, `main.go:218-232`). Attribute-cache shape `{items:[…]}` / `{rows:[…]}` (cf. `*_history` sensors `processor.go:1643-1801`).
* **Config KV**: `ada_config` via `UpsertConfig`/`GetConfig` (`queries/config.sql`) for persisted derived state (e.g. last-notified markers), as feeding uses for `next_feeding_target`.
* **Timers**: one-shot per-target reminder via `time.AfterFunc`, armed on write + re-armed on restart — `setFeedingAlertTimer`/`restoreAlertTimer` (`processor.go:1807-1840`). Periodic reconcile (drift without an event) via the 60s safety-net inside `startBoundaryTicker`/`runSafetyNet` (`processor.go:1508-1574`).
* **Caretaker push**: `p.q.GetActivePeopleWithChannels` → `p.ha.Notify` (`processor.go:1844-1873`).
* **Time**: `parseRFC3339` / `toTimestamptz` / `.UTC().Format(time.RFC3339)` (`processor.go:1985-1997`); store `TIMESTAMPTZ`.
* **Tests**: `//go:build fast`, table-driven, colocated `*_test.go` (`processor_test.go`, `data_integrity_test.go`, `trends_test.go`).

**Test-data integration points (enumerated lists to extend):** clear-script counts `ada-db-clear-test.sh:25-29` & `:64-68`, deletes `:56-60`; snapshot tables `ada-db-lib.sh:80-81` (`run_pg_dump -t …`); seed `scripts/seed-ada-test-data.sql` + `scripts/ada-db-seed.sh`; **birth-watcher** `validate_nuke` SQL in `scripts/ada-birth-watch.sh:40-45` (sum of `test=true` across tracking tables) — must include `medication_events`.

---

## Data model (migration `000008`)

One migration creates all five tables up front (so the test-data/birth tooling covers them from first existence). `order` is reserved → `sort_order`. `fixed_times` as `TEXT[]`. All tables carry `logged_by TEXT NOT NULL DEFAULT ''`, `deleted_at TIMESTAMPTZ`, `created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`, `test BOOLEAN NOT NULL DEFAULT false`, plus `idx_<t>_test ON <t>(test) WHERE test=true` and a primary read index.

* **medications** — `id`, `name`, `route` (oral|drops|topical|suppository), `measure_unit` (mL|mg|drops|supp), `min_interval_hours NUMERIC NULL`, `max_per_24h INT NULL`, `active BOOLEAN NOT NULL DEFAULT true`. *(identity + safety only — no dose, no schedule)*
* **medication_routines** — `id`, `medication_id` FK→medications, `dose_amount NUMERIC`, `schedule_type` (fixed_times|interval), `fixed_times TEXT[] NULL`, `interval_hours NUMERIC NULL`, `end_type` (none|max_doses|end_date), `end_value TEXT NULL`, `status` (active|completed).
* **medication_events** — `id`, `medication_id` FK, `status` (given|skipped|missed), `timestamp TIMESTAMPTZ`, `routine_id` FK NULL, `slot_time TEXT NULL` ("HH:MM"), `dose_amount NUMERIC NULL`, `dose_unit TEXT NULL` *(snapshot)*, `source` (scheduled|prn) NULL, `within_window_override BOOLEAN NOT NULL DEFAULT false`, `series_id` FK NULL, `started_watch BOOLEAN NOT NULL DEFAULT false`. *(`logged_by` = actor; `missed` rows are actorless/system-written)*
* **medication_temp_series** — `id`, `medication_id` FK, `interval_hours NUMERIC`, `anchor_dose_id` FK→medication_events, `status` (active|resolved|disregarded|expired), `ended_reason TEXT NULL`.
* **emergency_rows** — `id`, `sort_order INT`, `type` (contact|live_field), `label TEXT`, `name/phone/address TEXT NULL`, `field_key TEXT NULL`.

FKs declared `ON DELETE CASCADE` for parity with feedings (dormant under soft-delete). **Cascade on `medication.delete` is app-level soft-delete**: soft-delete the med + its routines + its active series in one transaction (mirrors the feeding multi-table tx, `edit_delete.go:69`).

---

## Pre-conditions

* [ ] Confirm the dashboard’s actual `payload.event` strings (PR #103) match the spec §3 table verbatim — the gateway allowlist keys must be exact (feedback: *event route keys must match HA-side names exactly; never guess*). Verify by reading the HA card source under `/opt/homeassistant` (read-only — no commits there) **or** firing one of each from the dashboard and grepping `docker logs ruby-core-prod-gateway | grep "unknown event type"`.
* [ ] Confirm the spec §4 sensor entity IDs and §5 formulas against the shipped card’s read-seam (same source), so server output matches client expectations field-for-field.
* [ ] `~/go/bin/sqlc` present (v1.30.0) and dev Postgres reachable; dev engine runs migrations at boot.
* [ ] Working from `origin/main` (currently v0.20.0); each phase branches fresh off `origin/main`.

---

## Steps

### Step 0 — Roadmap + ADRs (own branch `docs/ada-meds-emergency`, or first commit of Phase 1)

**Action:** Draft **ROADMAP-0011** (goal: “Persist & compute Medications + Emergency server-side so the dashboard can drop its client-local store”; efforts → the 4 phase branches; Done-When + ACs). Draft **ADR-0037** (Medications & Emergency persistence: reuse the ada processor + event/sensor contract; birth-wipe classification from Context-decision 1; reminder via caretaker push, decision 2) and **ADR-0038** (server-owned medication safety computations: the engine is authoritative for `next_due`/`earliest_safe`/`doses_in_24h`; `missed` and series-expire are the **only** engine-written events; the missed-never-stacks / no-phantom-dose / given-only-feeds-math invariants).
**Verification:** `markdownlint` clean; ADRs use `~/.claude/templates/ADR-TEMPLATE.md` with Alternatives + split Consequences; ROADMAP uses its template with verifiable ACs. Both committed before code.

### Phase 1 — Schema + registry/routines CRUD (`feat/ada-meds-schema-registry`)

**Action:**

1. Add `000008_medications.up.sql` (+ `.down.sql` dropping all five tables) with the §“Data model” schema.
2. Add type constants + payload structs to `pkg/schemas/ada.go` for `medication.upsert/delete`, `medication.routine.upsert/delete`, and (stub now, used later) the event/series/emergency types — or add per-phase; minimum this phase: med + routine upsert/delete.
3. Add `eventRoutes` entries in `gateway/ada/publish.go` for the registry/routine event keys.
4. Add `store/queries/medications.sql` (Upsert/SoftDelete/List for medications + routines; cascade-soft-delete query set) → `cd services/engine/processors/ada/store && sqlc generate`.
5. New `services/engine/processors/ada/medications.go`: `handleMedicationUpsert/Delete`, `handleRoutineUpsert/Delete` (registry/routines use `eventTest(evt)`); add switch cases in `processor.go ProcessEvent`.
6. Project `sensor.ada_medications` (`{items:[…]}`) and `sensor.ada_med_routines` (`{items:[…]}` incl. `status`) via `PushState`, pushed after every mutation.
7. **Test-data + birth tooling:** add all five tables to `ada-db-lib.sh` `run_pg_dump -t` list and to `ada-db-clear-test.sh` count/delete blocks; add `medication_events` to `ada-birth-watch.sh` `validate_nuke`. (Tables empty for now; wiring lands with creation so nothing is ever un-snapshotted/un-wiped.)
**Verification:**

* `go build ./...` clean; `git status` shows no unintended `*.sql.go` drift beyond the new file.
* Dev round-trip: publish a `ha.events.ada.medication_upsert` CloudEvent to dev NATS (`tls://127.0.0.1:4222`, via the gateway `POST /ada/events` or `nats-admin.sh`), then `ada-db-lib`-style psql against `ruby_core_dev`: `SELECT name, route, active FROM medications` returns the row; routine upsert likewise; `medication.delete` sets `deleted_at` on the med **and** its routines/series (verify `deleted_at IS NOT NULL` for all three).
* `make ada-db-clear-test ENV=dev` dry-run lists the new tables’ `test=true` counts without error.

### Phase 2 — Med events + history + edit/delete (`feat/ada-meds-events-history`)

**Action:**

1. Constants/structs for `medication.given`, `medication.skipped`, `medication.series.start/end`, `medication.event.update`, `medication.event.delete`; gateway `eventRoutes` entries.
2. Queries: insert MedEvent (with dose snapshot `dose_amount`+`dose_unit`), insert/soft-delete series, `UpdateMedicationEvent` (dose/timestamp correction), `SoftDeleteMedicationEvent`, plus read queries (`GetLast24hMedEvents`, `GetLastGivenForMed/Routine`, given-count windows).
3. Handlers in `medications.go`: `handleMedicationGiven/Skipped` (events use `p.eventTestOrPreBirth(evt)`), `handleSeriesStart/End`, `handleMedEventUpdate/Delete` (mirror `handleDiaperUpdate` + soft-delete). Series `start` records `anchor_dose_id`; each new `given` for an active series re-anchors it (decision deferred-compute is fine; persist anchor).
4. Project `sensor.ada_med_events` (`{items:[…]}` with stable `id` + dose snapshot), recorder-exclude on the HA side (note in runbook; ruby-core only projects).
**Verification:**

* `go test -tags=fast ./services/engine/...` passes (incl. new history-build unit tests asserting JSON marshals `[]` not `null` on empty — cf. `data_integrity_test.go`).
* Dev: publish `given` → `SELECT status,dose_amount,dose_unit FROM medication_events` shows the snapshot; `event.update` changes dose but not identity; `event.delete` sets `deleted_at` and the row leaves `sensor.ada_med_events` projection input (verify via the build function in a unit test, since dev HA push is a no-op).
* Invariant check (unit test): `skipped`/`missed` rows excluded from any “given” query.

### Phase 3 — Server-owned computations (`feat/ada-meds-computations`)

**Action:** (the heart — match client formulas exactly; cite them in ADR-0038)

1. **Derived projections** (computed in the routine/med push): `next_due` (fixed → routine `fixed_times`; interval → `last_given.timestamp + interval_hours`; series → `anchor_dose.timestamp + interval_hours`), `earliest_safe` = `last_given + min_interval_hours`, `doses_in_24h` = count of **given** in trailing rolling 24h. Only `status='given'` feeds all of it.
2. **System-emitted `missed`** (safety-net reconcile, 60s): for each active fixed-schedule routine, when a slot is unacted **and a later slot has already come due**, insert one actorless `missed` MedEvent — **never stacks, no carry-forward, no catch-up**. Idempotent: guard so a given (slot) isn’t double-missed (unique-ish on routine_id+slot_time+local-day).
3. **Routine auto-complete**: when given-count ≥ `max_doses`, or `end_date` passed → set `status='completed'`. **Never write a phantom dose** on completion.
4. **Series ~24h auto-expire**: active series whose `anchor_dose` is older than the backstop window → `status='expired'` (Exit 3; dashboard owns Exits 1 & 2).
5. **Due reminders**: arm one-shot timers at each routine’s `next_due` (mirror `setFeedingAlertTimer`), re-armed on each given/skip and on restart (mirror `restoreAlertTimer`, persisting markers in `ada_config`); on fire, `p.ha.Notify` active caretaker channels with “Medication due — {med name}”. Dedup so a due isn’t re-sent every tick.
6. **Parity tests** (`medications_test.go`, `//go:build fast`): earliest-safe boundary (exactly at vs. one second before/after `min_interval`), rolling-24h window edges, supersession-never-stacks (two unacted slots ⇒ exactly one carry-forward, not two), auto-complete-no-phantom-dose (completion writes zero events), series re-anchor + expire.
**Verification:** `go test -tags=fast ./services/engine/...` green incl. all parity cases; dev: publish a sequence of `given` events and assert `medication_events` given-count + a psql recomputation of `earliest_safe`/`next_due` matches the engine’s computed values (log them at INFO for the check); confirm a completed routine has `status='completed'` and **no** extra event row.

### Phase 4 — Emergency (`feat/ada-emergency`)

**Action:** Constants/structs + gateway routes for `emergency.row.upsert`, `emergency.row.delete`, `emergency.reorder`; queries (upsert row, soft-delete, bulk `sort_order` update from an ordered id list); `emergency.go` handlers (config → `eventTest(evt)` only); project `sensor.ada_emergency_card` (`{rows:[…]}` ordered by `sort_order`). Live fields resolve client-side off existing growth/age sensors — **persist rows + order only, no value service**. Extend seed (`seed-ada-test-data.sql`) with a couple of representative emergency rows + a small med set (meds + routines + given events), all `test=true, logged_by='seed'`.
**Verification:** `go test -tags=fast ./...`; dev: upsert three rows + `reorder` → `SELECT label,sort_order FROM emergency_rows ORDER BY sort_order` reflects the new order; `make ada-db-seed ENV=dev DOB=…` then `make ada-db-clear-test ENV=dev CONFIRM=yes` removes all seeded `test=true` rows across the new tables (count returns 0 after).

### Final step (rides Phase 4 branch) — docs + Pre-PR

**Action:** Update `services/engine/README.md` (new processors/sensors), `services/gateway/README.md` (new routes), `docs/runbooks/ada-test-data.md` (new domains in seed/clear/snapshot + recorder-exclusion note + birth classification), and archive PLAN-0028 → `docs/plans/archive/` (status Complete) as the last commit. Run the Pre-PR checklist per CLAUDE.md.
**Verification:** pre-commit hooks clean (golangci-lint, gitleaks, markdownlint, shellcheck, fast tests); README/runbook reflect reality.

---

## Rollback

* **Schema** is additive (5 new tables); each phase’s `000008_*.down.sql` drops them — no impact on existing Ada data. The `test`-flag/birth-wipe changes only add the new tables to existing `WHERE test=true` deletes; existing tracking tables are untouched.
* **Deploy ordering risk (flag):** the `ada-db-clear-test.sh`/`validate_nuke` edits reference `medication_events` etc., which exist only after `000008` runs. The engine runs migrations at **boot**, early in `make deploy-prod`; the birth-watcher script updates on the same `git checkout` but its running process only re-reads on `systemctl restart ada-birth-watcher` (manual, post-deploy). Net: tables exist before the new watcher script runs — safe — **but** do not run `make ada-db-clear-test` against prod between checkout and the engine finishing its migration. Per-phase, this only matters on the Phase 1 deploy.
* **Per-PR rollback** is redeploy-previous-image (engine is the only changed service besides the gateway allowlist; both are stateless beyond the additive migration). The migration is forward-only in practice (down drops tables ⇒ data loss) — standard for this repo; snapshot first via `make ada-db-snapshot ENV=prod`.
* **No destructive op runs without confirmation**; the birth-watcher’s snapshot-then-nuke is unchanged except for the added table in its validation count.

## Open Questions

None blocking — the three consequential forks were resolved (birth classification, reminder delivery, doc structure). The pre-conditions (exact `payload.event` strings + sensor IDs/formulas from the shipped card) are **verification tasks against PR #103**, not unresolved decisions; §3/§4/§5 of the spec are treated as authoritative and confirmed before Phase 1 code.

## Deliverables created by this plan

* ROADMAP-0011, ADR-0037, ADR-0038 (Step 0).
* New code: `000008` migration, `pkg/schemas/ada.go` additions, `gateway/ada/publish.go` routes, `store/queries/{medications,emergency}.sql` (+ generated), `processors/ada/{medications,emergency}.go`, `medications_test.go`/`emergency_test.go`.
* Tooling: extended `ada-db-clear-test.sh`, `ada-db-lib.sh`, `seed-ada-test-data.sql`, `ada-birth-watch.sh`.
* New make targets: none required (existing `ada-db-*` targets cover the new tables once the scripts are extended).
