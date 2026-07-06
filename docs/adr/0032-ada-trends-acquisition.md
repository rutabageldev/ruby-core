# ADR-0032 - Ada trends acquisition

* **Status:** Proposed
* **Date:** 2026-06-18 (amended 2026-07-05: calendar-anchored windows + offset navigation, issue #161)
* **Supersedes:** *(none)*
* **Superseded by:** *(none)*

---

## Context

The Ada dashboard's Trends view plots a metric (diapers / feeding / sleep / tummy) over a chosen
window (week / month / year) as per-bucket stacked aggregations, with a previous-period total for
the delta arrow. ruby-core must compute and serve these buckets: the rolling-24h history caches
and the today-totals sensors cannot supply them, and a Lovelace frontend has no NATS or
event-store access. The required answer is parametric over `(metric, view, period)` and grows as
data accumulates.

There is no backward-compatibility constraint. Today the dashboard's `buildTrend()`
(`frontend/src/ruby/lib/adaTrends.ts`) is a purely local deterministic mock behind a marked seam:
no request, no `request_id`, no network call. The frontend `TrendData` model and its consumers
stay unchanged; only the `buildTrend()` body is repointed at the real contract.

The consumer also confirmed the exact stable identifiers it sends and expects back (see Decision §4),
so the engine's query routing and response `segs` keys must match them verbatim.

**Amendment (2026-07-05, issue #161).** The original bedtime-boundary window alignment (old §5)
produced label skew: buckets ran 19:00→19:00 but were labeled by their start day, so the current
day's data appeared under the previous day's label and the chart seemed to end a day early. The
dashboard also needs to navigate past periods. This amendment replaces the window model
(Decision §5) and adds offset navigation and window metadata (Decision §§6–10). The transport
decision (result-sensor round-trip) is unchanged.

## Alternatives Considered

**A. Pre-computed cube as a sensor attribute** (the #80 growth approach) — Publish all buckets for
all (metric × view × period) on one sensor. Rejected: unlike growth's tiny append-mostly set, this
is a wide, constantly-recomputed cube that grows with time — a poor fit for a single ever-changing
attribute blob and wasteful to push on every event when the view is opened only occasionally.

**B-direct. Frontend → ruby-core gateway REST** — Rejected: reintroduces the cross-origin
auth/CORS surface explicitly rejected in #80 (ADR-0002 consumer-side: the dashboard is a pure HA
view layer).

**C. HA WebSocket custom command** — A new custom HA integration registers an `ada/trends` WS
command. Clean synchronous-style request/response with a real return value and no result-sensor
hack. Deferred (not rejected): it requires shipping and deploying a new custom HA integration
component, which is more surface than needed to ship Trends now. Retained as the clean target.

**B (chosen). Request/response over the existing `fire_ada_event` → NATS → sensor channel** —
Adds zero new transport/auth/CORS surface, stays within today's plumbing, and computes only what
is asked.

*Window-model alternatives (2026-07-05 amendment):*

**W1. Bedtime-boundary-aligned windows** (the original §5) — Rejected: day buckets labeled by a
start that is 5 hours before the calendar day they represent skews attribution (the observed
"missing Sunday" defect), and "week rolls when Sunday arrives" becomes Saturday evening, which is
unintelligible for period navigation.

**W2. Fixed-width windows shifted by offset** (keep 7×24h/4×7d/12×30d, slide by `offset` widths) —
Rejected: historical windows drift off calendar boundaries (a "month" is 28 days, a "year" 360),
so navigated periods stop corresponding to real weeks/months/years and week-over-week comparisons
lose meaning.

**W3 (chosen). Calendar-anchored windows at local midnight** — Every window is a real calendar
period; navigation and labels match the household's mental model; DST handled by calendar
arithmetic. Deliberately diverges from ADR-0043's bedtime rollover (see Decision §5 note).

## Decision

1. The dashboard mints an opaque `request_id` per query and fires
   `ada.trends.query {metric, view, period, request_id[, offset]}` through the existing
   `fire_ada_event` script. `period` MUST be one of `week`, `month`, `year`. `offset` is an
   optional integer ≤ 0: `0` (or absent) = the current period, `-n` = n periods back. The engine
   MUST clamp positive values to `0` (with a warning log), never error. `offset` applies to all
   metrics and views.
2. ruby-core computes the buckets and publishes the result to `sensor.ada_trends`. The payload
   MUST echo `request_id`, the resolved `{metric, view, period}`, and a `generated_at` timestamp,
   so the dashboard renders only the response matching its latest request and ignores stale ones.
3. The response MUST mirror the frontend `TrendData` shape:
   `{ request_id, metric, view, period, generated_at, buckets: [{ segs, total, label }],
   totals, grand, prevGrand }`, where `prevGrand` is the grand total of the period immediately
   before the requested window (see §8 for partial-period truncation). The response MUST also
   carry the window metadata of §7. All post-#161 fields are additive; the pre-#161 field set,
   key spelling (including camelCase `prevGrand`), and seg keys MUST NOT change.
4. Query routing and response `segs` keys MUST use these exact identifiers (note `milk`, not
   `breastmilk`, and `bf`/`bo` for feed counts):

   | metric    | view (id)  | seg keys                       |
   |-----------|------------|--------------------------------|
   | `diapers` | `count`    | `wet`, `dirty`, `mixed`        |
   | `feeding` | `breast`   | `left`, `right` (minutes)      |
   | `feeding` | `bottle`   | `milk`, `formula` (oz)         |
   | `feeding` | `feeds`    | `bf`, `bo` (counts)            |
   | `sleep`   | `wakeups`  | `wakeups`                      |
   | `tummy`   | `min`      | `min`                          |
   | `tummy`   | `sessions` | `sessions`                     |

   (Seg keys for any additional sleep view such as `hours` are to be confirmed with the consumer
   during the Trends effort; the dashboard's verbatim list above is authoritative for the views it
   sends today.)

5. Windows MUST be calendar-anchored at **local midnight** in the engine's local timezone
   (container `TZ`, America/New_York):
   * **week** = Sun–Sat, 7 one-day buckets; `offset: 0` rolls at Sunday 00:00.
   * **month** = the calendar month, bucketed into Sun–Sat weeks clipped to the month
     (4–6 buckets; partial edge weeks allowed); rolls on the 1st.
   * **year** = the calendar year, 12 one-month buckets; rolls Jan 1.

   All boundaries MUST be computed by calendar arithmetic (`time.Date`/`AddDate`), never by
   adding fixed durations — weeks and months containing DST transitions are not multiples of
   24h. Bucket membership is `[start, end)` on absolute instants. Bucket labels keep their
   formats: week = weekday (`Sun`…`Sat`), month = `M/D` of the bucket start, year = `Jan`…`Dec`.

   **Divergence from ADR-0043:** the Today view continues to roll at the bedtime boundary
   (ADR-0043, unchanged in scope); trends deliberately use calendar midnight instead, because
   bedtime-aligned buckets mislabel evening data and make period navigation ambiguous
   (Alternative W1). Post-bedtime evening events therefore count toward the trends bucket of the
   calendar day they occur on, which the Today view has already rolled past — an accepted,
   bounded mismatch (19:00–00:00 only).
6. The `offset: 0` window MUST include the current day as the last populated bucket. Future
   buckets within the current period MUST be present with zeroed segs so the chart shape is
   stable (always 7 day-slots, all weeks of the month, all 12 months).
7. The response MUST include window metadata: `window_start` and `window_end` (ISO `YYYY-MM-DD`;
   `window_end` is the **inclusive** last calendar day of the window), `days_elapsed` (the full
   day count for past windows; days into the period including today for `offset: 0`), the echoed
   post-clamp `offset`, and `min_offset` (see §9).
8. `prevGrand` MUST be computed over the period immediately before the requested window. For the
   current partial period (`offset: 0`), the comparison window MUST be truncated to the first
   `days_elapsed` days of the previous period, so the delta compares like-for-like; when the
   previous period is shorter than `days_elapsed` (e.g. comparing 30 days elapsed of March
   against February), the comparison clamps to the full previous period. Past windows compare
   full-to-full.
9. A window entirely before Ada's date of birth (or before retained data) MUST return a valid
   response with zeroed buckets and correct labels/metadata — never an error. `min_offset` is
   the offset of the period containing `ada_profile.birth_at`; when no profile exists the field
   is omitted.
10. Sleep attribution: sessions count toward the calendar day they **start** on. Wakeup
    counting groups night sessions into nights by a noon cut (a night session starting before
    12:00 local belongs to the previous calendar day) and each wakeup is attributed to the
    night's start day, so an early-morning wakeup counts toward the night it belongs to.
11. Promotion to an HA WebSocket command (alternative C) SHOULD happen if the result-sensor
    round-trip proves laggy or racy in practice; the bucket/response contract above MUST be
    reusable by that path unchanged.

## Consequences

### Positive

* Zero new transport, auth, or CORS surface; preserves the pure-HA-view-layer model.
* Computes only the requested slice rather than an ever-growing cube.
* The echoed `request_id` + `generated_at` lets the dashboard discard stale responses, resolving
  the single-result-sensor race noted in #82.

### Negative

* An async round-trip per parameter change (fire → compute → sensor update → re-read).
* A single shared result sensor is an awkward request/response medium; correctness depends on the
  `request_id` echo being honored by the consumer.

### Negative (2026-07-05 amendment)

* Trends and the Today view now use different day boundaries (calendar midnight vs. bedtime
  rollover); evening data can appear in trends "today" after the Today view has rolled. Accepted
  and bounded (19:00–00:00).
* Truncated `prevGrand` makes the delta's meaning depend on `offset` and the day of the period —
  correct, but subtler than the old full-window comparison.

### Neutral

* Introduces a query-style event class (`ada.trends.query`) into Ada's otherwise write-only event
  vocabulary.
* Establishes `sensor.ada_trends` as a latest-result channel rather than a state cache.
* Month views have a variable bucket count (4–6); the client renders whatever arrives.
