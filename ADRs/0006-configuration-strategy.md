# ADR-0006 - Use Declarative YAML Files for Automation Configuration

* **Status:** Accepted
* **Date:** 2026-02-01

## Context

The Ruby Core system requires a method for defining and loading automation rules. The chosen strategy must be flexible enough to allow changes without recompiling the application, yet simple enough to align with our V0 principles of a lean stack and no premature over-engineering. The options considered ranged from hard-coding rules in Go (too inflexible) to building a dynamic configuration service with a UI (too complex). A file-based approach was identified as the correct middle ground.

## Decision

We will adopt **declarative YAML files** as the method for defining and loading automation rules for the V0 implementation. This decision is governed by the following explicit constraints:

1. **Reload Boundary:** For V0, any changes to automation rules within the YAML files will require a **restart of the consuming service** (e.g., the `engine`) to take effect. Hot-reloading of configuration is an explicit non-goal for the initial implementation.
2. **Schema and Validation:** The YAML schema for defining rules **MUST** be versioned. The consuming service **MUST** validate the configuration files against the expected schema version on startup and **MUST refuse to start** if any rule file is invalid or malformed.
3. **Declarative Scope:** The rule schema will be intentionally **constrained and declarative**. It will define *what* an automation should do, not *how* it should be implemented. The inability to express arbitrary code or complex imperative logic within the YAML is a deliberate design choice for V0 to ensure system stability and predictability.

## Consequences

### Positive Consequences

* **Decouples Rules from Code:** Automation logic can be developed, reviewed, and version-controlled in Git independently of the service binaries.
* **Sufficient Flexibility for V0:** Allows for rapid iteration on automation logic without the high architectural and operational cost of a dedicated configuration service.
* **Stability and Predictability:** The versioned, validated schema and declarative-first approach create a stable contract and reduce the risk of malformed rules causing unpredictable behavior.
* **Aligns with V0 Principles:** Adheres to our goals of maintaining a lean stack and avoiding premature over-engineering.

### Negative Consequences

* **Requires Service Restarts:** Applying any configuration change requires a service restart, which is not suitable for a dynamic, zero-downtime control plane in the long term, but is acceptable for V0.
* **Implementation Overhead:** Requires adding YAML parsing and validation logic to the `engine` service.
* **Constrained Logic:** The declarative format may prove too limiting for highly advanced or complex automations in the future, which will require a new ADR to address.

### Neutral Consequences

* This decision establishes YAML as the primary language for defining user-facing automation logic in the project.
