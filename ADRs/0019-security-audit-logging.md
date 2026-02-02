# ADR-0019 - Decoupled Security Audit Trail via NATS

*   **Status:** Accepted
*   **Date:** 2026-02-01

## Context

For security and forensic purposes, we require a high-integrity, tamper-resistant audit trail of all critical actions taken by the system (e.g., executing commands that affect the physical world). Standard application logs are insufficient as they are not immutable and mix security events with diagnostic data. A dedicated, decoupled, and reliable mechanism is needed.

## Decision

We will implement a security audit trail using a dedicated, durable **NATS JetStream stream** as a decoupled buffer, with a separate "audit sink" service for long-term archival.

1.  **Audit Stream:** A dedicated JetStream stream named `audit.events` **MUST** be created. This stream will be configured for high durability and a long retention policy.

2.  **Audit Event Schema:** All audit records **MUST** be published as CloudEvents to the `audit.events` stream with a dedicated type (e.g., `dev.rubocore.audit.v1`). The event's `data` payload **MUST** contain, at a minimum:
    *   The full `correlationid` and `causationid` from the event chain.
    *   The authenticated identity of the actor performing the action (e.g., the service's NKEY public key).
    *   The full details of the command or decision being audited (the "intent").
    *   The outcome of the action (e.g., "success," "failure," "rejected").

3.  **Producers and Consumers:**
    *   Services performing auditable actions **MUST** publish a corresponding audit event to this stream.
    *   A dedicated "audit sink" service will be the sole consumer of this stream, responsible for archiving events to secure, long-term storage (e.g., a write-once object store or a SIEM).

4.  **Retention and Backpressure Policy:**
    *   **Stream Retention:** The `audit.events` stream **MUST** be configured with a retention policy sufficient to survive a prolonged archival service outage (e.g., a minimum of 72 hours), after which messages may be discarded by the server.
    *   **Backpressure:** The act of auditing is decoupled from the primary action. Publishing to the `audit.events` stream **MUST NOT** block the execution of the primary action. If the NATS stream is unavailable or full, the primary action should still complete successfully, and the failure to publish the audit event must be logged and generate a high-priority alert.

## Consequences

### Positive Consequences

*   **High Integrity:** Creates a traceable, attributable, and tamper-resistant audit trail with a clear path to immutable long-term storage.
*   **Decoupled Architecture:** Core services are not coupled to the final archival backend. They only need to know how to publish an event to NATS.
*   **Resilience:** The backpressure policy ensures that failures or slowdowns in the audit subsystem do not impact the primary functionality of the core system.
*   **Rich Context:** The defined schema ensures all audit events are self-contained and useful for forensic analysis.

### Negative Consequences

*   **New Component:** Requires the development and maintenance of a new, single-purpose `audit sink` service.
*   **Infrastructure Overhead:** Adds storage and I/O overhead to the NATS cluster to persist the audit stream.

### Neutral Consequences

*   This decision formalizes the concept of an "auditable action" and the requirement for services to explicitly publish audit events.
