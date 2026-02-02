# ADR-0014 - Schema Governance via Schema-in-Code and Semantic Versioning

*   **Status:** Accepted
*   **Date:** 2026-02-01

## Context

Our distributed system relies on stable data contracts (schemas) for YAML rules, CloudEvents, and other data structures. To evolve the system without causing consumers to break due to unexpected changes, we need a formal governance policy. An ad-hoc process is too risky for a reliable system, while a dedicated schema registry service is overly complex for our V0 needs.

## Decision

We will adopt a **"Schema-in-Code"** strategy with **Semantic Versioning** to govern all shared data schemas.

1.  **Source of Truth & Location:** The Go `struct` definitions for all shared schemas will be the single source of truth. These definitions **MUST** be located in a centralized, shared package (e.g., `pkg/schemas`) within the project repository to ensure all services use the same definitions.

2.  **Explicit Versioning:** All schemas **MUST** include an explicit version identifier. For example, YAML rule files will use a `schemaVersion: "1.0"` field, and CloudEvents will use the `dataschema` attribute.

3.  **Compatibility and Evolution Rules:** Schema changes **MUST** follow Semantic Versioning (SemVer) principles, defined as follows:
    *   **Additive Changes (MINOR):** Adding new optional fields is a backward-compatible change and **MUST** result in a MINOR version increment (e.g., `1.0` -> `1.1`). Consumers should be written to ignore unknown fields.
    *   **Breaking Changes (MAJOR):** Removing a field, renaming a field, or changing a field's data type is a backward-incompatible change. This **MUST** result in a MAJOR version increment (e.g., `1.1` -> `2.0`).
    *   **Migration Strategy for Breaking Changes:** To handle MAJOR version changes without downtime, a **"publish both versions"** strategy **MUST** be used. The producer will publish the new major version of an event (e.g., on a new NATS subject like `light.on.v2`), while temporarily continuing to publish the old version. Consumers will be upgraded incrementally, and the old version will be formally deprecated and removed only after all consumers have migrated.

## Consequences

### Positive Consequences

*   **Safe Evolution:** Provides a clear, predictable, and safe process for evolving schemas, preventing silent breakages between services.
*   **Zero-Downtime Migration:** The "publish both versions" strategy allows services to be upgraded without taking the entire system offline.
*   **Single Source of Truth:** Centralizing schema definitions in a shared Go package provides a single, type-safe source of truth that is version-controlled with the rest of the code.
*   **Pragmatic:** Implements robust governance without requiring new infrastructure (like a schema registry).

### Negative Consequences

*   **Requires Discipline:** Success depends on developer diligence in identifying breaking vs. non-breaking changes and correctly managing the versioning and deprecation process.
*   **Temporary Complexity:** Supporting multiple schema versions during a migration period adds temporary complexity to both the producer and consumer codebases.

### Neutral Consequences

*   This decision formalizes a key development process for managing data contracts within the project.
