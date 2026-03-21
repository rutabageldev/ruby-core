# Full-Stack Observability

* **Status:** Planned
* **Date:** 2026-02-23
* **Project:** ruby-core
* **Related ADRs:** ADR-0004, ADR-0028
* **Linked Plan:** [docs/plans/PLAN-0009-full-stack-observability.md](../plans/PLAN-0009-full-stack-observability.md)

---

**Goal:** Complete the observability stack with distributed tracing and metrics by consuming the centralized observability infrastructure delivered by Foundation.

---

## Efforts

### 0009.1 — Consume Foundation observability stack

Instrument all five services with OpenTelemetry traces and application-level metrics, exporting via OTLP to the Foundation OTel Collector. Blocked on Foundation Phase 6 delivering: OTel Collector, trace backend (Jaeger/Tempo), Prometheus, Loki, and Grafana (ADR-0028).

### 0009.2 — Wire log-trace correlation

Add trace IDs to structured log fields so logs and traces are correlated in the shared stack. The slog→OTel bridge path is already planned in `pkg/logging/logging.go`.

---

## Done When

A distributed trace is visible in the trace backend for a complete automation flow (HA event → engine rule evaluation → notifier push), and key service metrics (processing latency, queue depth) are visible in a Grafana dashboard.

---

## Acceptance Criteria

* `[ ]` A distributed trace can be viewed in the trace backend for a complete automation flow.
* `[ ]` Key service metrics (e.g., processing latency, queue depth) are visible in a dashboard.
* `[ ]` Log entries contain trace IDs that correlate with traces in the backend.

---

## Blocking Dependencies

* Foundation Phase 6: centralized OTel Collector, trace backend, Prometheus, Loki, Grafana (ADR-0028)
