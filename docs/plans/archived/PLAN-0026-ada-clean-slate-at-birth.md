# PLAN-0026 - Ada clean slate at birth (ADR-0035)

* **Status:** Complete
* **Date:** 2026-06-18
* **Project:** ruby-core
* **Roadmap Item:** none (go-live readiness; follows ROADMAP-0010)
* **Branch:** feat/ada-birth-clean-slate
* **Related ADRs:** ADR-0035, ADR-0031

---

## Scope

Guarantee a clean Ada history at birth: force `test=true` on all pre-birth events, backfill existing
pre-birth rows, and auto-clear `test=true` data on the first `ada.born`. Out of scope: changing the
operator-run `make ada-db-clear-test` (kept for ongoing testing); a snapshot inside the birth clear
(operator runs `make ada-db-snapshot` if they want a pre-birth backup).

## Pre-conditions

* [x] On `feat/ada-birth-clean-slate` (current `origin/main`).
* [x] Verified: prod has 371 `test=false` pre-birth rows; `ada_profile` empty (real birth not
      blocked); `ada_born=off`, `ada_live_test=on`.

## Steps

### Step 1 — Backfill migration

**Action:** `000007_backfill_test_flag.up.sql` sets `test = true` on all existing rows of the five
Ada tables (`WHERE test = false`). `.down.sql` is a no-op (original values are unrecoverable —
documented).
**Verification:** on a scratch DB seeded with mixed test flags, after migrate-up all rows are
`test=true`.

### Step 2 — Force test pre-birth

**Action:** Add an atomic `born` flag to the `Processor`, set in `Initialize` from `GetProfile`
(born = profile exists). Replace `eventTest(evt)` at every insert site with
`p.eventTestOrPreBirth(evt)` (`= eventTest(evt) || !born`), threading the bool through
`insertTummyAndPush`.
**Verification:** `go build ./...`; new `TestEventTestOrPreBirth` (born=false forces true; born=true
honors payload).

### Step 3 — Auto-clear on first birth

**Action:** Add per-table `DeleteAll*` (`DELETE FROM <table>`) queries; regenerate sqlc. Rewrite
`handleBornEvent`: if `GetProfile` returns a row → already born, set `born=true`, return (no clear).
If absent → `UpsertProfile`, run `clearTracking` (delete ALL tracking rows — not only `test=true`,
so out-of-band/API-seeded data is also caught; config tables untouched), set `born=true`,
`refreshAllSensors`. Gated so a re-fired birth never clears.
**Verification:** build; manual: firing `ada.born` once on a test DB wipes all tracking, keeps
config, saves the profile; a second `ada.born` is a no-op.

### Step 4 — Docs, build, lint, tests

**Action:** Update `docs/runbooks/ada-test-data.md` (birth clean-slate behavior + pre-birth snapshot
note). `go build ./...`; `go test -tags=fast ./...`; golangci-lint.
**Verification:** all green.

## Rollback

Code is a clean revert. The `000007` backfill and the birth clear are **irreversible** data
operations (by design — the data is test data). Reverting the code after the backfill leaves rows
`test=true` (harmless). There is no rollback for a birth clear once fired; an operator wanting a
pre-birth backup runs `make ada-db-snapshot ENV=prod` first.

## Open Questions

None — approach approved (test=true + backfill).
