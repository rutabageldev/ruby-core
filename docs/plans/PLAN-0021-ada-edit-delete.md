# PLAN-0021 - Ada edit & delete operations (#77, #79, #78)

* **Status:** In Progress
* **Date:** 2026-06-18
* **Project:** ruby-core
* **Roadmap Item:** docs/roadmap/ROADMAP-0010-ada-hardening-test-data-lifecycle.md (effort 0010.4)
* **Branch:** feat/ada-write-and-test-lifecycle
* **Related ADRs:** ADR-0029

---

## Scope

Full-resolution `ada.{feeding,diaper,sleep,tummy,growth}.update` (complete replacement of an
event's composition per #79) and `ada.{...}.delete {id}`, using the existing `deleted_at`
soft-delete columns, recomputing all derived sensors after each. Out of scope: the `test` column
(0010.5); bulk reset/import (the seed target, 0010.6).

## Pre-conditions

* [x] On `feat/ada-write-and-test-lifecycle` (fresh from current `origin/main`).
* [x] sqlc v1.30.0 available; history entries already expose stable `id` (0010.2 / prior work).

## Steps

### Step 1 — Event contract

**Action:** Add `_updated`/`_deleted` subject constants and the update payload structs
(`AdaFeedingUpdateData`, `AdaDiaperUpdateData`, `AdaSleepUpdateData`, `AdaTummyUpdateData`,
`AdaGrowthUpdateData`) plus a shared `AdaDeleteData {id}` to `pkg/schemas/ada.go`. Add all ten
routes to the gateway `eventRoutes`.
**Verification:** `go build ./...`; gateway route test (Step 4) covers every new type.

### Step 2 — Queries

**Action:** Add per-table `Update*` and `SoftDelete*` (`SET deleted_at = NOW()`) queries, plus
`DeleteFeedingSegments`/`DeleteFeedingBottleDetail` for the feeding rebuild. Regenerate sqlc.
**Verification:** `sqlc generate` clean.

### Step 3 — Handlers

**Action:** Store `cfg.Pool` on the processor (for the feeding-update transaction). Add a
`parseUUID` helper and a pure `deriveFeedingSource(leftS,rightS,breastMilkOz,formulaOz)` (reused by
`feeding.log_past`). Implement update handlers as complete replacements (feeding rebuilds segments

* bottle detail in a transaction; growth recomputes percentiles via an extracted
`computeGrowthPercentiles`) and delete handlers as soft-deletes by id. Each recomputes its derived
sensors via the existing push functions. Dispatch all ten from `ProcessEvent`.
**Verification:** `go build ./...`; new `TestDeriveFeedingSource` passes; existing tests still pass.

### Step 4 — Tests

**Action:** Add `TestDeriveFeedingSource` (pure) and gateway `TestEventRoutesEditDelete` asserting
all ten event types route to their subjects.
**Verification:** `go test -tags=fast ./services/...` passes.

### Step 5 — Build, lint, fast tests

**Action:** `go build ./...`; `go test -tags=fast ./...`; golangci-lint on changed packages.
**Verification:** all green.

## Rollback

Revert the commit. No schema migration; soft-deletes are recoverable (reset `deleted_at`), but
in-place updates overwrite prior values and are not reversible without a DB restore — exercised by
the user, not this code.

## Open Questions

None — ready for execution.
