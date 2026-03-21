# PLAN-0009 — Full-Stack Observability

* **Status:** Approved
* **Date:** 2026-03-20
* **Project:** ruby-core
* **Roadmap Item:** [docs/roadmap/ROADMAP-0009-full-stack-observability.md](../roadmap/ROADMAP-0009-full-stack-observability.md)
* **Branch:** `phase-9-observability`
* **Related ADRs:** ADR-0004, ADR-0028

---

## Scope

Instrument all five ruby-core services with OpenTelemetry distributed traces and
application-level metrics, wire log-trace correlation so slog entries carry trace IDs,
and connect services to the Foundation-hosted OTel Collector via OTLP gRPC. Ruby Core
deploys no observability infrastructure of its own (ADR-0028). This plan covers
instrumentation code, infrastructure wiring (compose files, env vars), and end-to-end
verification. It does not cover Foundation's observability stack — that is a pre-condition,
and it is met (Foundation Phase 6 complete, stack running as of 2026-03-08).

**Key decisions (resolved before approval):**

* **Protocol:** gRPC (`otel-collector:4317`) — Foundation RUNBOOK labels this preferred
* **Prometheus scrape model:** OTLP push only; Collector remote-writes to Prometheus.
  No per-service `/metrics` endpoints needed.
* **NATS trace propagation:** Inject W3C TraceContext into outbound NATS message headers;
  extract on consume to create parent-child spans across service boundaries.
* **Dev environment:** No local Collector; `pkg/otel` degrades to no-op when
  `OTEL_EXPORTER_OTLP_ENDPOINT` is unset.

---

## Pre-conditions

* [X] Foundation Phase 6 complete: OTel Collector, Prometheus, Tempo, Loki, Grafana all
      running (confirmed 2026-03-20, up since 2026-03-08)
* [X] OTel Collector reachable at `otel-collector:4317` (gRPC) via the `observability`
      Docker external bridge network
* [X] Prometheus remote-write endpoint live at `http://prometheus:9090/api/v1/write`;
      Collector already configured to push application metrics there
* [X] Grafana data source for Tempo (traces) confirmed working
* [ ] Ruby Core prod and staging services have been confirmed able to reach
      `otel-collector:4317` after joining the `observability` network (verify in Step 9)

---

## Steps

### Step 1 — Update Go dependencies

**Action:** Promote the following from indirect to direct dependencies, adding any missing
packages. Use gRPC exporter variants throughout:

```
go.opentelemetry.io/otel
go.opentelemetry.io/otel/sdk
go.opentelemetry.io/otel/trace
go.opentelemetry.io/otel/metric
go.opentelemetry.io/otel/sdk/metric
go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc
go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc
go.opentelemetry.io/otel/propagators/b3          (if needed; W3C TraceContext is SDK default)
go.opentelemetry.io/contrib/bridges/otelslog
```

Run `go mod tidy` after adding.

**Verification:** `go build ./...` succeeds. `go list -m all | grep opentelemetry` shows
all new packages as direct (no `// indirect`). Pre-commit passes clean.

---

### Step 2 — Create `pkg/otel/` initialization package

**Action:** Create `pkg/otel/otel.go` with a single exported function:

```go
func Init(ctx context.Context, serviceName, serviceVersion string) (shutdown func(context.Context) error, err error)
```

Requirements:

* Reads `OTEL_EXPORTER_OTLP_ENDPOINT` (default: `otel-collector:4317`)
* Configures a `TracerProvider` (OTLP gRPC exporter) and `MeterProvider` (OTLP gRPC exporter)
* Sets both as global providers via `otel.SetTracerProvider` and `otelmetric.SetMeterProvider`
* Attaches resource attributes: `service.name`, `service.version`, `deployment.environment`
  (from `ENVIRONMENT` env var, default `development`)
* Returns a `shutdown` func that flushes and closes both providers — callers must defer it
* **Graceful degradation:** if `OTEL_EXPORTER_OTLP_ENDPOINT` is unset or the endpoint is
  unreachable at startup, logs a `WARN` and installs no-op providers. Service must not fail
  to start due to an unavailable Collector.

**Verification:** `go test -tags=fast ./pkg/otel/...` passes, including:

* `TestInit_NoEndpoint`: calls `Init` with env unset; confirms no-op shutdown returned,
  no error, no crash
