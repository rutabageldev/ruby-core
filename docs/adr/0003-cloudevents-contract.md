# ADR-0003 - Adopt CloudEvents with Mandatory Traceability and Consumer-Side Idempotency

* **Status:** Accepted
* **Date:** 2026-02-01

## Context

With "at-least-once" delivery guaranteed by NATS JetStream (see ADR-0001), we need a clear event contract to ensure system reliability and debuggability. This contract must address:

1. **Standard Structure:** A consistent format for all events.
2. **Idempotency:** A mechanism to prevent duplicate side effects when events are redelivered.
3. **Traceability:** The ability to follow the flow of events through complex distributed workflows.

We considered a minimalist custom JSON, a rich custom envelope, and the CloudEvents standard. The CloudEvents standard offered a pre-vetted solution, but specific policies regarding idempotency scope, event ID semantics, and traceability needed explicit definition to be truly enforceable.

## Decision

We will adopt the **CloudEvents specification** as the standard event contract for all events in Ruby Core, governed by the following explicit commitments:

1. **CloudEvents Specification:** All events **MUST** conform to the CloudEvents specification (currently v1.0).
2. **Globally Unique Event IDs:** The CloudEvents `id` attribute **MUST** be a globally unique identifier for a specific event *emission*. This means each time an event is emitted, it receives a new, unique `id`, even if its payload content is identical to a previously emitted event. The `id` represents the unique instance of that event's occurrence. When an event is replayed (e.g., by JetStream), it retains its original `id`.
3. **Consumer-Side Idempotency:** Services performing non-idempotent side effects **MUST** implement idempotency logic based on the CloudEvents `id` attribute. The responsibility for preventing duplicate side effects lies with the consumer, not the publisher.
4. **Mandatory Traceability Attributes:** All events **MUST** include `correlationid` and `causationid` as CloudEvents extension attributes.
    * The `correlationid` **MUST** be preserved across an entire workflow, ideally established at the start of a logical chain. For root events (those initiating a new workflow), the `correlationid` **MUST** be newly generated.
    * The `causationid` **MUST** refer to the `id` of the immediate parent event that directly caused the current event. For root events, the `causationid` may be set to the event's own `id` or a defined null/empty value (e.g., an empty string), based on project-specific convention for indicating no prior cause.

## Consequences

### Positive Consequences

* **Standardization:** Leverages a well-defined industry standard, reducing internal design effort and promoting interoperability.
* **Tooling Availability:** Benefits from existing libraries and tools for CloudEvents across multiple languages (including Go).
* **Reliability:** Explicitly defines how to achieve idempotency, preventing duplicate side effects in an "at-least-once" delivery system.
* **Debuggability:** Mandatory `correlationid` and `causationid` provide a robust framework for tracing complex distributed workflows, crucial for debugging.

### Negative Consequences

* **Minor Learning Curve:** Developers need to familiarize themselves with the CloudEvents specification and the project's specific conventions for extension attributes.
* **Slightly More Verbose:** The CloudEvents envelope can be slightly larger than a bare custom JSON, but this overhead is negligible in practice.

### Neutral Consequences

* This decision aligns the project with a broader cloud-native ecosystem, which could offer future benefits for integration.
