# ADR-0023 - Enforce Single-Writer State via NATS ACLs

* **Status:** Accepted
* **Date:** 2026-02-01

## Context

ADR-0002 established the "single-writer" model as a convention to prevent data corruption in our NATS KV store. However, relying on developer discipline alone is fragile. This ADR moves from a convention to a programmatic guardrail by using our existing NATS security infrastructure to enforce this critical data integrity policy.

## Decision

The single-writer model for NATS KV state **MUST** be enforced at the infrastructure level using **per-service NATS Access Control Lists (ACLs)**.

1. **Enforcement Mechanism:** The ACL `permissions` block for each service's NKEY identity (per ADR-0017) **MUST** be configured to only allow `publish` access (which governs KV writes) to the specific NATS subjects corresponding to its designated KV keyspace(s).

2. **Permissions Policy (Least Privilege):**
    * **Write Access:** `publish` permissions on KV-related subjects **MUST** be denied by default and only granted to the single designated owner service for its specific keyspace.
    * **Read Access:** `subscribe` permissions on KV-related subjects **MUST** also be denied by default. A service needing to read data from a keyspace it does not own **MUST** be explicitly granted this permission in its ACLs.

3. **Naming Convention Dependency:** This enforcement is critically dependent on a predictable naming scheme for KV buckets and their underlying subjects. The ACL rules **MUST** be based on the patterns to be defined in the future **ADR for Subject Naming Convention**.

4. **Error Handling Expectation:** An attempt by a service to write to a keyspace it does not own will result in a `permission violation` error from the NATS server. This error **MUST** be logged by the service at a `CRITICAL` or `ERROR` level. It **SHOULD** also trigger the publication of an event to the `audit.events` stream (per ADR-0019) to flag the security policy violation.

## Consequences

### Positive Consequences

* **Data Integrity:** Programmatically prevents race conditions and data corruption by enforcing the single-writer model at the broker level, making violations impossible.
* **Enhanced Security:** Extends our "principle of least privilege" security posture to cover state access, preventing services from reading or writing state they are not explicitly authorized for.
* **Improved Reliability:** Moves from a fallible human convention to a reliable, automated infrastructure guarantee.
* **Clear Audit Trail:** Provides a clear, auditable signal when a service attempts to violate the data ownership policy.

### Negative Consequences

* **Increased ACL Complexity:** Significantly increases the detail required in the NATS server ACL configuration, which must be carefully managed. This is an accepted trade-off for the data integrity guarantee.

### Neutral Consequences

* This decision creates a strong dependency between our data integrity model and our NATS security configuration and subject naming scheme.
