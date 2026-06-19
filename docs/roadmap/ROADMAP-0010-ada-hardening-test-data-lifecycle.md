# Make Ada trustworthy end-to-end and safely validatable before birth

* **Status:** Complete
* **Date:** 2026-06-18
* **Project:** ruby-core
* **Related ADRs:** ADR-0031 (test-data model), ADR-0032 (trends acquisition), ADR-0033 (projection integrity)
* **Linked Plan:** PLAN-0018 … PLAN-0025 (one per effort, archived under docs/plans/archived/). Note: 0010.1 was already resolved on `main` by PR #72 before this roadmap landed.

---

**Goal:** Eliminate the wrong/missing/duplicated data the Ada dashboard shows today, give caregivers full edit/delete control over recorded events, and stand up a safe, repeatable test-data lifecycle (mark → seed → clear) so the experience can be validated end-to-end — including the production dashboard — before the real birth, without risking real records.

This roadmap addresses GitHub issues #63, #74, #75, #76, #77, #78, #79, #80, #81, #82, #19. The cross-cutting reliability finding (silent growth drops, stale `latest_*`, stripped `logged_by`) is root-caused to #63 (three concurrent gateway stacks racing on shared HA sensors from isolated databases) plus a missing `logged_by` field in the growth-history projection — both fixed here.

---

## Efforts

Each effort maps to roughly one branch/session. Ordering reflects dependencies: #63 is a hard prerequisite because it is the dominant cause of the §4 reliability symptoms and makes seeding/validation untrustworthy until fixed. Trends (#82) is deferred to last because it is fully independent and the dashboard already runs on a local mock.

### 0010.1 — Single-stack live HA ingest (#63) — ALREADY RESOLVED ON MAIN (PR #72)

Resolved before this roadmap landed: PR #72 added the `HA_INGEST_ENABLED` gate (the gateway skips the HA WebSocket entirely in dev/staging, prod-only ingest) plus dev-database isolation. This is broader than originally scoped here (it gates the whole WebSocket, not just `ada_event`) and fully addresses #63. No further work in this roadmap; ADR-0033 documents the decision. Out of scope: per-env dedicated HA instances (a future homeassistant-repo effort).

### 0010.2 — Data-integrity fixes (#74, #75, #76, #80-`logged_by`)

Pure handler/projection corrections, no schema migration: chronological `last_*` for diaper/sleep; a `sensor.ada_tummy_history` cache; mixed/single supplement merge routed by `source` onto the parent feed; `logged_by` carried on growth-history entries. Out of scope: edit/delete (0010.4).

### 0010.3 — Feed-claim lifecycle (#19, #81)

Implement the `ada.feeding.claimed` event and make ruby-core the owner of `input_boolean.ada_feeding_claimed` and `sensor.ada_feeding_claimed_by` (set on claim; cleared on the next completed feed). Out of scope: any explicit reset event (optional safety valve only).

### 0010.4 — Edit & delete operations (#77, #79, #78)

Full-resolution `ada.{feeding,diaper,sleep,tummy,growth}.update` (complete replacement of an event's composition) and `.delete` by `id`, using the existing `deleted_at` soft-delete columns, recomputing all derived sensors. Out of scope: bulk reset/import (the seed target in 0010.6 provides authoritative replacement).

### 0010.5 — Test-data marking model (ADR-0031)

Add `test BOOLEAN NOT NULL DEFAULT false` to every Ada table; persist the flag from event payload through ingestion; ensure no projection filters on it (test data renders identically). Coordinated consumer change: stamp `test:true` when `input_boolean.ada_live_test` is on. Out of scope: the seed/clear targets (0010.6).

### 0010.6 — Test-data lifecycle: seed + clear (DESTRUCTIVE — gated)

Env-parameterized `make` targets: a seed target that writes a representative, fully `test`-flagged, ~14-month dataset aligned to a `DOB=` (and writes the matching HA `input_datetime.ada_test_dob`, `ada_live_test=on`, `ada_born=off`); and a guarded clear target that deletes only `test=true` rows (dry-run/count-first, `CONFIRM=yes`, prod-safety prompt, pre-delete snapshot). Includes a one-time guarded purge of pre-existing junk and a §4 reliability harness. This effort contains the only destructive operations and must not be bundled with feature delivery. Out of scope: automated/scheduled Postgres backups (ROADMAP-0011).

### 0010.7 — Trends aggregation (#82, ADR-0032) — DONE (PLAN-0025)

Request/response over the existing channel: `ada.trends.query {metric, view, period, request_id}` → boundary-aligned bucket computation → `sensor.ada_trends` echoing `request_id` + resolved params + `generated_at`. Out of scope: promotion to an HA WebSocket command (future, contract-compatible).

---

## Done When

* Tapping a single Ada action in the dashboard produces exactly one recorded event (no 2–3× duplication), verified across all three gateway logs.
* The dashboard's "Right now" glance, Recent History (including tummy), Last-fed detail, growth chart, and Trends view all render data that is correct, complete, and attributed — back-dated entries never masquerade as "latest," mixed top-offs appear on their parent feed, and growth history is permanent and complete with `logged_by` intact.
* A caregiver can correct or delete any recorded event (feeding/diaper/sleep/tummy/growth) from the dashboard and every derived sensor recomputes.
* `make ada-db-seed ENV=<env> DOB=<rfc3339>` produces a coherent multi-month dataset across every Ada capability, and `make ada-db-clear-test ENV=<env> CONFIRM=yes` removes only that test data, leaving real records untouched — both proven by an assertion harness.

## Acceptance Criteria

* [ ] With all three stacks up, the #63 repro yields exactly one `ada: event published` line and one DB write per dashboard tap.
* [ ] Logging a back-dated diaper/sleep older than the current latest does **not** change `sensor.ada_last_diaper_time` / `sensor.ada_last_sleep_change`.
* [ ] `sensor.ada_tummy_history` exposes `entries` with `{id, start_time, end_time, duration_s, logged_by}` on the rolling-24h contract.
* [ ] A mixed `ada.feeding.supplement` (and a single-source one) merges its oz onto the most-recent feed's `ada_feeding_history` entry, routed into the correct column by `source`.
* [ ] Every `ada_growth_history` entry carries a non-empty `logged_by` when one was supplied.
* [ ] `ada.feeding.claimed` sets `sensor.ada_feeding_claimed_by` (= caregiver name) and `input_boolean.ada_feeding_claimed` on; both clear when the next `ada.feeding.log`/`log_past`/`end` is recorded, and a supplement does not clear them.
* [ ] Each `ada.<area>.update` transforms a recorded event into any other valid submission of its type and recomputes all derived sensors; `ada.<area>.delete {id}` soft-deletes and recomputes.
* [ ] `migrate up` then `migrate down` cleanly adds/removes the `test` column on all Ada tables; a `test=true` row appears in history identically to a real row.
* [ ] `make ada-db-clear-test ENV=dev` with no `CONFIRM` deletes nothing and prints per-table counts; with `CONFIRM=yes` it removes only `test=true` rows, a manually-inserted `test=false` row survives, and a pre-delete snapshot file is produced.
* [ ] The §4 harness writes N known growth records and asserts all N land in `sensor.ada_growth_history` with `logged_by` intact (exits non-zero otherwise).
* [ ] `ada.trends.query` returns a `sensor.ada_trends` payload echoing the sent `request_id` with correct boundary-aligned buckets (week 7×1d, month 4×7d, year 12×~30d) and a `prevGrand` equal to the prior window.
