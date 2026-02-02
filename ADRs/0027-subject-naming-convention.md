# ADR-0027 - Adopt a Hierarchical NATS Subject Naming Convention

*   **Status:** Accepted
*   **Date:** 2026-02-01

## Context

A formal naming convention for NATS subjects is a critical prerequisite for system security, observability, and maintainability. It enables effective wildcard-based ACLs, simplifies monitoring and debugging with wildcard subscriptions, and provides a clear, self-documenting taxonomy for all messages flowing through the system.

## Decision

All NATS subjects **MUST** adhere to a consistent, hierarchical, dot-separated format.

1.  **Standard Structure:** The standard subject structure is:
    **`{source}.{class}.{type}[.{id}][.{action}]`**

2.  **Token Rules:**
    *   **Casing and Characters:** All tokens **MUST** use only lowercase alphanumeric characters and underscores (`a-z`, `0-9`, `_`).
    *   **Separator:** The dot (`.`) is reserved exclusively as a token separator and **MUST NOT** be used within a token.

3.  **Standard Tokens:**
    *   `{source}`: **Required.** The service or system originating the message (e.g., `ha`, `ruby_engine`, `ruby_gateway`).
    *   `{class}`: **Required.** A high-level message category. The standard set of classes **MUST** be one of: `events`, `commands`, `audit`, `metrics`, `logs`.
    *   `{type}`: **Required.** The entity type (e.g., `light`, `sensor`).
    *   `{id}`: **Optional.** A specific, sanitized instance of the entity.
    *   `{action}`: **Optional.** The specific action or state change.

4.  **Reserved Namespaces:**
    *   **Dead-Letter Queues (DLQ):** All DLQ subjects **MUST** use the reserved top-level namespace `dlq`. The required format is `dlq.<stream_name>.<consumer_name>` (per ADR-0022).
    *   **NATS Internals:** Subjects beginning with `$` are reserved for NATS internal protocols (e.g., JetStream API, KV access) and are not governed by this ADR.

5.  **Examples:**
    *   **Event:** `ha.events.light.living_room_lamp.state_changed`
    *   **Command:** `ruby_engine.commands.light.set`
    *   **Audit Event:** `ruby_gateway.audit.command_executed`

## Consequences

### Positive Consequences

*   **Enables Security:** The predictable, hierarchical structure is essential for defining effective, wildcard-based ACLs (per ADR-0017 and ADR-0023).
*   **Improves Operability:** Allows for powerful wildcard subscriptions for monitoring, debugging, and building system-wide views (e.g., subscribing to `*.*.metrics.*`).
*   **Reduces Ambiguity:** Creates a clear, self-documenting "ubiquitous language" for messaging, reducing cognitive load for developers.

### Negative Consequences

*   **Requires Discipline:** Developers must strictly adhere to the convention. Violations could lead to security gaps or broken monitoring. This can be mitigated with code review and potential future linting rules.

### Neutral Consequences

*   This decision formalizes the "API contract" of the message bus, providing a stable foundation for future development.
