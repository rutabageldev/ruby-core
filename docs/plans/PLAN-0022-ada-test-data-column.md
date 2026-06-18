# PLAN-0022 - Ada test-data marking column (ADR-0031)

* **Status:** In Progress
* **Date:** 2026-06-18
* **Project:** ruby-core
* **Roadmap Item:** docs/roadmap/ROADMAP-0010-ada-hardening-test-data-lifecycle.md (effort 0010.5)
* **Branch:** feat/ada-write-and-test-lifecycle
* **Related ADRs:** ADR-0031

---

## Scope

Add `test BOOLEAN NOT NULL DEFAULT false` to every Ada event/record table and persist the flag
from the event payload through every insert path, so seeded/test data is distinguishable yet
behaves identically in all projections (ADR-0031). Out of scope: the seed/clear targets (0010.6);
the consumer-side `test:true` stamping (homeassistant repo).

## Pre-conditions

* [x] On `feat/ada-write-and-test-lifecycle`; effort 0010.4 committed.
* [x] ADR-0031 merged to main.

## Steps

### Step 1 — Migration

**Action:** Add `000006_test_flag.up.sql` adding `test BOOLEAN NOT NULL DEFAULT false` to
`feedings`, `diapers`, `sleep_sessions`, `tummy_time_sessions`, `growth_measurements`; `.down.sql`
drops them. (Child tables `feeding_segments`/`feeding_bottle_detail` need no column — they cascade
from the parent feeding on clear.)
**Verification:** on a scratch DB, `migrate up` then `\d feedings` shows `test`; `migrate down`
removes it. Engine applies it on startup via the existing `MigrateUp`.

### Step 2 — Carry the flag on insert payloads

**Action:** The `test` marker is envelope-level (present on every event the dashboard fires in
live-test mode), so read it generically via `eventTest(evt)` from `evt.Data["test"]` rather than
adding a field to each typed payload struct. Updates deliberately do NOT carry/change `test` — a
row's test-ness is fixed at creation, so editing never flips it (and never accidentally makes real
data clearable).
**Verification:** new `TestEventTest` passes (true / false / absent / wrong-type cases).

### Step 3 — Persist the flag

**Action:** Add `test` to the insert queries (`InsertFeeding`, `InsertDiaper`, `InsertSleepStart`,
`InsertSleepSession`, `InsertTummySession`, `InsertGrowthMeasurement`); regenerate sqlc. Pass
`d.Test` at every call site (thread a `test bool` param through `insertTummyAndPush`).
**Verification:** `sqlc generate` clean; `go build ./...`; no projection filters on `test`
(grep shows `test` only in inserts + future clear, never in a SELECT/aggregate).

### Step 4 — Build, lint, fast tests

**Action:** `go build ./...`; `go test -tags=fast ./...`; golangci-lint on changed packages.
**Verification:** all green.

## Rollback

`migrate down` drops the columns (additive migration, clean reverse). Revert the commit. No data
loss on rollback — the columns default false, so dropping them only loses the test/real
distinction, not rows.

## Open Questions

None — ready for execution.
