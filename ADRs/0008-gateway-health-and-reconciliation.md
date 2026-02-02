# ADR-0008 - Gateway Health Signaling and Targeted Drift Reconciliation

* **Status:** Accepted
* **Date:** 2026-02-01

## Context

The `gateway` service's connection to Home Assistant (HA) is a critical single point of failure. A disconnection can lead to two problems: other services being unaware that the data stream is stale (liveness), and our internal state becoming out-of-sync with HA's ground truth due to missed events (state drift). We need a strategy that solves both problems without overloading the system. A simple heartbeat is insufficient as it doesn't address drift, and a full state reconciliation on reconnect carries an unacceptable risk of creating an event storm.

## Decision

We will implement a two-part strategy: a regular health heartbeat for liveness, and a targeted drift reconciliation process triggered on reconnection.

1. **Health Heartbeat:** The `gateway` service **MUST** publish a `gateway.health` event to NATS at a regular, low frequency (e.g., every 15 seconds), indicating its connection status to Home Assistant.

2. **Targeted Reconciliation Trigger:** Upon re-establishing a connection to Home Assistant, the `gateway` **MUST** trigger a targeted reconciliation process for critical entities.

3. **Critical Entity Source:** The set of "critical entities" for reconciliation **MUST** be dynamically provided to the `Gateway` from the configuration derived from the automation rules (per ADR-0006). The `engine` service (or a configuration management component) is responsible for compiling this list from the `triggers` and `conditions` of all loaded rules and making it accessible to the `Gateway` (e.g., via a specific NATS KV key or dedicated config file for the `Gateway`).

4. **Reconciliation Logic:** For each critical entity, the `gateway` will fetch its latest state from HA. It will then compare the entity's timestamp against the timestamp of our internally stored state.
    * **Time Authority:** This comparison **MUST** use a consistent, normalized time basis. Both the `last_changed` time from the HA state and the timestamp of the last CloudEvent recorded for that entity in our KV store (which conforms to CloudEvents `time` attribute) **MUST be treated as absolute UTC and normalized prior to comparison** to avoid timezone parsing ambiguity.
    * If the HA state is determined to be newer, a new event representing this updated state will be published to the event bus.

5. **Future Enhancement - Periodic Reconciliation:** A periodic, low-frequency reconciliation of critical entities (e.g., once every few hours) may be implemented in the future if persistent, non-reconnection-related drift is observed. This is not required for the V0 implementation.

## Consequences

### Positive Consequences

* **Solves Both Problems:** Provides a clear liveness signal to the system while also implementing a safe, efficient mechanism to correct state drift.
* **Avoids "Thundering Herd":** The targeted nature of the reconciliation prevents event storms that could destabilize the system.
* **Self-Maintaining Scope:** By deriving the critical entity list from the automation rules, the reconciliation process automatically adapts to changes in the system's logic without manual intervention.
* **Reliable Drift Detection:** Using a normalized time basis for comparison ensures that drift detection is accurate and consistent with our event contract.

### Negative Consequences

* **Inter-Service Dependency:** The `Gateway` now has a dependency on another service (likely the `engine`) to provide the critical entity list, which introduces a new failure mode.
* **Acceptable Drift for Non-Critical Entities:** Entities not referenced in any automation rule will not be reconciled if their state drifts. This is an acceptable and intentional trade-off.

### Neutral Consequences

* This decision establishes reconnection as the primary event for triggering state consistency checks.
