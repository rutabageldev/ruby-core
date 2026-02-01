# ADR-0004 - Adopt OpenTelemetry Collector as the Unified Telemetry Boundary

*   **Status:** Accepted
*   **Date:** 2026-02-01

## Context

To debug and operate our distributed system, we require a robust observability strategy covering logs, metrics, and traces. OpenTelemetry (OTel) has been selected as the baseline standard for instrumentation. The key architectural decision is not *whether* to use OTel, but *how* to architect its implementation to ensure long-term flexibility, avoid vendor lock-in, and define a clear, manageable scope for V0. The primary question is where the boundary between our services and the observability backend should lie.

## Decision

We will adopt the **OpenTelemetry (OTel) Collector** as the single, unified boundary for all telemetry data emitted by Ruby Core services. This decision establishes an architectural posture, not a commitment to specific backend tools.

1.  **The OTLP Contract:** All services **MUST** be instrumented using the OTel SDK. They **MUST** export all telemetry (logs, metrics, traces) *only* to the OTel Collector endpoint using the OpenTelemetry Protocol (OTLP). Direct export from a service to any other backend (e.g., Jaeger, a vendor) is explicitly disallowed.
2.  **Centralized Policy Management:** The OTel Collector is designated as the central point for managing telemetry policy. This includes, but is not limited to, trace sampling, data enrichment (e.g., adding environment tags), and batching. The decision to centralize policy management is architectural; the specific sampling rates and enrichment rules are considered implementation details.
3.  **Minimal Required Signals for V0:** For the initial implementation (V0), all services **MUST** emit the following minimal signals:
    *   **Structured Logs** that **MUST** include `TraceId` and `SpanId` *when present* in the current execution context.
    *   **Distributed Traces** covering, at a minimum, inter-service communication (e.g., NATS message publishing and consumption).
    *   **Basic Service Health Metrics** (e.g., uptime, process health) **MUST** be emitted via OTLP to the Collector. If an application provides a Prometheus-compatible `/metrics` endpoint (e.g., for Go runtime metrics), it should be scraped by the Collector, not directly by an external Prometheus instance.
4.  **Backend Agnosticism:** The choice of specific backend tools (e.g., Jaeger for traces, Prometheus for metrics, Loki for logs) is an implementation detail. The Collector will be configured to forward telemetry to the chosen backends, which can be changed in the future without requiring any code changes in our Go services.

## Consequences

### Positive Consequences

*   **Architectural Decoupling:** Services are completely decoupled from the observability backend, providing maximum flexibility to change backends in the future (e.g., from self-hosted Jaeger to a commercial vendor) by only changing the collector's configuration.
*   **Centralized Control:** Simplifies service logic by centralizing complex and environment-specific configurations like sampling rules in the collector.
*   **Clear and Manageable Scope:** Defines a clear "minimum viable" set of signals for V0, preventing over-commitment while still providing essential debugging capabilities (logs with trace IDs).
*   **Vendor-Agnostic Standard:** Commits the project to the open OTLP standard, not a proprietary protocol.

### Negative Consequences

*   **Additional Component:** Introduces an additional service (the OTel Collector) into the operational stack that must be deployed and managed.

### Neutral Consequences

*   This decision formally commits the project to the OpenTelemetry standard for all service instrumentation.
