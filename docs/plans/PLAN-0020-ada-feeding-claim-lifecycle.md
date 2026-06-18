# PLAN-0020 - Ada feed-claim lifecycle (#19, #81)

* **Status:** In Progress
* **Date:** 2026-06-18
* **Project:** ruby-core
* **Roadmap Item:** docs/roadmap/ROADMAP-0010-ada-hardening-test-data-lifecycle.md (effort 0010.3)
* **Branch:** feat/ada-hardening
* **Related ADRs:** ADR-0033

---

## Scope

Implement the `ada.feeding.claimed` event (#19) and make ruby-core the owner of the claim
lifecycle (#81): on claim, set `input_boolean.ada_feeding_claimed` on and project the claimer to
`sensor.ada_feeding_claimed_by`; clear both on the next completed feed. Out of scope: any explicit
reset event; removal of the dashboard's optimistic write (consumer-side).

## Pre-conditions

* [x] On `feat/ada-hardening`; efforts 0010.1–0010.2 committed.
* [x] `input_boolean.ada_feeding_claimed` helper exists in HA (per #19/#81).

## Steps

### Step 1 — Event contract

**Action:** Add `AdaEventFeedingClaimed = "ha.events.ada.feeding_claimed"` and
`AdaFeedingClaimedData {GotItUser, Timestamp, LoggedBy}` to `pkg/schemas/ada.go`; add
`"ada.feeding.claimed": schemas.AdaEventFeedingClaimed` to the gateway `eventRoutes`.
**Verification:** `go build ./...`; route test (Step 4) passes; publishing `ada.feeding.claimed`
no longer logs "unknown event type".

### Step 2 — Set the claim

**Action:** Add `handleFeedingClaimed` (dispatched from `ProcessEvent`). Persist the claimer to
`ada_config` key `feeding_claimed_by`, push `input_boolean.ada_feeding_claimed` = on, and push
`sensor.ada_feeding_claimed_by` (state = caregiver name, attribute `claimed_by` = same). Restore
from config in `pushLastEventSensors` so a mid-claim restart re-projects it.
**Verification:** `go build ./...`.

### Step 3 — Clear the claim on the next completed feed

**Action:** Add `clearFeedingClaim` and call it at the end of `pushFeedingSensors` (the single
chokepoint for `ada.feeding.log` / `log_past` / `end`). It is idempotent and a no-op when not
currently claimed; supplements (which use `pushSupplementOzSensor`) do NOT clear.
**Verification:** code review confirms `pushFeedingSensors` is called only by the three completed-
feed handlers (not by supplement or refresh). Runtime (manual/consumer): logging a feed clears the
banner; a supplement does not.

### Step 4 — Route test

**Action:** Add a gateway `ada` package test asserting `"ada.feeding.claimed"` maps to
`schemas.AdaEventFeedingClaimed` in `eventRoutes`.
**Verification:** `go test -tags=fast ./services/gateway/ada/...` passes.

### Step 5 — Build, lint, fast tests

**Action:** `go build ./...`; `go test -tags=fast ./...`; golangci-lint on changed packages.
**Verification:** all green.

## Rollback

Revert the commit. The only persistent state is the `ada_config` row `feeding_claimed_by`, which is
benign and ignored by all other code. Clean code rollback.

## Open Questions

None — ready for execution. Note: the claim uses `PushState` (POST /api/states) for the
input_boolean, consistent with the rest of the processor; if HA proves to revert helper state, a
follow-up can switch to an `input_boolean.turn_on/off` service call.
