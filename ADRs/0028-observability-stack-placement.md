# ADR-0028 - Consume Centralized Observability from Foundation

* **Status:** Proposed
* **Date:** 2026-02-23

## Context

Phase 9 of the Ruby Core roadmap calls for a full-stack observability implementation: OTel Collector, Jaeger, Prometheus, and Loki (per ADR-0004). Ruby Core runs on a single node alongside the Foundation repo (`/opt/foundation`), which already provides shared infrastructure: Vault, Traefik, Zigbee2MQTT, MQTT.

The key question: **Should Ruby Core stand up its own dedicated observability stack, or consume a centralized observability stack in Foundation?**

Foundation's roadmap (Phase 6: Observability stack) explicitly plans a **centralized** stack for "foundation and its consumers (ruby-core, Home Assistant, homelab services, custom apps)" with Prometheus, Grafana, Uptime Kuma, and optionally Loki. It does not yet specify OTel Collector or a trace backend (Jaeger/Tempo).

## Decision

We will **consume a centralized observability stack from Foundation** rather than deploying a dedicated observability stack within Ruby Core.

1. **Stack ownership:** The OTel Collector, Prometheus, Loki, and trace backend (Jaeger or Tempo) are deployed and operated by Foundation, in `observability/` (or equivalent), reachable via a shared network (e.g. `vault_default` or a dedicated `observability` network that Ruby Core joins).

2. **Ruby Core responsibilities:**
   * Instrument all services with distributed traces and application-level metrics per ADR-0004.
   * Export telemetry via OTLP to the Foundation OTel Collector endpoint (configurable via `OTEL_EXPORTER_OTLP_ENDPOINT` or equivalent).
   * Do **not** deploy OTel Collector, Prometheus, Loki, Jaeger, or Grafana within the Ruby Core repo.

3. **Foundation scope alignment:** Foundation Phase 6 (or equivalent) **MUST** include:
   * **OTel Collector** — OTLP ingestion (gRPC/HTTP) and forwarding to backends.
   * **Trace backend** — Jaeger or Tempo for distributed traces.
   * **Prometheus** — metrics storage (scraped by Collector or directly from exporters).
   * **Loki** — log aggregation (optional but recommended for Phase 9).
   * **Grafana** — dashboards and query UI.

   Ruby Core Phase 9 implementation is contingent on Foundation providing this stack. If Foundation's observability phase is delayed, Ruby Core may temporarily run a minimal local OTel Collector that forwards to a future Foundation stack, but the long-term target is centralized consumption.

4. **Networking:** Ruby Core services (prod, staging) attach to the network where the Foundation OTel Collector is reachable (e.g. `otel-collector:4317` for gRPC or `http://otel-collector:4318` for HTTP). Foundation documents the endpoint and network attachment pattern in its RUNBOOK.

## Consequences

### Positive Consequences

* **Consistency with existing pattern:** Vault, Traefik, MQTT, and Z2M are shared infra in Foundation. Observability follows the same model — one stack for the node, consumed by all projects.
* **Single pane of glass:** One Prometheus, one Loki, one Jaeger/Grafana for the entire homelab (Foundation, ruby-core, HA, future apps). Easier to correlate events across services.
* **Lower operational burden:** No duplicate Prometheus/Loki/Jaeger to manage, back up, or size. Foundation owns retention, disk, and upgrade cadence.
* **Resource efficiency:** Fewer containers and volumes on the single node.
* **Alignment with Foundation roadmap:** Foundation Phase 6 already targets ruby-core as a consumer; this ADR formalizes the contract.

### Negative Consequences

* **Cross-repo dependency:** Ruby Core Phase 9 depends on Foundation delivering its observability stack. If Foundation Phase 6 is deprioritized, Ruby Core observability is blocked.
* **Coordination required:** Foundation must expose OTLP ingestion and document the endpoint; Ruby Core must configure services to use it. Both repos need to stay in sync on network topology and env vars.
* **No isolated observability for Ruby Core:** Cannot run a fully isolated observability stack for ruby-core-only debugging without Foundation. For a single-node personal project, this is acceptable.

### Neutral Consequences

* Ruby Core's instrumentation code and OTLP export configuration are unchanged regardless of where the Collector runs. Only the endpoint (env var) differs.
* ADR-0004 remains in force: services export only to the OTel Collector via OTLP; they do not export directly to Jaeger, Prometheus, or Loki.

## Alternatives Considered

### Dedicated Ruby Core observability stack

Deploy OTel Collector, Jaeger, Prometheus, Loki within the Ruby Core repo (e.g. `deploy/observability/` or a separate compose file).

* **Rejected because:** Duplicates infrastructure that Foundation already plans to provide. Inconsistent with the pattern of consuming shared infra (Vault, Traefik) from Foundation. Increases operational burden and resource usage on the single node.

### Hybrid: Ruby Core deploys Collector only, Foundation provides backends

Ruby Core runs a minimal OTel Collector that forwards to Foundation-hosted Prometheus/Loki/Jaeger.

* **Rejected because:** Adds an extra hop and another component to manage. If Foundation provides the full stack, a single Collector in Foundation is simpler. This alternative could be revisited if Foundation provides only backends (e.g. Prometheus/Loki) and not the Collector.

## References

* ADR-0004: Observability Strategy (OTel Collector as telemetry boundary)
* Foundation roadmap: Phase 6 — Observability stack (planned)
* Ruby Core ROADMAP: Phase 9 — Full-Stack Observability