* `TestInit_ResourceAttributes`: confirms `service.name` attribute is set on the provider

---

### Step 3 — Wire log-trace correlation in `pkg/logging/`

**Action:** In `pkg/logging/logging.go`, replace `slog.NewJSONHandler(os.Stdout, nil)` with
the `otelslog` bridge handler (`go.opentelemetry.io/contrib/bridges/otelslog`). The bridge
emits slog records as OTel log records, automatically including `trace_id` and `span_id`
when a span is active in the context. Preserve the `service` attribute.

This is a single-file change, explicitly planned in the package comment since Phase 7.

`pkg/otel.Init` must be called before `logging.NewLogger` in each service's `main()` so the
global TracerProvider is set before the bridge is wired.

**Verification:** `go test -tags=fast ./pkg/logging/...` passes. Write
`TestNewLogger_TraceCorrelation`: create a span, call `slog.InfoContext` within it, capture
output, confirm `trace_id` and `span_id` fields are present in the JSON.

---

### Step 4 — Instrument consumer message processing (`pkg/natsx/consumer.go`)

**Action:** In the worker goroutine, for each fetched message:

1. **Extract** W3C TraceContext from NATS message headers (using `propagation.TraceContext{}`)
   to create a child span when the publisher injected context. Fall back to a root span if
   no context header is present.
2. **Start span** `nats.consume` with semantic attributes:
   `messaging.system=nats`, `messaging.destination={subject}`,
   `messaging.consumer_group={consumer_name}`, `messaging.operation=process`
3. **Record span outcome:** OK on ack, error with message on nak/term
4. **End span** after ack/nak decision

Add metrics using the global MeterProvider:

* Counter `ruby_core_messages_processed_total` — labels: `service`, `stream`, `consumer`,
  `outcome` (`ack`|`nak`|`term`)
* Histogram `ruby_core_message_processing_duration_seconds` — labels: `service`, `stream`,
  `consumer`
* Counter `ruby_core_idempotency_dedup_total` — label: `service`; increment in the
  idempotency-duplicate path

**Verification:** `go test -tags=fast ./pkg/natsx/...` passes. Existing consumer tests
must continue to pass without modification (instrumentation is additive).

---

### Step 5 — Add NATS context injection on publish

**Action:** In `services/gateway/nats/publisher.go`, inject W3C TraceContext into outbound
NATS message headers when a span is active in the context:

```go
msg := &nats.Msg{Subject: subject, Data: data}
msg.Header = make(nats.Header)
propagation.TraceContext{}.Inject(ctx, propagation.MapCarrier(msg.Header))
```

This applies to event publishes from the gateway (HA events → HA_EVENTS stream) so engine
and presence consumers receive the parent span context and produce child spans.

**Verification:** `go test -tags=fast ./services/gateway/...` passes.

---

### Step 6 — Instrument audit publish failures (`pkg/audit/publisher.go`)

**Action:** Add counter metric `ruby_core_audit_publish_dropped_total` (label: `service`)
that increments in the channel-full drop path alongside the existing `Warn` log. This
surfaces the ADR-0019-required high-priority alert signal in metrics form.

**Verification:** `go test -tags=fast ./pkg/audit/...` passes.

---

### Step 7 — Wire OTel init into all five services' `main()`

**Action:** In each `main()`, immediately after `logging.NewLogger` and before the Vault
bootstrap:

```go
otelShutdown, err := otel.Init(ctx, "ruby-core-{service}", version)
if err != nil {
    logger.Warn("otel: init failed, running without telemetry", slog.String("error", err.Error()))
}
defer func() { _ = otelShutdown(context.Background()) }()
```

Note: `otel.Init` errors are non-fatal by design (Step 2 graceful degradation). Log the
warning and continue.

Services: `gateway`, `engine`, `notifier`, `presence`, `audit-sink`.

**Verification:** `go build ./...` succeeds. Each service starts cleanly in dev with
`OTEL_EXPORTER_OTLP_ENDPOINT` unset — confirm no crash, no error exit, single `WARN` log
line about running without telemetry.

---

### Step 8 — Add service-specific instrumentation

#### Gateway

* Span `ha.ingest` per HA WebSocket event received, ending after NATS publish; attributes:
  `ha.entity_id`, `ha.domain`, `messaging.destination`
