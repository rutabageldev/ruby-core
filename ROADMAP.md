## Ruby Core Implementation Roadmap (Revised)

### **Phase 0: Project Setup & Core Contracts** [Complete]

**Goal:** Establish the absolute foundational code structure and data contracts before any service logic is written.

* **Key Tasks:**
    1. Initialize the Go monorepo with the service directory structure.
    2. Create a shared `pkg/schemas` package with Go structs for data contracts.
    3. Create a shared `pkg/natsx` package to codify the subject naming convention.
    4. Set up a basic `docker-compose.yml` with placeholders.
* **Acceptance Criteria:**
  * `[X]` Data schemas are centralized and versioned in code (ADR-0014, ADR-0027).

### **Phase 1: Secure Infrastructure & Minimal CI** [Complete]

**Goal:** Deploy secure infrastructure (NATS, Vault) and establish the *minimum viable* CI safety net to support foundational development.

* **Key Tasks:**
    1. **NATS:** Deploy a single, persistent, TLS-enabled NATS server with NKEY/ACL auth.
    2. **Vault:** Set up the local Vault dev server for managing secrets.
    3. **Dev Tooling:** Create a basic dev container and a pre-commit hook (implemented `golangci-lint` with `gofumpt`, `govet`, `staticcheck`, `gosec` enabled, `gitleaks`, plus early non-Go linters like `hadolint`, `yamllint`, `actionlint`, `markdownlint`, `shellcheck`).
    4. **CI Gates:** Add a basic test gate to the CI workflow that runs `go test ./...` and verifies the code builds on every pull request.
    5. **Operations:** Validate that the target host is configured to use NTP for time synchronization (per ADR-0026).

* **Acceptance Criteria:**
  * `[X]` A secure NATS server is running (TLS + NKEY auth + JetStream).
  * `[X]` `git commit` triggers formatting, linting (Go + non-Go), and secret scanning.
  * `[X]` A pull request is blocked if basic tests or the build fails.
  * `[X]` Host time synchronization is confirmed (docs/ops/ntp.md).
  * `[x]` The dev container can be built and opened successfully, and supports running go test ./... and pre-commit run --all-files.

* **Implementation Notes (Phase 1):**
  * NATS config: `deploy/base/nats/nats.conf` with JetStream, TLS, NKEY ACLs
  * Compose: `deploy/dev/compose.dev.yaml` with NATS + Vault services
  * Vault docs: `docs/ops/vault-dev.md`
  * Pre-commit: `.pre-commit-config.yaml` (golangci-lint with gofumpt, govet, staticcheck, gosec + gitleaks; hadolint, yamllint, actionlint, markdownlint, shellcheck)
  * CI: `.github/workflows/ci.yml` (test + build on PRs)
  * Release: `.github/workflows/release.yml` (manual GHCR workflow)
  * NTP docs: `docs/ops/ntp.md`

### **Phase 2: "Hello World" Production Release** [Complete]

**Goal:** Build the minimum viable services to validate the full, automated release path, from Git tag to a running "production" container.

* **Key Tasks:**
    1. Build skeleton `gateway` and `engine` services that connect to NATS using secrets from Vault.
    2. Implement the full **release pipeline**: replace any manual CI workflow with an automated pipeline triggered by `v*` tags that builds and pushes versioned images to GHCR.
    3. Create and push a `v0.1.0` Git tag to trigger the new pipeline.
    4. Manually deploy to production by updating `docker-compose.prod.yml` to use the `v0.1.0` images.

* **Acceptance Criteria:**
  * `[X]` Gateway and engine log successful NATS connection using Vault-sourced credentials (ADR-0015).
  * `[X]` Pushing a `v0.1.0` tag builds and pushes `gateway:v0.1.0` and `engine:v0.1.0` to GHCR (ADR-0016).
  * `[X]` `docker compose` deploy runs both services and they stay running for at least 5 minutes.
  * `[X]` Rollback instructions exist for redeploying a prior tag.

* **Implementation Notes (Phase 2):**
  * Gateway: `services/gateway/main.go` — Vault-sourced NKEY + mTLS, graceful shutdown
  * Engine: `services/engine/main.go` — same pattern
  * Boot package: `pkg/boot/boot.go` — Vault fetch with retry, TLS 1.3, prod HTTPS guard
  * Unit tests: `pkg/boot/boot_test.go` (28 test cases)
  * Release pipeline: `.github/workflows/release.yml` (v* tag -> GHCR matrix build)
  * Prod compose: `deploy/prod/compose.prod.yaml` (ENVIRONMENT=production, Vault-sourced secrets)
  * Verification: `scripts/verify-tls-stack.sh`, `make dev-verify`
  * Rollback runbook: `docs/ops/deploy-rollback.md`
  * Verification report: `docs/ops/phase2-verification.md`

### **Phase 3: Reliability Patterns** [Complete]

**Goal:** Implement the core reliability patterns for message handling before business logic is written.

* **Key Tasks:**
    1. Implement the DLQ strategy (ADR-0022).
    2. Refactor consumers to be pull-based with flow control (ADR-0024).
    3. Create the shared idempotency checker library (ADR-0025).
    4. Codify default tuning values (`MaxAckPending`, TTLs, etc.) in a central config.

* **Acceptance Criteria:**
  * `[X]` A "poison pill" message is correctly moved to the DLQ.
  * `[X]` A consumer correctly applies backpressure under load.
  * `[X]` An idempotency check correctly discards a duplicate event.

### **Phase 4: Audit & Foundational Observability** [Complete]

**Goal:** Establish the security audit trail and implement baseline observability for debugging.

