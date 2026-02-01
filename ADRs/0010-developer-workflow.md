# ADR-0010 - Use 'air' for a Fast Compile-and-Restart Developer Workflow

*   **Status:** Accepted
*   **Date:** 2026-02-01

## Context

To ensure high developer productivity, a rapid and reliable feedback loop during local development is essential. The manual process of stopping, recompiling, and restarting services after each code change is slow and error-prone. True hot-reloading for a compiled language like Go is notoriously brittle and can lead to an inconsistent application state. Therefore, a strategy that balances speed with reliability is required.

## Decision

For local development, we will adopt a **fast compile-and-restart loop** strategy, implemented using the **`air`** tool.

1.  **Concrete Tooling:** We will standardize on `air` as the file-watching and live-reload tool for this project.
2.  **Development-Only Tooling:** `air` and its configuration **MUST** only be included in development environments (e.g., configured via `docker-compose.override.yml`). Production Docker images **MUST** be minimal and built without `air` or any other development-only dependencies.
3.  **Integration:** In development, `air` will be configured to monitor all `*.go` source files. Upon detecting a change, it will automatically rebuild the service's binary and restart the running process.
4.  **Fallback Mechanism:** The standard `go build` process and the ability to run the service binary manually must always be maintained. This serves as a reliable fallback if `air` encounters issues or is not desired for a specific debugging scenario.

## Consequences

### Positive Consequences

*   **Productivity:** Provides a fast and automated feedback loop, significantly improving the inner-loop developer experience.
*   **Reliability:** By using a full recompile and restart, it guarantees that every change results in a clean, predictable application state, avoiding the subtle bugs common with hot-reloading.
*   **Clean Production Artifacts:** Explicitly separates development tooling from production images, ensuring our production containers are minimal and secure.
*   **Architectural Alignment:** The restart-based workflow is well-suited to our stateless service architecture where durable state is externalized (per ADR-0002).

### Negative Consequences

*   **New Dependency:** Adds `air` as a new development-only dependency to the project.
*   **Not Instantaneous:** The feedback loop involves a compile/restart cycle (typically 1-3 seconds), which is slightly slower than a theoretical "perfect" hot-reload. This is a deliberate trade-off for reliability.

### Neutral Consequences

*   This decision formalizes a specific and consistent workflow for all developers working on the project locally.
