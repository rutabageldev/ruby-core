# ADR-0007 - Decompose Engine into Logical Processors

* **Status:** Accepted
* **Date:** 2026-02-01

## Context

The `engine` service is responsible for executing all automation logic. To prevent this service from becoming a monolithic "big ball of mud" as new automation types (debounce, presence, scheduling) are added, a clear internal architecture is required. A monolith would be untestable and unmaintainable, while a full microservices-per-domain approach would introduce excessive operational overhead for V0. This ADR defines a pragmatic middle ground that provides logical separation without physical distribution.

## Decision

The `engine` service **MUST** be architected as a lightweight host for multiple, independent **Logical Processors**. Each processor is a self-contained unit of business logic for a specific domain.

1. **Processor Contract:** All Logical Processors **MUST** adhere to a standard Go interface. At a minimum, this interface will define methods for:
    * `Initialize(config, NatsConn)`: To receive its specific configuration and a NATS connection.
    * `Subscriptions() []string`: To declare the NATS subjects it needs to subscribe to.
    * `ProcessEvent(event)`: To handle an incoming event.
    * `Shutdown()`: To perform graceful cleanup.

2. **State Ownership:** Each processor is considered the sole owner and **single writer** for its durable state. It **MUST** store this state in a dedicated NATS KV keyspace, named for its domain (e.g., the `PresenceProcessor` owns the `presence:*` keyspace), adhering to the single-writer model defined in ADR-0002.

3. **Cross-Processor Communication:** Direct communication between processors via function calls is **forbidden**. If one processor needs to trigger logic in another, it **MUST** do so by publishing a standard CloudEvent (per ADR-0003) onto the NATS bus. This ensures processors are loosely coupled.

## Consequences

### Positive Consequences

* **Strong Separation of Concerns:** Code for different automation domains (e.g., presence, lighting) is cleanly isolated into different packages, preventing unintended coupling.
* **High Testability:** Each processor can be unit-tested in isolation, leading to more reliable code.
* **Lean Operations:** Provides the architectural benefits of modularity and separation while maintaining a single service to deploy and operate for V0.
* **Clear Path to Microservices:** Because processors are already isolated and communicate over the event bus, a processor that becomes sufficiently complex can be "lifted and shifted" into its own dedicated microservice in the future with minimal architectural changes.

### Negative Consequences

* **Upfront Design Cost:** Requires more initial design work to define the stable `Processor` interface and the hosting/routing logic within the `engine`.
* **Minor Communication Overhead:** In-process, event-based communication has slightly more overhead than a direct function call, which is an acceptable trade-off for the decoupling it provides.

### Neutral Consequences

* This decision establishes a "microservices-within-a-monolith" pattern as the primary software architecture for the `engine` service.