* **Key Tasks:**
    1. **Audit:** Implement the `audit.events` NATS stream and a simple `audit-sink` service. Services performing critical actions must publish audit events (ADR-0019).
    2. **Operations:** Document and test the backup and restore procedure for the production JetStream volume (ADR-0021).
    3. **Logging:** Implement structured (JSON) logging in all services. Ensure all logs include `correlationid` when available (per ADR-0004).

* **Acceptance Criteria:**
  * `[X]` A critical action correctly produces a message in the audit log.
  * `[X]` The JetStream restore procedure is documented and validated.
  * `[X]` Logs are structured and contain correlation IDs, enabling basic distributed debugging.

### **Phase 5: Core Feature Implementation**

**Goal:** Build the primary business logic of the `gateway` and `engine` services.

* **Key Tasks:**
    1. **Edge Auth:** Configure Traefik with middleware to handle edge authentication *before* any API endpoints are exposed (ADR-0020).
    2. **Gateway:** Implement the full HA WebSocket client, lean projection, health heartbeat, and targeted reconciliation logic (ADR-0009, ADR-0008).
    3. **Engine:** Implement the "Logical Processor" framework and the YAML configuration file loader (ADR-0007, ADR-0006).
    4. Implement one complete, real automation.

* **Acceptance Criteria:**
  * `[X]` Any exposed API on the `gateway` is protected by Traefik.
  * `[X]` The `gateway` can connect to HA, process events, and reconcile state.
  * `[X]` The `engine` can load a YAML rule and execute a simple automation.

### **Phase 6: Post-Deploy Smoke Test & Auto-Rollback**

**Goal:** Eliminate silent broken deploys by running a full end-to-end pipeline check immediately after every `make deploy-prod`, rolling back automatically on failure and pushing a phone notification either way.

* **Key Tasks:**
    1. Write `scripts/smoke-test.sh $VERSION` — publishes a synthetic `ha.events.phone.katie {state:home}` event to prod NATS, then polls the PRESENCE and COMMANDS JetStream streams for the expected messages within a 10-second timeout each. Accepts an optional `ROLLBACK_FROM` env var so the notification can read "vX.X.X failed, rollback to vY.Y.Y was successful".
    2. Extend `deploy-prod` in the Makefile to: (a) capture the currently-running version before pulling (from the running container image label, written to `.last-deployed-version`); (b) run `smoke-test.sh $VERSION` after the NATS SIGHUP; (c) on smoke test failure, re-deploy the previous version and re-run the smoke test as a rollback validation, then exit non-zero.
    3. On smoke test **pass**: push HA notification "Deployment of ruby-core vX.X.X successful at HH:MM" to `mobile_app_phone_michael`.
    4. On smoke test **fail + rollback pass**: push "ruby-core vX.X.X failed — rollback to vY.Y.Y successful at HH:MM".
    5. On smoke test **fail + rollback fail**: push "ruby-core vX.X.X failed — rollback to vY.Y.Y also failed. Manual intervention required." and exit non-zero loudly.

* **Acceptance Criteria:**
  * `[X]` A successful `make deploy-prod` sends a push notification to Michael's phone confirming the version and time.
  * `[X]` Deploying a broken image (e.g. bad ACLs, missing rule) triggers automatic rollback and a failure notification.
  * `[X]` `make deploy-prod` exits non-zero if rollback is also required, making CI-friendliness possible in Phase 8.

### **Phase 7: Full Developer Experience & CI Polish**

**Goal:** Fully build out the CI pipeline and enhance the developer experience to improve velocity and safety.

* **Key Tasks:**
    1. **DevEx:** Enhance `docker-compose.dev.yml` to use `air` for a fast, live-reload experience (ADR-0010).
    2. **Pre-commit:** Flesh out the pre-commit hook to include fast unit tests (ADR-0011).
    3. **CI Polish:** Expand the CI pipeline to run the full comprehensive test gates, including **integration tests**, on all pull requests (ADR-0013).

* **Acceptance Criteria:**
  * `[ ]` A productive live-reload environment is available.
  * `[ ]` `git commit` enforces all defined quality checks.
  * `[ ]` No PR can be merged without passing all unit and integration tests.

### **Phase 8: Staging Environment & Deploy Validation**

**Goal:** Eliminate manual back-and-forth during releases by automatically validating deployability before prod.

* **Key Tasks:**
    1. Add `deploy/staging/compose.staging.yaml` (same images as prod, separate container names/ports, shared Vault).
    2. Add a GitHub Actions workflow triggered on `v*` tags that deploys to the node over SSH, runs `scripts/smoke-test.sh` (from Phase 6) against staging, and blocks the release from being marked "latest" on failure.
    3. Gate `make deploy-prod` on a passing staging run (GitHub environment protection rule).

* **Acceptance Criteria:**
  * `[ ]` Pushing a version tag auto-deploys to staging and runs smoke tests.
  * `[ ]` A broken deploy (e.g. missing rule files, bad ACLs) fails in staging before reaching prod.
  * `[ ]` `make deploy-prod` requires a green staging run.

---

### **Phase 9: Full-Stack Observability**

**Goal:** Complete the observability stack with distributed tracing and metrics.

* **Key Tasks:**
    1. Deploy the OTel Collector, Jaeger, Prometheus, and Loki.
    2. Fully instrument services with **distributed traces** and **application-level metrics**, exporting via OTLP to the collector (ADR-0004).

* **Acceptance Criteria:**
  * `[ ]` A distributed trace can be viewed in Jaeger for a complete automation flow.
  * `[ ]` Key service metrics (e.g., processing latency, queue depth) are visible in a dashboard.
