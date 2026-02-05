# ADR-0011 - Adopt a Balanced Pre-commit Policy

* **Status:** Accepted
* **Date:** 2026-02-01

## Context

To ensure a baseline of code quality, consistency, and security before code is reviewed or tested in CI, we need an automated pre-commit policy. This policy must be rigorous enough to catch common issues but fast enough to avoid adding significant friction to the local development workflow. This ADR defines the set of checks that will be run automatically on every `git commit`.

## Decision

We will adopt a multi-stage pre-commit policy using the `pre-commit` framework. The following checks **MUST** be configured to run on all staged files where applicable:

1. **Formatting:** All Go files (`*.go`) **MUST** be automatically formatted via `golangci-lint`, with the `gofumpt` linter enabled (a stricter superset of `gofmt`). This avoids a separate gofumpt hook while still enforcing the stricter formatting standard.
2. **Secrets Scanning:** All staged changes **MUST** be scanned for hard-coded secrets using `gitleaks`. A commit that contains potential secrets will be blocked.
3. **Static Analysis & Linting:** The `golangci-lint` linter **MUST** be run. Its configuration **MUST** enable the following essential linters at a minimum:
    * `govet`: Catches common mistakes and suspicious constructs.
    * `staticcheck`: Provides extensive static analysis for bugs and performance issues.
    * `gosec`: Scans the code for security vulnerabilities.
4. **Dependency Hygiene:** If the `go.mod` or `go.sum` files are part of a commit, `go mod tidy` **MUST** be run automatically to ensure the dependency graph is tidy and module files are consistent.
5. **Unit Testing:** A "fast" subset of unit tests, identified by the `//go:build fast` tag, **MUST** be run. These tests must be pure unit tests with no I/O, network calls, or significant delays. **Untagged tests are considered 'slow' and will not be run as part of the pre-commit hook.** The full test suite (including untagged tests) is reserved for the CI pipeline.
6. **Non-Go Linters (Early Adoption):** To avoid accumulating unlinted configuration and tooling debt, we **MUST** also run maintained, pre-commit-managed linters for non-Go assets that exist in this repository:
   * **Dockerfiles:** `hadolint`
   * **YAML:** `yamllint`
   * **GitHub Actions:** `actionlint`
   * **Markdown (optional but enabled):** `markdownlint`
   * **Shell scripts (optional but enabled):** `shellcheck`

## Consequences

### Positive Consequences

* **Improved Code Quality:** Automatically enforces formatting, catches common bugs, and improves code style before review.
* **Enhanced Security:** The inclusion of `gitleaks` and `gosec` provides a critical first line of defense against committing secrets and introducing security vulnerabilities.
* **Faster Feedback Loop:** Catches a wide range of issues on the developer's machine in seconds, which is much faster than waiting for a CI pipeline to fail.
* **Maintains Developer Velocity:** The curated set of fast checks provides significant value without adding a frustrating delay to the `git commit` process.
* **Reduced Config Debt:** Early linting of Docker, YAML, GitHub Actions, Markdown, and shell scripts prevents a backlog of cleanup work later.

### Negative Consequences

* **Adds Dependencies:** Requires developers to have `pre-commit` and the associated tools (`golangci-lint`, `gitleaks`, plus non-Go linters) installed and configured.
* **Commit Time Overhead:** Adds a small delay (typically a few seconds) to each commit, which is a deliberate trade-off for the quality and security gains.

### Neutral Consequences

* This decision formalizes a specific set of tools and a standard of quality that all code contributions to the project must meet.
