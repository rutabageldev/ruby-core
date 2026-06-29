# Phase 9 Verification — Full-Stack Observability (PLAN-0009)

PLAN-0009 shipped in two PRs:

* **PR 1 — metrics + log correlation** (#150, merged): the OTel SDK foundation, OTLP metric
  export from all 5 services, log-trace ID injection, and compose wiring. Closed **#31**
  (superseded by OTLP — no `/metrics` endpoint) and **#137** (idempotency metrics).
* **PR 2 — distributed traces** (this PR): per-service spans, W3C context propagation across
  NATS, and `ctx` threading through the engine `process` path to connect a
  gateway→engine→notifier trace.

## Acceptance Criteria (ROADMAP-0009)

| # | Criterion | Status | Notes |
|---|---|---|---|
| AC-1 | A distributed trace for a complete automation flow is viewable | `[~]` | Mechanism complete + unit-verified (inject→extract shares one trace ID); the live Tempo view is the deploy step below |
| AC-2 | Key service metrics visible in a dashboard | `[~]` | Emission code-complete (PR 1); dashboard visibility needs the live export below |
| AC-3 | Log entries contain trace IDs that correlate with traces | `[~]` | Injection shipped (PR 1) + spans now populate the IDs (PR 2); live Loki↔Tempo correlation is the deploy step |

`[~]` = code-complete and unit-verified; final sign-off is the one-time live confirmation in
Grafana after a deploy (this node has no telemetry backend access from CI, so it can't be
ticked from the repo). The connected-trace mechanism itself is proven by
`TestPublishWithContext_PropagatesTraceToObserve` in `pkg/natsx`.

---

## What PR 1 delivers

**OTel SDK foundation (`pkg/otel`).** `otel.Init(ctx, name, version)` installs OTLP gRPC
trace + metric exporters and the W3C propagator, keyed on `OTEL_EXPORTER_OTLP_ENDPOINT`.
Empty endpoint → no-op providers + nil error (the dev path); the gRPC exporters connect
lazily, so an unreachable collector never blocks or fails startup. Wired into all 5 service
mains with a deferred flush.

**Metrics (OTLP push, no `/metrics` endpoint — ADR-0004).**

| Metric | Type | Labels | Source |
|---|---|---|---|
| `ruby_core_messages_processed_total` | counter | service, stream, consumer, outcome | all 4 consumer loops (`pkg/natsx.MsgInstruments`) |
| `ruby_core_message_processing_duration_seconds` | histogram | service, stream, consumer, outcome | all 4 consumer loops |
| `ruby_core_idempotency_dedup_total` | counter | service | engine `resultSkip` branch |
| `ruby_core_idempotency_mark_failures_total` | counter | service | `hybridStore.Mark` KV-failure (#137) |
| `ruby_core_idempotency_kv_entries` | observable gauge | — | engine main, reads `kv.Status().Values()` (#137) |
| `ruby_core_audit_publish_dropped_total` | counter | service | `pkg/audit` channel-full drop |
| `ruby_core_ha_events_received_total` | counter | entity_domain | gateway publish path |
| `ruby_core_ha_websocket_reconnects_total` | counter | — | gateway HA client connect |
| `ruby_core_dlq_forwarded_total` | counter | stream | engine DLQ forwarder |
| `ruby_core_presence_state_published_total` | counter | person_id, state | presence publish path |
| `ruby_core_ada_boundary_crossings_total` | counter | — | ada processor (migrated from direct Prometheus) |

Per-service counters that would merely duplicate `ruby_core_messages_processed_total`
(audit-sink archived/write-failures, notifier sent/duration) were **deliberately not added**
— they are already `{service, outcome}` slices of the shared metric plus its duration
histogram. This keeps cardinality and dashboard surface intentional.

**Log correlation.** `pkg/logging` wraps the stdout JSON handler so every record stamped
inside a span carries `trace_id`/`span_id`. Logs stay on stdout (promtail → Loki; smoke and
stability scripts grep `docker logs`) rather than re-routing over the otelslog OTLP bridge.
With no active span (PR 1, or dev) the fields are simply omitted.

**Deploy wiring.** The 5 instrumented services join the external `observability` network and
set `OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317` in prod + staging. `api` is not
instrumented (no NATS); dev is unchanged. Staging sets `OTEL_DEPLOYMENT_ENVIRONMENT=staging`
so its telemetry is distinguishable from prod (the staging stack runs `ENVIRONMENT=production`
to mirror prod code paths).

---

## Verification

### Unit (`-tags=fast`) — passing now

```bash
go test -tags=fast ./pkg/otel/... ./pkg/logging/... ./pkg/natsx/...
```

* `pkg/otel`: no-op without an endpoint; propagator round-trips a `traceparent`.
* `pkg/logging`: `trace_id`/`span_id` injected from an active span; omitted without one.
* `pkg/natsx`: `Observe` records `messages_processed_total` (manual-reader MeterProvider);
  `NewDedupCounter` increments; nil-receiver `Observe` still runs the work.

### Dev — no-op degradation

```bash
make dev-up && make dev-services-up
make dev-ps    # all services Up; OTEL_EXPORTER_OTLP_ENDPOINT unset => no export, no crash
```

Confirm structured JSON logs still emit on stdout (no `trace_id` yet — expected until PR 2).

### Live metrics (staging or prod) — AC-2 sign-off

After a deploy with the `observability` network present, in Prometheus/Grafana:

```promql
sum by (service, outcome) (rate(ruby_core_messages_processed_total[5m]))
histogram_quantile(0.95, sum by (le, service) (rate(ruby_core_message_processing_duration_seconds_bucket[5m])))
ruby_core_idempotency_kv_entries
```

Expect a series per running service, filterable by `deployment_environment`
(`production` vs `staging`).

## What PR 2 delivers — distributed traces

**Spans** (all OTLP-exported, parented via W3C trace context carried in NATS headers):

| Span | Service | Where |
|---|---|---|
| `ha.ingest` | gateway | `handleEvent` — root span per HA WebSocket event |
| `nats.consume` | engine, notifier, presence, audit-sink | `pkg/natsx.MsgInstruments.Observe` (extracts the parent from headers) |
| `engine.process` | engine | `ProcessorHost.Process` |
| `notify.send` | notifier | `sendNotification` (HA push REST call) |
| `presence.wifi_check` | presence | `queryWifiState` (WiFi corroboration REST call) |

**Propagation.** `natsx.PublishWithContext` injects the active span's `traceparent` into the
NATS message headers; the consumer's `Observe` extracts it and opens `nats.consume` as a child.
The gateway injects on all three publish paths (`state_changed`, `ada_event`, `ruby_home_event`);
the engine threads `ctx` through `handle → decide → process → ProcessorHost.Process →
Processor.ProcessEvent`, and `presence_notify` publishes the notify command with context — so the
full flow connects:

```text
ha.ingest → nats.consume(engine) → engine.process → nats.consume(notifier) → notify.send
```

**Unit-verified:** `TestPublishWithContext_PropagatesTraceToObserve` asserts the consumer span
shares the producer's trace ID (inject→extract round-trip).

### Live verification (run once after a deploy — completes AC sign-off)

1. **Trace (AC-1):** trigger a presence transition (or publish a notify command); in Grafana →
   Tempo, search for service `gateway` and open the trace. Expect the connected span chain above
   with the engine and notifier spans as descendants of `ha.ingest`.
2. **Log correlation (AC-3):** in Loki, find an `engine`/`notifier` log line emitted during that
   flow; it carries `trace_id`/`span_id`. Paste the `trace_id` into Tempo and confirm it opens the
   same trace.
3. **Metrics (AC-2):** the PromQL above returns a series per service.
