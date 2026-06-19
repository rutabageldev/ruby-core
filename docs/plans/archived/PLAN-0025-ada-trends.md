# PLAN-0025 - Ada Trends aggregation (#82, ADR-0032)

* **Status:** Complete
* **Date:** 2026-06-18
* **Project:** ruby-core
* **Roadmap Item:** docs/roadmap/ROADMAP-0010-ada-hardening-test-data-lifecycle.md (effort 0010.7)
* **Branch:** feat/ada-trends
* **Related ADRs:** ADR-0032

---

## Scope

Serve bucketed activity aggregations for the dashboard's Trends view via request/response:
`ada.trends.query {metric, view, period, request_id}` → boundary-aligned buckets → `sensor.ada_trends`
echoing the request. The final ROADMAP-0010 effort. Out of scope: promotion to an HA WebSocket
command (future, contract-compatible per ADR-0032).

## Pre-conditions

* [x] On `feat/ada-trends` (current `origin/main`); ADR-0032 merged.
* [x] Existing range queries (`GetTodayDiapers`, `GetLast24hFeedings`, `GetTodaySleepSessions`,
      `GetLast24hTummy`) are reusable with `boundary = prevWindowStart` — no new sqlc needed.

## Steps

### Step 1 — Event contract + route

**Action:** Add `AdaEventTrendsQuery` subject + `AdaTrendsQueryData {metric, view, period, request_id}`
to `pkg/schemas/ada.go`; add `"ada.trends.query"` to the gateway `eventRoutes`; dispatch from
`ProcessEvent`.
**Verification:** `go build ./...`; gateway route test.

### Step 2 — Bucketing + aggregation (pure, testable)

**Action:** Add `trends.go`: `periodSpec` (week 7×1d / month 4×7d / year 12×~30d), `trendBuckets`
(boundary-aligned to `computeTodayBoundary` + 24h), and `aggregateTrend` (assign events to buckets,
sum `totals`/`grand`, and `prevGrand` over the prior equal window). Per-metric event builders map
existing query rows to `{when, segs}` using the consumer's verbatim seg/view keys (diapers→wet/dirty/
mixed; feeding breast→left/right, bottle→milk/formula, feeds→bf/bo; sleep hours→night/nap,
wakeups→wakeups; tummy min→min, sessions→sessions).
**Verification:** `TestTrendBuckets`, `TestAggregateTrend` pass.

### Step 3 — Handler + sensor

**Action:** `handleTrendsQuery` parses, validates, computes the window from the bedtime boundary,
fetches the metric's rows since `prevWindowStart`, aggregates, and pushes `sensor.ada_trends` with
`{request_id, metric, view, period, generated_at, buckets:[{segs,total,label}], totals, grand,
prevGrand}`. Invalid metric/view/period pushes an empty (echoed) response so the dashboard never hangs.
**Verification:** unit test on a fixed dataset asserts exact bucket values + `prevGrand`.

### Step 4 — Build, lint, fast tests; close the roadmap

**Action:** `go build ./...`; `go test -tags=fast ./...`; golangci-lint. Mark ROADMAP-0010.7 done.
**Verification:** all green.

## Rollback

Revert the commit; read-only aggregation + one new sensor, no schema/state change. Clean rollback.

## Open Questions

Consumer to confirm against the `buildTrend()` mock (the established repoint-and-verify step):
the **wakeup** semantic (implemented as night sleep sessions beyond the first per night-window), the
**bf/bo** classification (by feed source), and **bucket label** formats. Seg/view keys match the
consumer's verbatim table; sleep `hours` seg keys (night/nap) were flagged in ADR-0032.
