# ADR-0001 - Use NATS JetStream for Durable Messaging

*   **Status:** Accepted
*   **Date:** 2026-02-01

## Context

The Ruby Core architecture is event-driven, with services communicating via a message bus. A core non-functional requirement is reliability. For many automations, if a consumer service is down or disconnected, it must be able to process events that were published during that outage upon its recovery. Simple, "at-most-once" delivery is insufficient for these use cases.

We considered two primary options:
1.  **NATS Core:** Simple, extremely fast pub/sub that offers "at-most-once" delivery. Event loss is possible if a subscriber is not active.
2.  **NATS JetStream:** An extension of NATS that provides durable, "at-least-once" delivery through streams and acknowledged consumers.

## Decision

We will adopt **NATS JetStream** as the primary messaging backbone for Ruby Core.

1.  **"At-Least-Once" by Default:** All event streams that can trigger state transitions, commands, or other critical logic **MUST** be configured as durable JetStream streams. Consumers of these streams **MUST** acknowledge messages to ensure at-least-once delivery. **Critical events are defined as those that, if missed, would lead to an incorrect system state, incorrect command execution, or compromise safety-relevant automations.**
2.  **Allowing "At-Most-Once" for Efficiency:** For high-volume, ephemeral telemetry or other non-critical data where loss is acceptable, services **MAY** use standard NATS subjects (NATS Core pub/sub) for efficiency. This must be a deliberate and documented choice for that specific data type.
3.  **Replay Policy Boundary:** The use of JetStream's replay functionality is primarily for recovery and debugging purposes (e.g., rebuilding a service's state). It is explicitly **not intended for re-driving side effects** on other services without ensuring strict idempotency in the consuming services.

## Consequences

### Positive Consequences

*   **Reliability:** The system becomes resilient to transient service outages. Events are not lost, ensuring automations are not silently dropped.
*   **Foundation for State Management:** Provides the durable streams necessary for other architectural patterns, including the NATS KV Store (see ADR-0002) and potential event sourcing in the future.
*   **Replayability:** JetStream streams can be replayed for debugging or to rebuild the state of new service instances.

### Negative Consequences

*   **Increased Complexity:** Configuring and managing JetStream streams, consumer acknowledgements, and delivery policies is more complex than standard NATS pub/sub.
*   **Minor Latency Overhead:** The requirement for storage and acknowledgements introduces a slight latency increase compared to fire-and-forget messaging.

### Neutral Consequences

*   This decision commits the project more deeply to the NATS ecosystem, making NATS a critical piece of core infrastructure.
