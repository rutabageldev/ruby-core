# ADR-0009 - Gateway Responsibilities: Failure Isolation and Lean Projection

*   **Status:** Accepted
*   **Date:** 2026-02-01

## Context

The `gateway` service has two primary responsibilities: managing communication with Home Assistant (ingress and egress) and translating HA events into our internal model. This presents two risks: first, that a failure in one communication channel (e.g., ingress) could disable the other (e.g., egress); second, that the translation logic could become overly complex and brittle. This ADR defines our V0 strategy for mitigating both risks.

## Decision

We will adopt a two-part policy for the `gateway`'s responsibilities: implementing internal circuit breakers for failure isolation and adopting a "lean projection" strategy for event normalization.

1.  **Failure Isolation Strategy:**
    *   The `gateway` **MUST** be deployed as a single service for V0.
    *   The ingress (WebSocket) and egress (REST) communication components **MUST** be wrapped in independent circuit breaker patterns.
    *   **Boundary Definition:** This pattern is intended to handle *transient, recoverable faults* (e.g., temporary network errors). Fatal, process-level failures (e.g., panics) are not handled by the circuit breaker; their resolution is delegated to the container supervisor (e.g., Docker's `restart: unless-stopped` policy).

2.  **Normalization Strategy (Lean Projection):**
    *   The `gateway` **MUST** perform a "lean projection" of incoming Home Assistant events. This involves dropping all entity attributes except those explicitly required by the system.
    *   **Passlist Source of Truth:** The list of required attributes (the "passlist") **MUST** be dynamically derived from the loaded automation rule configurations (per ADR-0006). This ensures that the gateway only ever passes through data that is actively required by a configured automation.
    *   **Schema Versioning:** The schema of the projected `data` payload within the resulting CloudEvent **MUST** be versioned. Any change to the projection that is not backward-compatible (e.g., removing or renaming a field) **MUST** result in a new version for that event type, allowing downstream consumers to adapt gracefully.

## Consequences

### Positive Consequences

*   **Pragmatic Resilience:** Provides good resilience against common transient faults while maintaining the operational simplicity of a single service for V0.
*   **Clear Failure Model:** Explicitly defines the different responsibilities for handling transient application faults (circuit breaker) versus fatal process faults (container supervisor).
*   **Maintainable Decoupling:** Insulates internal services from the "noise" and churn of the raw HA event model without creating a brittle, complex mapping layer.
*   **Automatic Configuration:** Deriving the passlist from automation rules prevents configuration drift and ensures the gateway is always aligned with the system's needs.
*   **Stable Evolution:** Versioning the projected event schema prevents silent breaking changes for downstream consumers.

### Negative Consequences

*   **No Total Isolation:** Does not protect egress functionality from a fatal, process-level crash on the ingress side. This is an accepted trade-off for V0 simplicity.
*   **Increased Startup Complexity:** The gateway has a more complex initialization sequence, as it must derive its passlist from the rule configurations.

### Neutral Consequences

*   This decision formalizes the `gateway`'s role as a "filter and forward" agent rather than a complex "translator" of business logic.
