# ADR-0043 - Ada boundary-based "Today" rollover (configurable bedtime, not UTC midnight)

* **Status:** Accepted
* **Date:** 2026-06-29
* **Supersedes:** *(none)*
* **Superseded by:** *(none)*

---

## Context

Backfilled to document a decision shipped in `feat/ada-bedtime-boundary` (v0.10.0); no ADR was
written at the time (drift #32).

The Ada baby-tracking dashboard shows "Today" aggregates — today's feeding count/oz, diaper
counts, sleep totals, tummy-time minutes. The question is **when "today" rolls over**. A newborn's
day is not the civil calendar day: caregivers think in terms of the overnight period and the
bedtime that bounds it, and a roll-over at calendar midnight splits a single night's sleep and
feeds across two "days", making the dashboard wrong precisely when tired parents read it at 2 AM.

Forces:

* **The parenting day is anchored to bedtime, not midnight.** A feed at 23:30 and the next at
  02:00 belong to the same caregiving night.
* **The boundary must be configurable** — bedtime differs per household and shifts as the baby
  grows.
* **ruby-core is the single source of truth** for derived state (ADR-0023/ADR-0033). The dashboard
  must not compute the boundary independently, or two definitions of "today" drift apart.
* **Overnight sessions straddle the boundary.** A sleep that starts before the boundary and ends
  after it must count only its post-boundary portion toward "today".

## Alternatives Considered

**UTC midnight rollover** — Trivial (`date_trunc('day', ... )`), already the implicit default.
Rejected: it is the wrong timezone and the wrong semantic — it splits the caregiving night and
rolls over in the middle of the local evening/night.

**Local calendar midnight** — Correct timezone, still the wrong semantic: a 00:30 feed would start
a new "today" mid-night, resetting counts while the parents are still in the same session.

**HA-computed boundary** (dashboard derives "today" itself) — No backend change. Rejected:
splits ownership of a derived value across two systems; ruby-core must be the single source of
truth (ADR-0023), and automations/sensors outside the dashboard also need the boundary.

**Per-event "night id" stamped at write time** — Assign each event to a caregiving-night bucket on
ingest. More flexible for historical re-bucketing, but materially more complex (every event carries
a derived bucket; changing bedtime would require backfill). Rejected for the MVP; the on-read
boundary is sufficient and has no stored derived state to drift.

## Decision

1. **Configurable bedtime boundary.** The "Today" boundary **MUST** be derived from a configurable
   bedtime stored in `ada_config` (`bedtime_hhmm`, `HH:MM` 24h, default `19:00`), not from UTC or
   local calendar midnight. `computeTodayBoundary(bedtimeHHMM)` is the **canonical** computation:
   if now ≥ today's bedtime the boundary is today at bedtime, else yesterday at bedtime; on a parse
   error it falls back to UTC-midnight-today.

2. **Aggregate queries take the boundary as a parameter.** Every "today" aggregate query **MUST**
   accept the boundary (`@boundary`) rather than computing a day bucket in SQL. Overnight sessions
   **MUST** be clipped to the boundary — sleep/tummy duration uses
   `GREATEST(start_time, @boundary)` so only the post-boundary portion counts toward today.

3. **Daily refresh is boundary-driven.** A once-per-day ticker at the bedtime boundary (not a
   midnight cron) **MUST** trigger the daily-aggregate refresh; boundary crossings are counted
   (`ada_boundary_crossings_total`).

4. **The boundary is published, not re-derived downstream.** The engine **MUST** push
   `sensor.ada_today_boundary` (the boundary instant + `bedtime_hhmm`/`daytime_hhmm`/grace
   attributes) so the dashboard and automations display elapsed-since-boundary and pre-populate the
   config screen **without** computing the boundary independently.

## Consequences

### Positive

* "Today" matches how caregivers actually experience the day; counts don't reset mid-night.
* One canonical boundary (`computeTodayBoundary` + `sensor.ada_today_boundary`) — no split-brain
  between backend and dashboard.
* Bedtime is tunable per household via `ada_config` without code changes.

### Negative

* Every aggregate query must thread a boundary parameter; a new "today" query that forgets it
  silently reverts to a wrong (un-clipped) total — a standing footgun.
* Daily rollover logic lives in the processor (the ticker + boundary math), not in SQL, so it is
  only exercised when the engine runs.

### Neutral

* The boundary is computed on read (no stored per-event night bucket), so changing `bedtime_hhmm`
  re-buckets all "today" reads immediately — convenient, but means historical "today" snapshots are
  not pinned.
* Sleep categorization (`nap` vs `night`) uses the same bedtime/daytime/grace config but is a
  separate concern from the Today boundary.
