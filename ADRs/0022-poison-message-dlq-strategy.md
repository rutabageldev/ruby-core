# ADR-0022 - Adopt a Dead-Letter Queue (DLQ) Strategy for Failed Messages

*   **Status:** Accepted
*   **Date:** 2026-02-01

## Context

In a message-driven system with "at-least-once" delivery, a "poison message" (e.g., malformed data) or a persistent bug can cause a consumer to fail processing repeatedly. Without a mitigation strategy, this leads to an infinite retry loop, blocking the consumer from processing valid messages and effectively causing a service outage. We need a strategy that unblocks the consumer while preserving the failed message for analysis.

## Decision

We will adopt a **"Retry with Backoff, then DLQ"** strategy for all critical JetStream consumers, leveraging built-in NATS features.

1.  **Consumer Retry Policy:** All critical consumers **MUST** be configured with a retry policy. The project default policy **SHOULD** be:
    *   `max_deliver`: 5 attempts.
    *   `backoff`: An exponential backoff policy (e.g., starting at 1 second).
    Individual consumers may tune these values if a different behavior is required.

2.  **Dead-Letter Queue (DLQ) Redirection:** If a message fails processing `max_deliver` times, the JetStream consumer configuration **MUST** instruct the server to automatically republish the message to a dedicated DLQ subject.

3.  **DLQ Naming Convention:** The DLQ subject name **MUST** follow a standard convention. This convention will be formally defined in the future **ADR for Subject Naming Convention**, but will be based on the pattern `dlq.<stream_name>.<consumer_name>`. This ensures consistency for ACLs and monitoring.

4.  **DLQ Archival and Monitoring:**
    *   A single, wildcard-based JetStream stream **MUST** be created to capture and persist all messages published to `dlq.*` subjects.
    *   This central DLQ stream **MUST** be monitored, and alerts **MUST** be generated if its message count grows, indicating a persistent processing issue that requires operator intervention.

5.  **Reprocessing Policy:** For V0, reprocessing a message from the DLQ after a bug fix is a manual operational task. The process will be to use the `nats` CLI or a script to copy the message from the DLQ stream and republish it to its original subject for a final delivery attempt. Automated reprocessing is out of scope for V0.

## Consequences

### Positive Consequences

*   **Resilience:** Prevents a single poison message from blocking a consumer and causing a service outage.
*   **Debuggability:** Safely quarantines every failed message in its original, unmodified form, which is invaluable for debugging and root cause analysis.
*   **Leverages Core Features:** Uses the mature, built-in capabilities of NATS JetStream, keeping application logic simple (consumers just need to `-NAK`).
*   **Clear Operational Path:** Defines a clear, if manual, process for handling and reprocessing failed messages.

### Negative Consequences

*   **Configuration Overhead:** Adds configuration complexity to both JetStream streams (to define the `republish` policy) and consumers.
*   **Requires Active Monitoring:** An unmonitored DLQ can hide systemic problems. The effectiveness of this strategy is dependent on having alerts on the DLQ stream size.

### Neutral Consequences

*   This decision formalizes the operational process for handling and analyzing message processing failures within the system.