* Counter `ruby_core_ha_events_received_total` — label: `entity_domain`
* Counter `ruby_core_ha_websocket_reconnects_total`

#### Engine

* Span `engine.rule_eval` per rule evaluated in the processor host; attributes: `rule_name`,
  `triggered` (bool)
* Counter `ruby_core_dlq_forwarded_total`

#### Notifier

* Span `notify.send` per HA REST call; attributes: `device`, `http.status_code`; record
  error on non-2xx
* Histogram `ruby_core_notification_duration_seconds` — label: `outcome` (`success`|`ha_error`|`config_missing`)
* Counter `ruby_core_notifications_sent_total` — label: `outcome`

#### Presence

* Span `presence.wifi_check` per HA REST corroboration call; attributes: `person_id`,
  `wifi_connected`
* Counter `ruby_core_presence_state_published_total` — labels: `person_id`, `state`

#### Audit-sink

* Counter `ruby_core_audit_events_archived_total`
* Counter `ruby_core_audit_write_failures_total`

**Verification:** `go test -tags=fast ./...` passes. `go test -tags=integration ./...` passes.
In dev (no-op mode), trigger a test event and confirm structured logs still emit correctly.

---

### Step 9 — Update compose files and environment configuration

**Action:**

1. Add `observability` as an external network in `deploy/prod/compose.prod.yaml` and
   `deploy/staging/compose.staging.yaml`:

   ```yaml
   networks:
     observability:
       external: true
   ```

   Attach all five application services (gateway, engine, notifier, presence, audit-sink)
   to this network.

2. Add to each service's env block in prod and staging compose:

   ```yaml
   OTEL_EXPORTER_OTLP_ENDPOINT: "otel-collector:4317"
   OTEL_EXPORTER_OTLP_PROTOCOL: "grpc"
   ```

3. Add `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_EXPORTER_OTLP_PROTOCOL` to
   `deploy/prod/.env.example` and `deploy/staging/.env.example` with comments.

4. Leave `deploy/dev/compose.dev.yaml` and `deploy/dev/compose.air.yaml` unchanged.
   Dev runs in no-op mode without the Collector.

5. Verify connectivity: after joining the `observability` network, confirm a ruby-core
   container can reach the Collector:

   ```bash
   docker run --rm --network observability curlimages/curl \
     -sf http://otel-collector:13133/  # Collector health check endpoint
   ```

**Verification:** `docker compose -f deploy/prod/compose.prod.yaml config` resolves without
errors. The `observability` network appears in the rendered output for all five services.
Connectivity check above returns 200.

---

### Step 10 — End-to-end verification in staging

**Action:** Tag a staging release. After `deploy-staging` succeeds, trigger a full
automation flow (Katie's phone state change → presence → engine → notifier). Then verify:

1. **Traces (Tempo/Grafana):** Find the trace for the automation flow. Confirm spans are
   connected across service boundaries: gateway ingest → engine rule eval → notifier send.
   Confirm parent-child span relationships via injected NATS headers.

2. **Metrics (Prometheus/Grafana):** Confirm the following series are present and populated:
   * `ruby_core_messages_processed_total`
   * `ruby_core_message_processing_duration_seconds`
   * `ruby_core_notifications_sent_total`

3. **Log correlation (Loki/Grafana):** Find log entries for the automation flow. Confirm
   `trace_id` and `span_id` fields are present and match the trace in Tempo.

**Verification:** All three checks pass. Mark acceptance criteria in
`docs/roadmap/ROADMAP-0009-full-stack-observability.md` as `[X]`. Create
`docs/ops/phase9-verification.md` documenting trace IDs, screenshots, and metric query
results from the verification run.

---

## Rollback

Instrumentation is purely additive — no schema changes, no new streams, no stateful
operations. If deployed instrumentation causes a service crash or performance regression,
rollback is: revert the instrumentation commits and redeploy via `make deploy-prod`
(auto-rollback will fire automatically if smoke tests fail).

The `pkg/otel` graceful degradation (Step 2) means a misconfigured or unreachable Collector
produces a `WARN` log and a no-op provider, not a crash. A blocked gRPC dial at startup
(wrong endpoint, network misconfigured) is the most likely failure mode — the `WithTimeout`
on the gRPC connection must be set to avoid hanging the boot sequence.
