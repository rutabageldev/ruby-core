# ADR Index

Generated from `docs/adr/*.md` by `make docs-index` — do not hand-edit; run the
target after adding an ADR. See the individual ADRs for full context.

| ADR | Title | Status |
|---|---|---|
| [0001](adr/0001-nats-strategy.md) | Use NATS JetStream for Durable Messaging | Accepted |
| [0002](adr/0002-state-management-strategy.md) | Use NATS JetStream KV for State Management with a Single-Writer Model | Accepted |
| [0003](adr/0003-cloudevents-contract.md) | Adopt CloudEvents with Mandatory Traceability and Consumer-Side Idempotency | Accepted |
| [0004](adr/0004-observability-strategy.md) | Adopt OpenTelemetry Collector as the Unified Telemetry Boundary | Accepted |
| [0005](adr/0005-time-and-command-policy.md) | Adopt a Hybrid Time-to-Live (TTL) Policy for Commands | Accepted |
| [0006](adr/0006-configuration-strategy.md) | Use Declarative YAML Files for Automation Configuration | Accepted |
| [0007](adr/0007-engine-decomposition.md) | Decompose Engine into Logical Processors | Accepted |
| [0008](adr/0008-gateway-health-and-reconciliation.md) | Gateway Health Signaling and Targeted Drift Reconciliation | Accepted |
| [0009](adr/0009-gateway-responsibilities.md) | Gateway Responsibilities: Failure Isolation and Lean Projection | Accepted |
| [0010](adr/0010-developer-workflow.md) | Use 'air' for a Fast Compile-and-Restart Developer Workflow | Accepted |
| [0011](adr/0011-pre-commit-policy.md) | Adopt a Balanced Pre-commit Policy | Accepted |
| [0012](adr/0012-test-strategy.md) | Adopt a Pragmatic Pyramid Test Strategy | Accepted |
| [0013](adr/0013-ci-cd-test-gates.md) | Define Comprehensive CI Test Gates for Pull Requests | Accepted |
| [0014](adr/0014-schema-governance.md) | Schema Governance via Schema-in-Code and Semantic Versioning | Accepted |
| [0015](adr/0015-secrets-config-management.md) | Use Vault for Secrets and Environment Files for Configuration | Accepted |
| [0016](adr/0016-release-promotion-policy.md) | Adopt a Git Tag-based Release and Promotion Policy | Accepted |
| [0017](adr/0017-nats-auth.md) | Per-Service Authentication and Authorization using NKEYs and ACLs | Accepted |
| [0018](adr/0018-transport-security.md) | Mandate End-to-End Transport Layer Security (TLS) | Accepted |
| [0019](adr/0019-security-audit-logging.md) | Decoupled Security Audit Trail via NATS | Accepted |
| [0020](adr/0020-gateway-api-auth.md) | Edge Authentication via Traefik for Gateway APIs | Accepted |
| [0021](adr/0021-nats-ha-strategy.md) | Adopt a Resilient Single-Node NATS Deployment | Accepted |
| [0022](adr/0022-poison-message-dlq-strategy.md) | Adopt a Dead-Letter Queue (DLQ) Strategy for Failed Messages | Accepted |
| [0023](adr/0023-single-writer-enforcement.md) | Enforce Single-Writer State via NATS ACLs | Accepted |
| [0024](adr/0024-backpressure-flow-control.md) | Adopt Pull Consumers for Backpressure and Flow Control | Accepted |
| [0025](adr/0025-idempotency-tracking-store.md) | Idempotency Tracking using a Cached KV Store | Accepted |
| [0026](adr/0026-clock-skew-tolerance.md) | Manage Clock Skew with Infrastructure Sync and Application Tolerance | Accepted |
| [0027](adr/0027-subject-naming-convention.md) | Adopt a Hierarchical NATS Subject Naming Convention | Accepted |
| [0028](adr/0028-observability-stack-placement.md) | Consume Centralized Observability from Foundation | Accepted |
| [0029](adr/0029-stateful-processors.md) | Extend Engine to Support Stateful Processors | Accepted |
| [0030](adr/0030-direct-pki-issuance-no-sidecar.md) | Direct-PKI Issuance for Ruby-Core mTLS (No Sidecar) | Accepted |
| [0031](adr/0031-ada-test-data-model.md) | Ada test-data marking model | Proposed |
| [0032](adr/0032-ada-trends-acquisition.md) | Ada trends acquisition | Proposed |
| [0033](adr/0033-ada-projection-integrity.md) | Ada projection integrity: single-writer live ingest and permanent growth retention | Accepted |
| [0034](adr/0034-jetstream-stream-retention-bounds.md) | Bounded JetStream stream retention and config reconciliation | Accepted |
| [0035](adr/0035-ada-clean-slate-at-birth.md) | Ada clean slate at birth: pre-birth test marking and auto-clear on ada.born | Accepted |
| [0036](adr/0036-ada-birth-watcher-snapshot-then-nuke.md) | Ada birth clean slate: host watcher snapshots then nukes | Accepted |
| [0037](adr/0037-ada-medications-emergency-persistence.md) | Ada Medications & Emergency: persistence and event/sensor contract | Accepted |
| [0038](adr/0038-ada-medication-safety-computations.md) | Ada medication safety computations: server is authoritative | Accepted |
| [0039](adr/0039-nats-boot-time-recovery.md) | NATS Boot-Time Recovery: Decouple Startup from the nats-init One-Shot Gate | Proposed |
| [0040](adr/0040-read-api-service-and-auth.md) | Synchronous Read API Service (`services/api`) with Defense-in-Depth Auth | Accepted |
| [0041](adr/0041-openapi-lifecycle-and-codegen-governance.md) | OpenAPI Lifecycle & Codegen Governance for the HTTP API | Accepted |
| [0042](adr/0042-calendar-sync-architecture.md) | Calendar Sync Architecture: Google as Source of Record, Local Durable Mirror | Accepted |
| [0043](adr/0043-ada-boundary-based-today-rollover.md) | Ada boundary-based "Today" rollover (configurable bedtime, not UTC midnight) | Accepted |
