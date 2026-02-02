# ADR-0002 - Use NATS JetStream KV for State Management with a Single-Writer Model

* **Status:** Accepted
* **Date:** 2026-02-01
* **Supersedes:** Defers external DBs and explicit in-memory strategies for V0.

## Context

Many core features of Ruby Core (debouncing, confidence-based presence, state machines) require state that must be durable and survive service restarts. This state must be managed in a way that is both reliable and consistent, especially in a distributed system where multiple services might be interested in the same state.

Given the decision to use NATS JetStream (see ADR-0001), its built-in Key-Value (KV) store became a primary candidate. We also considered embedded databases (e.g., BoltDB) and external databases (e.g., Redis). A key challenge with any shared state is preventing race conditions and ensuring clear data ownership.

## Decision

We will use the **NATS JetStream Key-Value (KV) Store** as the primary mechanism for persisting durable service state, governed by the following strict constraints:

1. **Durability Boundary:** The KV store **MUST** be used for any state whose loss during a service restart would lead to incorrect system behavior. This includes, but is not limited to: the current state of finite state machines, calculated confidence scores, and the deadlines for in-flight debounce timers.
2. **Ownership & Consistency Model:** A strict **single-writer ownership model MUST** be enforced at the application level. Ownership is defined per **logical keyspace** (e.g., `presence:person_id:*` keys might be owned by the PresenceService), not globally per service. For any given keyspace, only one designated service or logical processor is allowed to write to it. All other services are consumers (readers) of that state. This is a development convention that is critical for system stability.
3. **Source of Truth:** The KV store itself serves as the **durable source of truth** for the state it manages. Any in-memory caches or read models built by services are projections derived from this source.
4. **Read Models:** Services are permitted to `Watch()` KV buckets to build and maintain their own in-memory, queryable read models for performance and efficiency.

## Consequences

### Positive Consequences

* **Reliability:** State is durable and survives service restarts, meeting a core system requirement.
* **Lean Architecture:** Avoids introducing a new database technology (e.g., Redis, BoltDB) into the stack for V0, reducing operational complexity.
* **Consistency:** The single-writer model provides a clear, predictable data flow and prevents a large class of race condition bugs.
* **Observability:** State is centrally managed within the NATS infrastructure and can be inspected and managed via the `nats` CLI tools, improving system-wide debugging.

### Negative Consequences

* **Requires Discipline:** The single-writer model is an application-level convention. It requires developer discipline, code reviews, and clear documentation to enforce, as the KV store itself does not prevent multiple writers.
* **Network Latency:** State access is over the network, which will always be higher latency than an in-process database. This is an acceptable trade-off for our use case.
* **Simple State Model:** The KV store is not suitable for complex queries. This may require more sophisticated read model strategies in the future if complex querying becomes a requirement.

### Neutral Consequences

* This decision tightly couples our state management strategy to the NATS ecosystem, making it a foundational technology for both messaging and state.
