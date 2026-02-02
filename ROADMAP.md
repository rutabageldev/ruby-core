## Ruby Core Implementation Roadmap (Revised)

### **Phase 0: Project Setup & Core Contracts**

*(No changes to this phase)*

**Goal:** Establish the absolute foundational code structure and data contracts before any service logic is written.

*   **Key Tasks:**
    1.  Initialize the Go monorepo with the service directory structure.
    2.  Create a shared `pkg/schemas` package with Go structs for data contracts.
    3.  Create a shared `pkg/natsx` package to codify the subject naming convention.
    4.  Set up a basic `docker-compose.yml` with placeholders.
*   **Acceptance Criteria:**
    *   `[ ]` Data schemas are centralized and versioned in code (ADR-0014, ADR-0027).

### **Phase 1: Secure Infrastructure & Minimal CI**

**Goal:** Deploy secure infrastructure (NATS, Vault) and establish the *minimum viable* CI safety net to support foundational development.

*   **Key Tasks:**
    1.  **NATS:** Deploy a single, persistent, TLS-enabled NATS server with NKEY/ACL auth.
    2.  **Vault:** Set up the local Vault dev server for managing secrets.
    3.  **Dev Tooling:** Create a basic dev container and a minimal pre-commit hook (`gofumpt`, `gitleaks`).
    4.  **CI Gates:** Add a basic test gate to the CI workflow that runs `go test ./...` and verifies the code builds on every pull request.
    5.  **Operations:** Validate that the target host is configured to use NTP for time synchronization (per ADR-0026).

*   **Acceptance Criteria:**
    *   `[ ]` A secure NATS server is running.
    *   `[ ]` `git commit` triggers basic formatting and secret scanning.
    *   `[ ]` A pull request is blocked if basic tests or the build fails.
    *   `[ ]` Host time synchronization is confirmed.

### **Phase 2: "Hello World" Production Release**

**Goal:** Build the minimum viable services to validate the full, automated release path, from Git tag to a running "production" container.

*   **Key Tasks:**
    1.  Build skeleton `gateway` and `engine` services that connect to NATS using secrets from Vault.
    2.  Implement the full **release pipeline**: replace any manual CI workflow with an automated pipeline triggered by `v*` tags that builds and pushes versioned images to GHCR.
    3.  Create and push a `v0.1.0` Git tag to trigger the new pipeline.
    4.  Manually deploy to production by updating `docker-compose.prod.yml` to use the `v0.1.0` images.

*   **Acceptance Criteria:**
    *   `[ ]` Services start successfully by fetching secrets from Vault (ADR-0015).
    *   `[ ]` Pushing a `v0.1.0` tag successfully builds and pushes versioned images to GHCR (ADR-0016).
    *   `[ ]` The `v0.1.0` services run successfully in the production environment.

### **Phase 3: Reliability Patterns**

**Goal:** Implement the core reliability patterns for message handling before business logic is written.

*   **Key Tasks:**
    1.  Implement the DLQ strategy (ADR-0022).
    2.  Refactor consumers to be pull-based with flow control (ADR-0024).
    3.  Create the shared idempotency checker library (ADR-0025).
    4.  Codify default tuning values (`MaxAckPending`, TTLs, etc.) in a central config.

*   **Acceptance Criteria:**
    *   `[ ]` A "poison pill" message is correctly moved to the DLQ.
    *   `[ ]` A consumer correctly applies backpressure under load.
    *   `[ ]` An idempotency check correctly discards a duplicate event.

### **Phase 4: Audit & Foundational Observability**

**Goal:** Establish the security audit trail and implement baseline observability for debugging.

*   **Key Tasks:**
    1.  **Audit:** Implement the `audit.events` NATS stream and a simple `audit-sink` service. Services performing critical actions must publish audit events (ADR-0019).
    2.  **Operations:** Document and test the backup and restore procedure for the production JetStream volume (ADR-0021).
    3.  **Logging:** Implement structured (JSON) logging in all services. Ensure all logs include `correlationid` when available (per ADR-0004).

*   **Acceptance Criteria:**
    *   `[ ]` A critical action correctly produces a message in the audit log.
    *   `[ ]` The JetStream restore procedure is documented and validated.
    *   `[ ]` Logs are structured and contain correlation IDs, enabling basic distributed debugging.

### **Phase 5: Core Feature Implementation**

**Goal:** Build the primary business logic of the `gateway` and `engine` services.

*   **Key Tasks:**
    1.  **Edge Auth:** Configure Traefik with middleware to handle edge authentication *before* any API endpoints are exposed (ADR-0020).
    2.  **Gateway:** Implement the full HA WebSocket client, lean projection, health heartbeat, and targeted reconciliation logic (ADR-0009, ADR-0008).
    3.  **Engine:** Implement the "Logical Processor" framework and the YAML configuration file loader (ADR-0007, ADR-0006).
    4.  Implement one complete, real automation.

*   **Acceptance Criteria:**
    *   `[ ]` Any exposed API on the `gateway` is protected by Traefik.
    *   `[ ]` The `gateway` can connect to HA, process events, and reconcile state.
    *   `[ ]` The `engine` can load a YAML rule and execute a simple automation.

### **Phase 6: Full Developer Experience & CI Polish**

**Goal:** Fully build out the CI pipeline and enhance the developer experience to improve velocity and safety.

*   **Key Tasks:**
    1.  **DevEx:** Enhance `docker-compose.dev.yml` to use `air` for a fast, live-reload experience (ADR-0010).
    2.  **Pre-commit:** Flesh out the pre-commit hook to include the full `golangci-lint` suite and fast unit tests (ADR-0011).
    3.  **CI Polish:** Expand the CI pipeline to run the full comprehensive test gates, including **integration tests**, on all pull requests (ADR-0013).

*   **Acceptance Criteria:**
    *   `[ ]` A productive live-reload environment is available.
    *   `[ ]` `git commit` enforces all defined quality checks.
    *   `[ ]` No PR can be merged without passing all unit and integration tests.

### **Phase 7: Full-Stack Observability**

**Goal:** Complete the observability stack with distributed tracing and metrics.

*   **Key Tasks:**
    1.  Deploy the OTel Collector, Jaeger, Prometheus, and Loki.
    2.  Fully instrument services with **distributed traces** and **application-level metrics**, exporting via OTLP to the collector (ADR-0004).

*   **Acceptance Criteria:**
    *   `[ ]` A distributed trace can be viewed in Jaeger for a complete automation flow.
    *   `[ ]` Key service metrics (e.g., processing latency, queue depth) are visible in a dashboard.
