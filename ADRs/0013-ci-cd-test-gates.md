# ADR-0013 - Define Comprehensive CI Test Gates for Pull Requests

*   **Status:** Accepted
*   **Date:** 2026-02-01

## Context

The CI/CD pipeline serves as the ultimate gatekeeper for code quality, protecting the stability of the `main` branch. While pre-commit hooks provide an initial check, the CI pipeline must be the authoritative source of truth for whether a change is safe to merge. This ADR defines the suite of automated checks that a pull request must pass to be considered "green."

## Decision

We will adopt a **"Comprehensive Gate"** policy for our CI pipeline. To be eligible for merging, all pull requests targeting the `main` branch **MUST** pass the following sequence of checks:

1.  **Static Analysis & Security Scanning:** This initial stage runs fast, parallel checks on the submitted code:
    *   **Formatting & Linting:** `gofumpt` and `golangci-lint` must pass (per ADR-0011).
    *   **Secrets Scanning:** `gitleaks` **MUST** be run as a defense-in-depth measure to ensure no secrets are present.

2.  **Unit Tests:** The full suite of unit tests (`go test ./...`), including both "fast" and "slow" tests, **MUST** pass. This stage runs after the static analysis is complete.

3.  **Integration Tests:**
    *   The full suite of integration tests **MUST** pass. This stage only runs after the unit test stage has succeeded.
    *   **Execution Policy:** For V0, integration tests will run on all non-draft pull requests. A policy for allowing manual skipping of this gate on draft PRs (e.g., via labels) may be considered in the future.

4.  **Build Artifacts:**
    *   The pipeline **MUST** successfully build the Go service binaries (`go build ./...`) and the final production Docker images (`docker build ...`).
    *   **Boundary:** This step's purpose is to verify that the application is buildable. The act of *pushing* the built images to a container registry is a release-specific action and is not part of this standard pull request gate.

## Consequences

### Positive Consequences

*   **High Confidence:** Provides strong confidence that merged code is high-quality, secure, well-tested, and deployable.
*   **Protects `main` Branch:** Creates a robust safety net that prevents regressions, broken integrations, and non-buildable code from being merged.
*   **Efficient Feedback:** By ordering the stages (fast static checks first, then unit tests, then slower integration tests), the pipeline can fail quickly on simple errors.
*   **Defense in Depth:** Re-running checks like secrets scanning in CI ensures they are not bypassed locally.

### Negative Consequences

*   **Slower Pipeline Duration:** A comprehensive pipeline including integration tests and Docker builds will take longer to run than a minimalist approach. This is an accepted trade-off for the quality assurance it provides.

### Neutral Consequences

*   This decision formally defines the meaning of a "green build" for a pull request within the project.
