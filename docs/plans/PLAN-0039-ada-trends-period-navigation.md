# PLAN-0039 - Ada trends period navigation + calendar-anchored windows

* **Status:** Approved
* **Date:** 2026-07-05
* **Project:** ruby-core
* **Roadmap Item:** none (issue-driven: #161)
* **Branch:** feat/ada-trends-period-navigation
* **Related ADRs:** ADR-0032 (amended by this plan), ADR-0043 (deliberate divergence, scope unchanged)

---

## Scope

Implement issue #161 for `ada.trends.query`: an optional `offset` parameter (0 = current
period, -n = n periods back), fixed calendar-anchored windows (week = Sun–Sat; month =
calendar month bucketed into Sun–Sat weeks clipped to the month, 4–6 buckets; year =
calendar year, 12 month-buckets), current-day inclusion with zero-filled future buckets,
window metadata in the response (`window_start`, `window_end`, `days_elapsed`, echoed
`offset`, `min_offset`), like-for-like `prevGrand` deltas (truncated to `days_elapsed` for
the current partial period), and graceful pre-DOB windows (valid zeroed responses).

Out of scope: the HA-side dashboard UI (period chevrons, window label, adopting
`days_elapsed`) — separate PR in the homeassistant repo; promotion of the trends channel to
an HA WebSocket command (ADR-0032 Alternative C, unchanged); any SQL/sqlc query changes.

Root-cause note: the defect reported in #161 ("current day never appears") is actually
bedtime-boundary label skew — buckets run 19:00→19:00 (ADR-0043 boundary) and are labeled
by their start day, so Sunday-daytime data sat in a bucket labeled "Sat". Calendar
anchoring removes the skew.

Design decisions (approved 2026-07-05):

1. Trend buckets are true calendar days at **local midnight** (container TZ =
   America/New_York), deliberately diverging from ADR-0043's bedtime rollover, which
   continues to govern the Today view. Sleep sessions attribute to their start day;
   wakeups are restamped to the night's start day via a noon cut.
2. Month view buckets are Sun–Sat weeks clipped to the month (4–6 buckets, partial edge
   weeks allowed).
3. `min_offset` is included in the response, computed from `ada_profile.birth_at`
   (omitted when the profile is unset).

Compatibility constraints (MUST hold): sensor state = `request_id`; attributes
`request_id`, `buckets[{segs,total,label}]`, `totals`, `grand`, `prevGrand` (camelCase)
and all seg keys unchanged. All new fields are additive; the current HA client ignores
unknown attributes.

---

## Pre-conditions

* [x] Branch `feat/ada-trends-period-navigation` created from origin/main tip (882004f).
* [x] Issue #161 requirements confirmed; three design decisions approved by owner.
* [x] Current contract verified against the live HA client (`/opt/homeassistant`
      frontend source reads `buckets/totals/grand/prevGrand/request_id`; renders
      `label` verbatim; unknown attributes ignored).

---

## Steps

### Step 1 — Plan doc on branch

**Action:** Commit this plan (`docs: add PLAN-0039 ada trends period navigation`).

**Verification:** `git log --oneline -1` shows the commit; file present in `docs/plans/`.

### Step 2 — Amend ADR-0032

**Action:** Rewrite Decision §5 of `docs/adr/0032-ada-trends-acquisition.md` for
calendar-anchored windows (MUST language); add Decision items for `offset` (int ≤ 0,
positive clamped to 0), response fields (`window_start`, `window_end` inclusive date,
`days_elapsed`, echoed `offset`, `min_offset`), full-shape zero-fill, truncated
`prevGrand` semantics (with prev-month clamp), pre-DOB zeroed responses, and wakeup
night-attribution (noon cut, stamped at night-start day). Add Alternatives Considered
entries (bedtime-aligned windows — rejected for label/navigation ambiguity; fixed-width
offset-shifted windows — rejected for calendar drift) and an explicit "Divergence from
ADR-0043" paragraph. Commit
(`docs: amend ADR-0032 for calendar-anchored trends windows and offset navigation`).

**Verification:** ADR covers R1–R6 with MUST/SHOULD phrasing; grep finds the ADR-0043
divergence note.

### Step 3 — Request schema

**Action:** Add `Offset int` (`json:"offset,omitempty"`) to `AdaTrendsQueryData` in
`pkg/schemas/ada.go` with a doc comment (≤ 0; absent = 0 = current period; positive
values clamped).

**Verification:** `go build ./...` passes.

### Step 4 — Calendar window math

**Action:** New `services/engine/processors/ada/trends_window.go` with pure functions
(all calendar math via `time.Date`/`AddDate`, never fixed durations — DST-safe):
`midnight`, `calendarWindow(period, offset, now)`, `calendarBuckets(period, winStart,
winEnd)` (month iterator: `step := (7-int(cur.Weekday()))%7; if step==0 {step=7}`,
clipped to winEnd), `daysBetween` (AddDate loop), `daysElapsedIn`, `minOffsetFor`
(clamped ≤ 0), `normalizeOffset`. Delete `periodSpec` and `trendBuckets`; keep
`bucketLabel` unchanged.

