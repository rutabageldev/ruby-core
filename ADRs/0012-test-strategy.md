# ADR-0012 - Adopt a Pragmatic Pyramid Test Strategy

* **Status:** Accepted
* **Date:** 2026-02-01

## Context

To ensure the reliability and maintainability of Ruby Core, a clear and consistent testing philosophy is required. This strategy must balance test execution speed (for rapid feedback) with test coverage and confidence. The key decisions involve the ratio of unit vs. integration tests and the approach to handling external dependencies like NATS and the Home Assistant API.

## Decision

We will adopt a **"Pragmatic Pyramid"** testing strategy that applies different testing approaches to different types of code, combined with a clear policy on when different test suites are run.

1. **Pure Business Logic (Unit Tests):** The core business logic, such as that within `engine` processors (per ADR-0007), **MUST** be tested with a large suite of fast, isolated **unit tests**. These tests must not have any external I/O dependencies (e.g., network, filesystem).

2. **External Interactions (Integration Tests):** Code that directly interacts with external systems (our "adapters") **MUST** be tested with a smaller, more focused suite of **integration tests**.

3. **Dependency Strategy for Integration Tests:**
    * **NATS:** Integration tests involving NATS **MUST** run against a **real NATS server**, preferably managed via a library like `testcontainers`. We will not mock the NATS client library.
    * **Home Assistant:** Integration tests involving the Home Assistant API **MUST** run against a **scriptable, deterministic mock HA API server**. This ensures tests are fast and reliable, avoiding a dependency on a live, stateful Home Assistant instance.

4. **Test Execution Policy:**
    * **Unit Tests:** These are expected to run frequently. They **MUST** run as part of the pre-commit hook (per ADR-0011) and on every commit in the CI pipeline.
    * **Integration Tests:** These are considered slower and are reserved for the main CI/CD pipeline. They **MUST** run on every pull/merge request. Developers can run them locally on-demand, but they are not part of the pre-commit hook.

## Consequences

### Positive Consequences

* **Balanced Confidence:** Provides fast, precise feedback for pure business logic via unit tests, and high-confidence verification of our critical I/O boundaries via integration tests.
* **Avoids Brittle Mocks:** By testing against real (NATS) or realistic (mock HA server) dependencies at the system's edge, we avoid maintaining complex and brittle mocks of client libraries.
* **Keeps Developer Loop Fast:** The explicit separation of test execution keeps the fast-feedback pre-commit hook from being slowed down by integration tests.
* **Architectural Alignment:** This strategy fits perfectly with our "Logical Processor" architecture, which naturally separates pure logic from I/O-bound adapters.

### Negative Consequences

* **Requires Discipline:** Requires developers to maintain a clean separation between pure logic and I/O-bound adapter code.
* **CI Complexity:** The CI environment must be configured to provide the necessary dependencies for integration tests (e.g., Docker for `testcontainers`, the mock HA server).

### Neutral Consequences

* This decision formalizes a clear testing philosophy, providing guidance to all developers and ensuring a consistent approach to quality assurance.
