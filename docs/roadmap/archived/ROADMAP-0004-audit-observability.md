# Audit & Foundational Observability

* **Status:** Complete
* **Date:** 2026-02-25
* **Project:** ruby-core
* **Related ADRs:** ADR-0019, ADR-0021, ADR-0004
* **Linked Plan:** none

---

**Goal:** Establish the security audit trail and implement baseline observability for debugging.

---

## Efforts

1. **Audit:** Implement the `AUDIT_EVENTS` NATS stream and a simple `audit-sink` service. Services performing critical actions must publish audit events (ADR-0019).
2. **Operations:** Document and test the backup and restore procedure for the production JetStream volume (ADR-0021).
3. **Logging:** Implement structured (JSON) logging in all services. Ensure all logs include `correlationid` when available (ADR-0004).

---

## Done When

Critical actions produce audit records in the stream, the JetStream restore procedure is documented and validated, and all service logs are structured JSON with correlation IDs.

---

## Acceptance Criteria

* `[X]` A critical action correctly produces a message in the audit log.
* `[X]` The JetStream restore procedure is documented and validated.
* `[X]` Logs are structured and contain correlation IDs, enabling basic distributed debugging.

---

## Implementation Notes

* Audit stream: `AUDIT_EVENTS` (subjects: `audit.>`); see ADR-0019 for naming rationale
* Audit publisher: `pkg/audit/publisher.go` — bounded channel (cap 256), non-blocking
* Audit sink: `services/audit-sink/` — NDJSON archival to `/var/lib/ruby-core/audit`
* Backup procedure: `docs/ops/jetstream-backup.md`
* Verification report: `docs/ops/phase4-verification.md`
