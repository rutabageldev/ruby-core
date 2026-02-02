# ADR-0025 - Idempotency Tracking using a Cached KV Store

*   **Status:** Accepted
*   **Date:** 2026-02-01

## Context

ADR-0003 established that consumers are responsible for ensuring idempotency. This requires a storage mechanism for recently processed event IDs that is both durable (survives restarts) and highly performant (to not slow down message processing). A simple in-memory store is not durable, and using only a network-based KV store for every check can be a performance bottleneck.

## Decision

We will adopt a hybrid **"In-Memory Cache with a NATS KV Backend"** strategy for tracking processed event IDs.

1.  **Architecture:** Consumers **MUST** use a two-layer check for idempotency:
    a. First, check a local, **in-memory cache** for the event ID. If found, the message is a duplicate and is discarded.
    b. If not in the cache, check the durable **NATS KV store**. If found, the message is a duplicate; it should be added to the in-memory cache and then discarded.
    c. If the ID is in neither store, the event is new. After it is successfully processed, its ID must be written to both the in-memory cache and the NATS KV store.

2.  **TTL Policy:** When an event ID is persisted to the NATS KV store, it **MUST** be written with a Time-to-Live (TTL). The default TTL **SHOULD** be **24 hours**. This is sufficient to handle most redelivery scenarios while ensuring the data store does not grow indefinitely.

3.  **Cache Bounds:** The in-memory cache **MUST** be bounded to prevent memory exhaustion. It **SHOULD** implement a size limit with a simple LRU (Least Recently Used) eviction policy or a time-based eviction policy aligned with the KV store's TTL.

4.  **Residual Risk (Side-effect Window):** We acknowledge a small race condition window: a crash could occur after an external side-effect is performed but before the event ID is durably persisted to the KV store. This residual risk is accepted for V0. To mitigate it, external side-effects **SHOULD** be designed to be idempotent themselves where possible (e.g., using `UPSERT` database logic instead of a simple `INSERT`).

## Consequences

### Positive Consequences

*   **Performant and Durable:** Provides the high performance of an in-memory check for the common path, while guaranteeing durability across service restarts via the NATS KV backend.
*   **Reduces Network Load:** Significantly reduces read load on the NATS server, as most duplicate checks are handled locally.
*   **Prevents Unbounded Growth:** The TTL policy on the KV store and the bounded nature of the in-memory cache prevent resource leaks.

### Negative Consequences

*   **Increased Application Complexity:** The consumer's idempotency-checking logic is more complex than a simple KV lookup. This complexity should be abstracted into a shared internal library.
*   **Acknowledged Risk Window:** There is a small, explicit window of risk for non-idempotent side effects if a crash occurs at a precise moment.

### Neutral Consequences

*   This decision formalizes the implementation pattern for the idempotency guarantee required by ADR-0003.