**Verification:** Step 5 tests pass; `grep -rn periodSpec services/` returns nothing.

### Step 5 — Window-math tests

**Action:** New `services/engine/processors/ada/trends_window_test.go`
(`//go:build fast`, `time.LoadLocation("America/New_York")`): window offsets including
month year-rollover (July 2026, offset −7 → Dec 2025); DST weeks 2026-03-08 (23h day)
and 2026-11-01 (25h day) with boundaries asserted by `time.Date` equality; Aug 2026 →
6 buckets with labels `8/1, 8/2, 8/9, 8/16, 8/23, 8/30`; Feb 2026 → exactly 4 buckets;
year → 12 buckets incl. leap 2028; bucket shape independent of `now` (R3);
`daysElapsedIn` (Sunday→1, Jul 5 year-view→186, offset −1 Feb→28, year 2024→366);
`minOffsetFor`; `normalizeOffset`. Delete `TestTrendBuckets_Week` and
`TestTrendBuckets_MonthYearCounts` in the same commit.

**Verification:** `go test -tags=fast -race ./services/engine/processors/ada/` green.

### Step 6 — aggregateTrend explicit prev window

**Action:** `aggregateTrend` gains `prevStart, prevEnd time.Time`; `prevGrand` counts
events iff `[prevStart, prevEnd)`; everything unmatched drops. Rewrite
`TestAggregateTrend` on calendar buckets plus regressions: event in
`[prevCmpEnd, windowStart)` dropped (truncation guard), event exactly at `prevEnd`
excluded, prev-month clamp (now = 2026-03-30, Feb comparison).

**Verification:** targeted `-run AggregateTrend` green.

### Step 7 — Wakeup night attribution

**Action:** `sleepWakeupEvents(rows, loc)` replaces the b0-relative index with a noon
cut: session starting local hour < 12 keys to the previous calendar day; wakeups
(beyond-first per key) emit stamped at the night-day's local midnight.
`sleepEvents`/`trendEvents` drop `b0` and take `loc`. Test
`TestSleepWakeupEvents_NightDayAttribution`: 21:00 + 02:00 pair → one wakeup on day D;
lone 02:00 → keyed D−1, no wakeup; naps ignored; UTC-instant rows prove `.In(loc)`
conversion.

**Verification:** test green; `grep -n b0 services/engine/processors/ada/trends.go`
returns nothing.

### Step 8 — Handler + response wiring

**Action:** `trendResponse` gains `WindowStart`, `WindowEnd`, `DaysElapsed`, `Offset`,
`MinOffset *int` (snake_case JSON; `min_offset,omitempty`). `handleTrendsQuery`:
normalize offset (warn on clamp; echo even on the unsupported-metric early return);
compute window/buckets/days_elapsed via Step 4 functions; prev window =
`calendarWindow(period, offset-1, now)` with `prevCmpEnd` truncation clamp for
offset 0; fetch since `prevStart`; `window_end` = inclusive last day
(`winEnd.AddDate(0,0,-1)` formatted `2006-01-02`); `min_offset` via `GetProfile`
(nil on error/unset). Remove `computeTodayBoundary`/`loadSleepConfig` from the
handler; update the file-top comment. `pushTrends` untouched. Add
`TestTrendResponseJSON_Additive` asserting exact JSON keys (legacy intact incl.
camelCase `prevGrand`; `min_offset` omitted when nil).

**Verification:** `go build ./...`; full `go test -tags=fast -race ./...` green;
`computeTodayBoundary` absent from trends.go.

### Step 9 — Pre-push

**Action:** Run `go test -tags=fast -race ./...` and
`/home/primaryrutabaga/bin/golangci-lint run --new-from-rev=origin/main`. Run the
Pre-Push Checklist; archive this plan to `docs/plans/archived/` (Status → Complete) as
the final commit; notify owner ready-to-push.

**Verification:** both commands exit 0; plan archived on this branch; checklist results
included in the ready-to-push note.

---

## Rollback

Revert the merge commit. All changes are additive sensor attributes and in-process
window math — no migrations, no persisted state, no operational surface. The HA
dashboard keeps functioning against either contract version.

---

## Post-merge verification (live)

Fire `ada.trends.query {"metric":"diapers","view":"count","period":"week","offset":-1,
"request_id":"plan-check"}` and inspect `sensor.ada_trends`: 7 buckets `Sun…Sat`,
`window_start`/`window_end` spanning the previous Sun–Sat, `days_elapsed: 7`,
`offset: -1`, `min_offset` present. Repeat with `offset: 0` in the evening: today is
the last populated bucket, future day-slots zeroed, delta uses the truncated
comparison.
