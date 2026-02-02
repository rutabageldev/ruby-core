# ADR-0021 - Adopt a Resilient Single-Node NATS Deployment

* **Status:** Accepted
* **Date:** 2026-02-01

## Context

The NATS message bus is a critical component of the system. While enterprise deployments often require multi-node clusters for high availability, the context for this project is a single-host deployment for personal use. This prioritizes operational simplicity, resource efficiency, and robust data durability over zero-downtime failover. A multi-node cluster on a single host provides a false sense of security and unnecessary complexity.

## Decision

For the production environment, we will deploy NATS as a **resilient single node**, with the following operational requirements:

1. **Container Configuration:** The NATS server **MUST** run in a single container with a `restart: unless-stopped` policy defined in its service configuration. This ensures the service automatically recovers from process crashes or host reboots.

2. **Storage Configuration:** The container's JetStream storage directory **MUST** be mounted to a reliable, persistent path on the host node.

3. **Backup and Recovery Policy:**
    * The host path used for JetStream storage **MUST** be included in a regular, automated backup schedule.
    * A procedure for restoring the NATS state from this backup **MUST** be documented and should be tested periodically to ensure its validity.

4. **Physical SPOF Mitigation:** The host's physical storage is the primary single point of failure in this design. Therefore, the underlying disk(s) **SHOULD** be monitored for health issues (e.g., via SMART diagnostics or ZFS scrubs) to preemptively detect failures.

5. **Future Re-evaluation:** This decision is specific to a single-host deployment. If the project's production topology ever expands to multiple physical hosts, the decision to implement a multi-node NATS cluster **MUST** be revisited in a new ADR.

## Consequences

### Positive Consequences

* **Simplicity:** Provides a simple, low-overhead, and maintainable NATS deployment that is ideal for a personal-use case.
* **Resilience:** Ensures automatic recovery from the most common failure modes (process crashes and host reboots).
* **Durability:** The explicit backup and monitoring requirements provide a strong guarantee of data durability and a clear path to disaster recovery.
* **Fit-for-Purpose:** Avoids the unnecessary complexity and resource consumption of a multi-node cluster on a single host.

### Negative Consequences

* **No High Availability:** Does not provide true high availability; there will be a short period of downtime (typically seconds to minutes) during a service restart or host reboot. This is an accepted trade-off.
* **Hardware Dependency:** The system's reliability is ultimately dependent on the reliability of the single host's hardware and its maintenance schedule.

### Neutral Consequences

* This decision formalizes a deployment strategy that is pragmatic and appropriate for the stated personal-use context, rather than defaulting to a more complex enterprise pattern.
