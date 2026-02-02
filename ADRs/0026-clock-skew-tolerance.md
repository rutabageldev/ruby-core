# ADR-0026 - Manage Clock Skew with Infrastructure Sync and Application Tolerance

*   **Status:** Accepted
*   **Date:** 2026-02-01

## Context

In a distributed system, clock skew—the small, inevitable difference between clocks on different servers—can cause non-deterministic failures in time-sensitive logic. For example, our command TTL policy (ADR-0005) could fail if the consumer's clock is out of sync with the producer's. Relying solely on infrastructure-level time synchronization (like NTP) is insufficient, as it minimizes skew but does not eliminate it.

## Decision

We will adopt a two-layer, defense-in-depth strategy to mitigate issues arising from clock skew.

1.  **Infrastructure Requirement:** All host machines running Ruby Core services **MUST** run a time synchronization daemon (e.g., NTP, chrony) configured to synchronize with a reliable time source. This is a mandatory operational baseline.

2.  **Application Tolerance Policy:** All time-sensitive business logic **MUST** incorporate a configurable tolerance window to gracefully handle residual clock skew and network latency.
    *   **Default Tolerance:** The default clock skew tolerance **SHOULD** be **1000ms**. This value **MUST** be configurable on a per-environment basis.
    *   **Scope of Application:** This tolerance window policy **MUST** be applied to, but is not limited to, the following critical checks:
        *   Validation of the `valid_until` field on incoming commands.
        *   Calculations for time-based automations, such as debounce or delay windows.
        *   Timestamp comparisons performed during state drift reconciliation.

## Consequences

### Positive Consequences

*   **Robustness:** Provides a strong, defense-in-depth strategy against flaky, hard-to-debug, time-related bugs.
*   **Predictable Behavior:** Makes the system's time-sensitive logic predictable and reliable by explicitly accounting for the physical realities of distributed clocks.
*   **Configurability:** Makes the system's tolerance for timing inaccuracies an explicit and configurable parameter.

### Negative Consequences

*   **Operational Dependency:** The system's correctness has a hard dependency on the proper configuration and operation of NTP on all host machines.
*   **Developer Discipline:** Requires developers to consistently apply the tolerance window in all time-sensitive application code. This can be mitigated by creating and using shared helper functions.

### Neutral Consequences

*   This decision formalizes both the operational requirement for time synchronization and the application-level pattern for handling time comparisons.
