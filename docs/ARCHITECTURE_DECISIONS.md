# Architecture Decisions to be Made

This document tracks the key architectural decisions that need to be made for Ruby Core. It is a living document, synthesized from multiple rounds of architectural review. Once a decision is reached, it should be documented in an ADR and the corresponding item should be checked off.

---

### Core Infrastructure & Messaging

- [ ] **NATS Strategy: Core vs. JetStream**
  - **Question:** Do we need the durability guarantees (and operational complexity) of JetStream for V0, or is core NATS pub/sub sufficient?
  - **Options:** Start with Core NATS (simpler, faster, but no message replay), Start with JetStream (durable, at-least-once, but more complex to configure).
  - **Default Bias:** Start with NATS Core for initial phasing. Introduce JetStream only when a specific feature (e.g., event replay for state reconstruction) proves it is necessary.

- [ ] **Event Contract & Idempotency**
  - **Question:** How do we ensure "at-least-once" delivery doesn't cause duplicate actions? What is the standard shape of an event?
  - **Options:** No formal contract (risky), Define a standard event envelope with a unique Event ID, timestamp, source, and correlation/causation IDs.
  - **Default Bias:** Define a formal, versioned event envelope from day one. Every event MUST have a unique ID that consumers can use for idempotent processing.

- [ ] **Time & Command Policy**
  - **Question:** How does the system handle time and prevent stale commands?
  - **Options:** No explicit policy, Define clock discipline (event time vs. processing time), Enforce a Time-to-Live (TTL) on all commands.
  - **Default Bias:** All commands should have a short, sensible TTL. A stale command in home automation is often worse than no command. Clock discipline should be explicitly defined as "event time" where possible.

---

### Service Design & State

- [ ] **State Management & Read Models**
  - **Question:** Where will service state live, and how will we inspect it?
  - **Options:** In-memory only (fragile), NATS KV Store, Local embedded DB (e.g., BoltDB), No queryable state.
  - **Default Bias:** Use a lightweight persistent store like NATS KV or an embedded DB. Crucially, plan for services to materialize queryable "read models" of their internal state to aid debugging and future UI needs.

- [ ] **Configuration Strategy**
  - **Question:** How are automation rules and service configurations defined and loaded?
  - **Options:** Hardcoded, YAML/JSON files from disk, A dedicated configuration service.
  - **Default Bias:** Use YAML files read on startup. This is GitOps-friendly and avoids a service dependency.

- [ ] **Engine Decomposition**
  - **Question:** Will the "Automation/Rules Engine" be a monolith or decomposed?
  - **Options:** One large service, Separate logical processors within a single binary, Physically distinct microservices.
  - **Default Bias:** Develop as logical, single-purpose processors within a single Go binary initially. Do not split into microservices until domain boundaries are proven and complexity demands it.

---

### Gateway & External System Interaction

- [ ] **Gateway Responsibilities & Failure Domains**
  - **Question:** How are ingress and egress managed? How do we prevent over-normalization?
  - **Options:** Single service for ingress/egress, Split services, Keep as a single service but with independent internal circuit-breakers.
  - **Default Bias:** Keep as a single service for V0, but implement separate failure domains (e.g., circuit breakers) for the HA WebSocket connection and the HA REST API client. Avoid aggressive event normalization/whitelisting early on to prevent brittleness.

- [ ] **Gateway Health & Drift Reconciliation**
  - **Question:** How does the system detect and correct for stale state when the HA connection fails?
  - **Options:** Simple heartbeat, Heartbeat + periodic full state snapshot/reconciliation from HA.
  - **Default Bias:** The Gateway must publish its own health heartbeat. Additionally, a reconciliation mechanism should be implemented to periodically pull state from HA for key entities, correcting any drift caused by missed events during a disconnect.

---

### Operability & Developer Experience

- [ ] **Observability Strategy**
  - **Question:** What is the minimum viable observability stack?
  - **Options:** Health-checks only, Structured logging + metrics, Full distributed tracing + an "event journal".
  - **Default Bias:** Implement structured logging and OpenTelemetry from the start. Also, create a simple "event journal" service that logs all events on the bus to a file or stream for easy `grep`-style debugging.

- [ ] **Developer Workflow**
  - **Question:** What is the preferred method for rapid development feedback?
  - **Options:** True hot-reloading, A fast compile-and-restart loop.
  - **Default Bias:** Use a fast restart loop triggered by file changes (`air`, `watchever`). It is more reliable for Go.
