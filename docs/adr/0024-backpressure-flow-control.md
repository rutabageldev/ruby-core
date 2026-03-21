# ADR-0024 - Adopt Pull Consumers for Backpressure and Flow Control

* **Status:** Accepted
* **Date:** 2026-02-01

## Context

"Event storms" (large bursts of messages) can overwhelm a consumer service, leading to resource exhaustion, high latency, and potential crashes. A robust system requires a backpressure mechanism, allowing consumers to control the rate of message delivery from the server to match their current processing capacity.

## Decision

We will adopt **pull-based JetStream consumers** with explicit flow control settings as the default pattern for all critical services.

1. **Scope:** This pull-consumer policy **MUST** be applied to all consumers that perform business-critical, resource-intensive, or I/O-bound work (e.g., the `engine` service). Simpler, non-critical consumers (e.g., a passive logger) **MAY** use push-based subscriptions for ease of implementation.

2. **Consumer Configuration & Defaults:** All pull consumers **MUST** be configured with `MaxAckPending`. The following conservative project-wide defaults **SHOULD** be used for V0 unless a specific consumer has a documented reason to deviate:
    * `MaxAckPending`: 128 messages
    * `AckWait`: 30 seconds. This value **SHOULD** be tuned on a per-consumer basis to be longer than the expected worst-case processing time for a single message.

3. **Application Logic & Concurrency Model:**
    * Consumers **MUST** use a `Fetch(batch_size)` loop to pull messages. A default `batch_size` of 20 **SHOULD** be used.
    * To process messages concurrently, consumers **SHOULD** implement a **fixed-size worker pool**.
    * The `Fetch` `batch_size` **SHOULD NOT** exceed the worker pool size. This ensures that fetched messages can be immediately dispatched to a worker without causing additional unbounded queueing within the application's memory.

## Consequences

### Positive Consequences

* **Resilience:** Provides true, adaptive backpressure, preventing event storms from overwhelming services and causing cascading failures.
* **Prevents Resource Exhaustion:** Gives developers explicit, server-enforced control over consumer concurrency via `MaxAckPending`.
* **Efficiency:** Using the pull-based `Fetch` model is the most efficient way to consume messages at high performance, as it prevents unnecessary client-side buffering.
* **Pragmatic Scope:** Allows for simpler push consumers for non-critical telemetry, applying the complexity only where it provides value.

### Negative Consequences

* **Increased Application Complexity:** The application logic for a pull consumer is more complex than for a basic push consumer, as it requires managing a `Fetch` loop and a worker pool. This is a necessary trade-off for resilience.

### Neutral Consequences

* This decision formalizes a specific, resilient consumer implementation pattern as the standard for all critical services in the project.
