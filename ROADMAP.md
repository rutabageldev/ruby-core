## Ruby Core Implementation Roadmap (Revised)

### **Phase 0: Project Setup & Core Contracts**

*(No changes to this phase)*

**Goal:** Establish the absolute foundational code structure and data contracts before any service logic is written.

*   **Key Tasks:**
    1.  Initialize the Go monorepo with the service directory structure (`/pkg`, `/services/gateway`, `/services/engine`).
    2.  Create a shared `pkg/schemas` package.
    3.  Define the core Go structs for CloudEvents, YAML rules, etc.
    4.  Create a shared `pkg/natsx` package to codify the subject naming convention.
    5.  Set up a basic `docker-compose.yml` with placeholders.
*   **Acceptance Criteria:**
    *   `[ ]` Data schemas are centralized and versioned in code (ADR-0014).
    *   `[ ]` The subject naming convention is available as a shared Go package (ADR-0027).

### **Phase 1: Secure Foundations & Initial Tooling**

**Goal:** Deploy a secure NATS server and establish the *minimum viable* developer and CI tooling needed for a functional release workflow.

*   **Key Tasks:**
    1.  **NATS:** Deploy a single, persistent, TLS-enabled NATS server with NKEY/ACL auth (per ADR-0021, ADR-0018, ADR-0017).
    2.  **Vault:** Set up the local Vault dev server (`vault server -dev`) for managing secrets required in subsequent phases (per ADR-0015).
    3.  **Dev Tooling:** Create a basic `docker-compose.dev.yml` with bind mounts for code. Implement a minimal pre-commit hook with only the fastest checks: `gofumpt` and `gitleaks` (per ADR-0011).
    4.  **Minimal CI:** Create a placeholder GitHub Actions workflow that can manually build and push a Docker image to GHCR. This validates credentials and the basic build process.

*   **Acceptance Criteria:**
    *   `[ ]` A secure NATS server is running.
    *   `[ ]` A developer can use the local Vault instance to manage secrets.
    *   `[ ]` `git commit` triggers basic formatting and secret scanning.
    *   `[ ]` A container image can be manually built and pushed to GHCR from the CI environment.

*   **What Not to Build Yet:** Full CI gates, live reload, complex pre-commit checks.

### **Phase 2: "Hello World" Production Release**

**Goal:** Build the minimum viable services to validate the full, automated release path, from Git tag to a running "production" container.

*   **Key Tasks:**
    1.  Build skeleton `gateway` and `engine` services that connect to NATS using credentials from Vault.
    2.  Implement the full **release pipeline**: **replace** the manual CI workflow with an automated pipeline triggered by `v*` tags that builds and pushes versioned images to GHCR (per ADR-0016).
    3.  Create and push a `v0.1.0` Git tag to trigger the new pipeline.
    4.  Manually deploy to the "production" host by updating `docker-compose.prod.yml` to use the `v0.1.0` images.

*   **Acceptance Criteria:**
    *   `[ ]` Services start successfully by fetching secrets from Vault (ADR-0015).
    *   `[ ]` Pushing a `v0.1.0` tag successfully builds and pushes versioned images to GHCR (ADR-0016).
    *   `[ ]` The `v0.1.0` services run successfully in the production environment.

### **Phase 3: Reliability & Audit Foundations**

**Goal:** Implement the core reliability and security logging patterns before adding complex business logic.

*   **Key Tasks:**
    1.  **Reliability:** Implement the DLQ strategy, refactor consumers to be pull-based with flow control, and create the shared idempotency checker library (per ADR-0022, ADR-0024, ADR-0025).
    2.  **Audit:** Implement the `audit.events` NATS stream and a simple `audit-sink` service that writes to a file. Services performing critical actions must publish audit events (per ADR-0019).
    3.  **Operations:** Document and test the backup and restore procedure for the production JetStream volume (per ADR-0021).
    4.  **Codify Defaults:** Create a central config struct for tuning values (`MaxAckPending`, TTLs, etc.).

*   **Acceptance Criteria:**
    *   `[ ]` A "poison pill" message is correctly moved to the DLQ.
    *   `[ ]` A consumer correctly applies backpressure under load.
    *   `[ ]` An idempotency check correctly discards a duplicate event.
    *   `[ ]` A critical action correctly produces a message in the audit log.
    *   `[ ]` The JetStream restore procedure is documented and validated.

### **Phase 4: Core Feature Implementation**

**Goal:** Build the primary business logic of the `gateway` and `engine`, including security at the edge.

*   **Key Tasks:**
    1.  **Edge Auth:** Configure Traefik with middleware to handle edge authentication *before* any API endpoints are exposed (per ADR-0020).
    2.  **Gateway:** Implement the full HA WebSocket client, lean projection, health heartbeat, and targeted reconciliation logic (per ADR-0009, ADR-0008).
    3.  **Engine:** Implement the "Logical Processor" framework and the YAML configuration file loader (per ADR-0007, ADR-0006).
    4.  Implement one complete, real automation (e.g., a debounced light).

*   **Acceptance Criteria:**
    *   `[ ]` Any exposed API on the `gateway` is protected by Traefik authentication.
    *   `[ ]` The `gateway` can connect to HA, process events, and reconcile state.
    *   `[ ]` The `engine` can load a YAML rule and execute a simple automation.

### **Phase 5: Full Developer Experience & CI Gates**

**Goal:** Fully build out the CI pipeline and enhance the developer experience to improve velocity and safety.

*   **Key Tasks:**
    1.  **DevEx:** Enhance `docker-compose.dev.yml` to use `air` for a fast, live-reload experience (ADR-0010).
    2.  **Pre-commit:** Flesh out the pre-commit hook to include `golangci-lint` and fast unit tests (ADR-0011).
    3.  **CI Gates:** Expand the CI pipeline to run the full comprehensive test gates (static analysis, all unit tests, integration tests, build artifacts) on all pull requests (ADR-0013).

*   **Acceptance Criteria:**
    *   `[ ]` `docker-compose up` provides a productive live-reload environment.
    *   `[ ]` `git commit` enforces all defined quality and security checks.
    *   `[ ]` A pull request cannot be merged without passing all CI gates.

### **Phase 6: Full Observability**

**Goal:** Complete the observability stack.

*   **Key Tasks:**
    1.  Deploy the OTel Collector, Jaeger, Prometheus, and Loki.
    2.  Fully instrument services with traces and metrics, exporting via OTLP to the collector (ADR-0004).

*   **Acceptance Criteria:**
    *   `[ ]` A distributed trace can be viewed in Jaeger for a complete automation flow.
    *   `[ ]` Basic service metrics are visible in a dashboard.
