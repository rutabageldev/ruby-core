# ADR-0032 - Ada trends acquisition

* **Status:** Proposed
* **Date:** 2026-06-18
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

## Decision

1. The dashboard mints an opaque `request_id` per query and fires
   `ada.trends.query {metric, view, period, request_id}` through the existing `fire_ada_event`
   script. `period` MUST be one of `week`, `month`, `year`.
2. ruby-core computes the buckets and publishes the result to `sensor.ada_trends`. The payload
   MUST echo `request_id`, the resolved `{metric, view, period}`, and a `generated_at` timestamp,
   so the dashboard renders only the response matching its latest request and ignores stale ones.
3. The response MUST mirror the frontend `TrendData` shape:
   `{ request_id, metric, view, period, generated_at, buckets: [{ segs, total, label }],
   totals, grand, prevGrand }`, where `prevGrand` is the grand total of the same view/period
   shifted back one full window.
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

5. Buckets MUST be boundary-aligned consistently with `sensor.ada_today_boundary` (bedtime
   rollover), with counts: week = 7 × 1-day, month = 4 × 7-day, year = 12 × ~30-day.
6. Promotion to an HA WebSocket command (alternative C) SHOULD happen if the result-sensor
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

### Neutral

* Introduces a query-style event class (`ada.trends.query`) into Ada's otherwise write-only event
  vocabulary.
* Establishes `sensor.ada_trends` as a latest-result channel rather than a state cache.
