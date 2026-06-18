# PLAN-0019 - Ada data-integrity fixes (#74, #75, #76, #80-logged_by)

* **Status:** In Progress
* **Date:** 2026-06-18
* **Project:** ruby-core
* **Roadmap Item:** docs/roadmap/ROADMAP-0010-ada-hardening-test-data-lifecycle.md (effort 0010.2)
* **Branch:** feat/ada-hardening
* **Related ADRs:** ADR-0033

---

## Scope

Four projection/handler corrections with no destructive migration: chronological `last_*` for
diaper/sleep (#76); a rolling-24h `sensor.ada_tummy_history` (#75); mixed/single supplement merge
routed by `source` onto the most-recent feed (#74); `logged_by` carried on growth-history entries
(#80). Out of scope: edit/delete (0010.4), the test column (0010.5).

## Pre-conditions

* [x] On `feat/ada-hardening`; effort 0010.1 committed.
* [x] sqlc v1.30.0 available at `~/go/bin/sqlc`.

## Steps

### Step 1 — #76 chronological last_* (diaper, sleep)

**Action:** Change `pushDiaperSensors` to derive `last_diaper_*` from `GetLastDiaper`
(MAX timestamp) instead of the inbound event. Change `pushSleepStartedSensors`/
`pushSleepEndedSensors` to delegate sleep state + `last_sleep_change` to the DB-derived
`pushActiveSleepState` (newest active start, else MAX end) instead of pushing the inbound time.
Update callers; drop now-unused params.
**Verification:** `go build ./...`; existing `TestBuildSleepHistory_*`/`TestBuildDiaperHistory_*`
still pass. Runtime (manual): back-dating an older diaper/sleep does not move `last_*`.

### Step 2 — #75 tummy history

**Action:** Add `GetLast24hTummy` to `tummy.sql`; regenerate sqlc. Add `sensorTummyHistory`,
`TummyHistoryEntry`, pure `buildTummyHistory`, and `pushTummyHistory` (24h window, matching the
other history sensors). Call from `insertTummyAndPush` and add to `refreshAllSensors`.
**Verification:** `sqlc generate` clean; new `TestBuildTummyHistory_*` passes; entries carry
`id`, `start_time`, `end_time`, `duration_s`, `logged_by`.

### Step 3 — #74 supplement merge routed by source

**Action:** Add `BreastMilkOz`/`FormulaOz` to `AdaFeedingSupplementData`. Add a pure
`supplementAmounts(source, amountOz, bmo, fo)` helper mapping: `mixed`→(bmo, fo); `breast_milk`→
amountOz into breast-milk; `formula`→amountOz into formula; `amount`=their sum. Add an additive
upsert query `AddFeedingBottleDetailAmounts` (`ON CONFLICT (feeding_id) DO UPDATE SET col =
COALESCE(existing,0)+COALESCE(excluded,0)`), regenerate sqlc, and rewrite `handleFeedingSupplemented`
to attach to the most-recent non-deleted feed (`GetLastFeedingID`) using those amounts.
**Verification:** `sqlc generate` clean; new `TestSupplementAmounts` passes; a mixed and a
single-source supplement both populate the parent feed's named oz columns and `total_oz`.

### Step 4 — #80 logged_by on growth history

**Action:** Add `LoggedBy` to `growthWeightEntry`/`growthLengthEntry`/`growthHeadEntry`; extract a
pure `buildGrowthHistory(rows)` helper from `pushGrowthHistory` and populate `logged_by` from the
row.
**Verification:** new `TestBuildGrowthHistory_LoggedBy` passes; every entry carries `logged_by`.

### Step 5 — Full build, lint, fast tests

**Action:** `go build ./...`; `go test -tags=fast ./...`; golangci-lint on changed packages.
**Verification:** all green.

## Rollback

Revert the commit. No schema migration and no persistent-state change (new queries are read/write
against existing tables; the additive upsert only touches `feeding_bottle_detail` rows created by
feeds). Clean code rollback.

## Open Questions

None — ready for execution.
