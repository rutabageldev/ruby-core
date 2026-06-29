package natsx

import (
	"context"
	"time"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// meterName is the instrumentation scope for ruby-core consumer metrics and the
// nats.consume span.
const meterName = "github.com/primaryrutabaga/ruby-core/pkg/natsx"

// tracer opens consumer spans. otel.Tracer delegates to the global TracerProvider, so a
// package-level value picks up the real provider once otel.Init installs it.
var tracer = otel.Tracer(meterName)

// Outcome labels for ruby_core_messages_processed_total. Each consumer loop maps its
// terminal branch (ack / nak / dedup) to one of these.
const (
	OutcomeSuccess   = "success"
	OutcomeFailure   = "failure"
	OutcomeDuplicate = "duplicate"
)

// natsHeaderCarrier adapts nats.Header to the W3C TextMapCarrier interface so trace
// context can be injected into and extracted from NATS message headers.
type natsHeaderCarrier nats.Header

func (c natsHeaderCarrier) Get(key string) string { return nats.Header(c).Get(key) }
func (c natsHeaderCarrier) Set(key, value string) { nats.Header(c).Set(key, value) }
func (c natsHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// MsgPublisher is the subset of *nats.Conn needed to publish a message with headers.
// Defined as an interface so processors that inject a narrow publish dependency (for
// testing) can still carry trace context.
type MsgPublisher interface {
	PublishMsg(*nats.Msg) error
}

// PublishWithContext publishes data to subject, injecting the active span's W3C trace
// context into the message headers so the consumer can continue the same trace. Use this
// in place of nc.Publish on any cross-service hop that should appear as one connected
// trace (PLAN-0009).
func PublishWithContext(ctx context.Context, pub MsgPublisher, subject string, data []byte) error {
	msg := &nats.Msg{Subject: subject, Data: data, Header: nats.Header{}}
	otel.GetTextMapPropagator().Inject(ctx, natsHeaderCarrier(msg.Header))
	return pub.PublishMsg(msg)
}

// MsgInstruments holds the OTel metric instruments shared by every JetStream consumer
// loop: a processed counter and a processing-duration histogram, both labeled by
// service / stream / consumer / outcome. Construct one per service (after otel.Init) and
// pass it into each loop. The instruments degrade to no-op when no OTLP endpoint is
// configured (the global MeterProvider is then a no-op), so the dev path needs no special
// handling. A nil *MsgInstruments is also safe — Observe just runs the work without a
// span or metrics, which keeps direct-constructed consumers in tests instrument-free.
type MsgInstruments struct {
	service   string
	processed metric.Int64Counter
	duration  metric.Float64Histogram
}

// NewMsgInstruments registers the shared message instruments under the global
// MeterProvider. Call after otel.Init. With no OTLP endpoint the provider is a no-op and
// the returned instruments record nothing; the error is non-nil only on a genuine
// registration failure.
func NewMsgInstruments(service string) (*MsgInstruments, error) {
	m := otel.Meter(meterName)
	processed, err := m.Int64Counter(
		"ruby_core_messages_processed_total",
		metric.WithDescription("JetStream messages processed by a consumer, by outcome"),
	)
	if err != nil {
		return nil, err
	}
	duration, err := m.Float64Histogram(
		"ruby_core_message_processing_duration_seconds",
		metric.WithDescription("Wall-clock duration of per-message processing"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}
	return &MsgInstruments{service: service, processed: processed, duration: duration}, nil
}

// Observe extracts any W3C trace context from msg's headers, opens a child nats.consume
// span, runs fn (the per-message work) with the span-scoped context, then records the
// processed counter and duration histogram with the outcome string fn returns. fn MUST use
// the context it is passed so downstream publishes and logs join the same trace. A nil
// receiver runs fn with the original context and records nothing.
//
// The stream/consumer labels identify which durable consumer produced the message.
func (mi *MsgInstruments) Observe(ctx context.Context, msg *nats.Msg, stream, consumer string, fn func(context.Context) string) {
	if mi == nil {
		fn(ctx)
		return
	}

	pctx := otel.GetTextMapPropagator().Extract(ctx, natsHeaderCarrier(msg.Header))
	sctx, span := tracer.Start(pctx, "nats.consume",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "nats"),
			attribute.String("messaging.destination.name", stream),
			attribute.String("messaging.consumer.name", consumer),
		),
	)
	defer span.End()

	start := time.Now()
	outcome := fn(sctx)
	elapsed := time.Since(start).Seconds()

	span.SetAttributes(attribute.String("messaging.outcome", outcome))
	if outcome == OutcomeFailure {
		span.SetStatus(codes.Error, "processing failed")
	}

	attrs := metric.WithAttributes(
		attribute.String("service", mi.service),
		attribute.String("stream", stream),
		attribute.String("consumer", consumer),
		attribute.String("outcome", outcome),
	)
	mi.processed.Add(sctx, 1, attrs)
	mi.duration.Record(sctx, elapsed, attrs)
}

// NewDedupCounter registers ruby_core_idempotency_dedup_total, incremented each time a
// consumer discards a message its idempotency store has already seen (#137). The caller
// applies the {service} attribute at increment time. Returns a no-op counter when no OTLP
// endpoint is configured; the error is non-nil only on a genuine registration failure.
func NewDedupCounter() (metric.Int64Counter, error) {
	return otel.Meter(meterName).Int64Counter(
		"ruby_core_idempotency_dedup_total",
		metric.WithDescription("Messages discarded as duplicates by the idempotency store"),
	)
}
