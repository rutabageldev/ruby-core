# ADR-0025 - Idempotency Tracking using a Cached KV Store

* **Status:** Accepted (amended 2026-06-28, see Amendments)
* **Date:** 2026-02-01

## Context

ADR-0003 established that consumers are responsible for ensuring idempotency. This requires a storage mechanism for recently processed event IDs that is both durable (survives restarts) and highly performant (to not slow down message processing). A simple in-memory store is not durable, and using only a network-based KV store for every check can be a performance bottleneck.

## Decision

We will adopt a hybrid **"In-Memory Cache with a NATS KV Backend"** strategy for tracking processed event IDs.

1. **Architecture:** Consumers **MUST** use a two-layer check for idempotency:
    a. First, check a local, **in-memory cache** for the event ID. If found, the message is a duplicate and is discarded.
    b. If not in the cache, check the durable **NATS KV store**. If found, the message is a duplicate; it should be added to the in-memory cache and then discarded.
    c. If the ID is in neither store, the event is new. After it is successfully processed, its ID must be written to both the in-memory cache and the NATS KV store.

2. **TTL Policy:** When an event ID is persisted to the NATS KV store, it **MUST** be written with a Time-to-Live (TTL). The TTL **MUST** be sized to the **maximum redelivery window** — `MaxDeliver × AckWait + Σ BackOff` (currently ≈ 165s) — plus a safety margin, **not** to "how long we might plausibly see a duplicate." Dedup only needs to outlive the window in which JetStream can still redeliver a message before DLQ; retaining IDs longer serves no correctness purpose and bloats the bucket. The default is **30 minutes** (amended 2026-06-28; was 24h). See the Amendments section.

3. **Cache Bounds:** The in-memory cache **MUST** be bounded to prevent memory exhaustion. It **SHOULD** implement a size limit with a simple LRU (Least Recently Used) eviction policy or a time-based eviction policy aligned with the KV store's TTL.

4. **Residual Risk (Side-effect Window):** We acknowledge a race condition window: a crash — or an `AckWait` expiry while a worker is still inside a slow side-effect — can cause a redelivery to be processed before (or concurrently with) the first attempt's KV mark. The KV dedup store **cannot** close this window. Therefore external side-effects of a **write-through to an external system of record MUST be idempotent themselves**, not merely SHOULD: use `UPSERT` over `INSERT`, deterministic/client-assigned resource IDs so a duplicate create collapses at the sink, and ensure-absent (not blind-delete) semantics. The dedup store is a performance optimization (skip obvious replays cheaply); it is not the correctness guarantee.

## Consequences

### Positive Consequences

* **Performant and Durable:** Provides the high performance of an in-memory check for the common path, while guaranteeing durability across service restarts via the NATS KV backend.
* **Reduces Network Load:** Significantly reduces read load on the NATS server, as most duplicate checks are handled locally.
* **Prevents Unbounded Growth:** The TTL policy on the KV store and the bounded nature of the in-memory cache prevent resource leaks.

### Negative Consequences

* **Increased Application Complexity:** The consumer's idempotency-checking logic is more complex than a simple KV lookup. This complexity should be abstracted into a shared internal library.
* **Acknowledged Risk Window:** There is a small, explicit window of risk for non-idempotent side effects if a crash occurs at a precise moment.

### Neutral Consequences

* This decision formalizes the implementation pattern for the idempotency guarantee required by ADR-0003.

## Amendments

### 2026-06-28 — TTL sizing + sink-idempotency requirement (PLAN-0034)

Live validation of the calendar write-through (ADR-0042) exposed both halves of the
residual risk in practice. The shared `idempotency` bucket had grown to ~97k entries
because the engine consumer marks **every** processed event, including the high-volume
`state_changed` firehose, at the original 24h TTL. The bloat slowed KV operations until
marks timed out; combined with an `AckWait` expiry during a slow Google `Insert`, a
redelivery was processed concurrently and produced a duplicate Google event — the create
side-effect was not idempotent. A redelivered delete likewise hit a Google 410 and DLQ'd.

Two changes follow:

* **TTL** lowered 24h → 30m (Decision §2), sized to the redelivery window. This is bloat
  hygiene; it narrows but does not close the race.
* **Sink idempotency** is now a **MUST** for write-through sinks (Decision §4). The
  calendar create derives a deterministic Google event id so a duplicate insert returns
  409 (converge, don't re-insert); delete treats the local mirror as the source of truth
  for "already applied" and treats a 410/404 as the satisfied postcondition.

Operational note: a NATS KV bucket's TTL is fixed at creation — lowering the default has
no effect on a live bucket until it is deleted and recreated. `CreateOrBindKVBucket` now
WARNs on a TTL mismatch at startup. Do **not** add a tight `MaxBytes` cap to these buckets:
the KV backing stream is `discard=new`, so a byte cap makes `Put` fail (and that failure
is non-fatal/swallowed), silently disabling dedup. Short TTL is the correct bound.
