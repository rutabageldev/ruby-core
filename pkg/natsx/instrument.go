package natsx

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// meterName is the instrumentation scope for ruby-core consumer metrics.
const meterName = "github.com/primaryrutabaga/ruby-core/pkg/natsx"

// Outcome labels for ruby_core_messages_processed_total. Each consumer loop maps its
// terminal branch (ack / nak / dedup) to one of these.
const (
	OutcomeSuccess   = "success"
	OutcomeFailure   = "failure"
	OutcomeDuplicate = "duplicate"
)

// MsgInstruments holds the OTel metric instruments shared by every JetStream consumer
// loop: a processed counter and a processing-duration histogram, both labeled by
// service / stream / consumer / outcome. Construct one per service (after otel.Init) and
// pass it into each loop. The instruments degrade to no-op when no OTLP endpoint is
// configured (the global MeterProvider is then a no-op), so the dev path needs no special
// handling. A nil *MsgInstruments is also safe — Observe just runs the work without
// recording, which keeps direct-constructed consumers in tests instrument-free.
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

// Observe runs fn (the per-message work), measures its wall-clock duration, and records
// the processed counter and duration histogram with the outcome string fn returns. A nil
// receiver runs fn without recording. The stream/consumer labels identify which durable
// consumer produced the message.
//
// ctx carries cancellation/baggage for the metric record. Distributed-trace spans are
// added in a later change; the signature already takes ctx so wrapping does not move.
func (mi *MsgInstruments) Observe(ctx context.Context, stream, consumer string, fn func() string) {
	if mi == nil {
		fn()
		return
	}
	start := time.Now()
	outcome := fn()
	elapsed := time.Since(start).Seconds()
	attrs := metric.WithAttributes(
		attribute.String("service", mi.service),
		attribute.String("stream", stream),
		attribute.String("consumer", consumer),
		attribute.String("outcome", outcome),
	)
	mi.processed.Add(ctx, 1, attrs)
	mi.duration.Record(ctx, elapsed, attrs)
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
