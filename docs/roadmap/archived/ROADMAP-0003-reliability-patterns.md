# Reliability Patterns

* **Status:** Complete
* **Date:** 2026-02-18
* **Project:** ruby-core
* **Related ADRs:** ADR-0022, ADR-0024, ADR-0025
* **Linked Plan:** none

---

**Goal:** Implement the core reliability patterns for message handling before business logic is written.

---

## Efforts

1. Implement the DLQ strategy (ADR-0022).
2. Refactor consumers to be pull-based with flow control (ADR-0024).
3. Create the shared idempotency checker library (ADR-0025).
4. Codify default tuning values (`MaxAckPending`, TTLs, etc.) in a central config.

---

## Done When

A poison-pill message routes to the DLQ, consumers apply backpressure under load, and a duplicate event is correctly discarded by the idempotency store.

---

## Acceptance Criteria

* `[X]` A "poison pill" message is correctly moved to the DLQ.
* `[X]` A consumer correctly applies backpressure under load.
* `[X]` An idempotency check correctly discards a duplicate event.
