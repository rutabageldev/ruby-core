//go:build fast

package natsx

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
)

// pubFunc adapts a function to the MsgPublisher interface.
type pubFunc func(*nats.Msg) error

func (f pubFunc) PublishMsg(m *nats.Msg) error { return f(m) }

// PublishWithContext injects the active span's W3C context into the message headers, and a
// downstream Observe extracts it so the consumer span shares the producer's trace — the
// mechanism that connects a gateway→engine→notifier trace.
func TestPublishWithContext_PropagatesTraceToObserve(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(nooptrace.NewTracerProvider()) })

	// Producer: start a span and publish, capturing the message that would hit NATS.
	ctx, span := tracer.Start(context.Background(), "producer")
	parentTraceID := span.SpanContext().TraceID()

	var captured *nats.Msg
	pub := pubFunc(func(m *nats.Msg) error { captured = m; return nil })
	if err := PublishWithContext(ctx, pub, "ruby_engine.commands.notify.x", []byte("{}")); err != nil {
		t.Fatalf("PublishWithContext: %v", err)
	}
	span.End()

	if captured.Header.Get("traceparent") == "" {
		t.Fatal("publish did not inject a traceparent header")
	}

	// Consumer: Observe extracts the parent and opens a child span in the same trace.
	mi, err := NewMsgInstruments("notifier")
	if err != nil {
		t.Fatalf("NewMsgInstruments: %v", err)
	}
	var childTraceID trace.TraceID
	mi.Observe(context.Background(), captured, "COMMANDS", "notifier_processor", func(sctx context.Context) string {
		childTraceID = trace.SpanContextFromContext(sctx).TraceID()
		return OutcomeSuccess
	})

	if childTraceID != parentTraceID {
		t.Errorf("consumer span trace ID = %s, want %s (not connected)", childTraceID, parentTraceID)
	}
}

// sumValue returns the total of all int64 data points for the named counter in rm, and
// whether the metric was found at all.
func sumValue(rm metricdata.ResourceMetrics, name string) (int64, bool) {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				return 0, false
			}
			var total int64
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
			return total, true
		}
	}
	return 0, false
}

// hasMetric reports whether a metric with the given name is present in rm.
func hasMetric(rm metricdata.ResourceMetrics, name string) bool {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				return true
			}
		}
	}
	return false
}

// Observe records the processed counter and the duration histogram, and runs fn exactly
// once. Uses a manual-reader MeterProvider installed globally for the test.
func TestObserve_RecordsProcessedAndDuration(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { otel.SetMeterProvider(noopmetric.NewMeterProvider()) })

	mi, err := NewMsgInstruments("engine")
	if err != nil {
		t.Fatalf("NewMsgInstruments: %v", err)
	}

	runs := 0
	for i := 0; i < 3; i++ {
		mi.Observe(context.Background(), &nats.Msg{Subject: "ha.events.x"}, "HA_EVENTS", "engine_processor", func(context.Context) string {
			runs++
			return OutcomeSuccess
		})
	}
	if runs != 3 {
		t.Fatalf("fn ran %d times, want 3", runs)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}

	if got, ok := sumValue(rm, "ruby_core_messages_processed_total"); !ok || got != 3 {
		t.Errorf("ruby_core_messages_processed_total = %d (found=%v), want 3", got, ok)
	}
	if !hasMetric(rm, "ruby_core_message_processing_duration_seconds") {
		t.Error("ruby_core_message_processing_duration_seconds not recorded")
	}
}

// A nil *MsgInstruments still runs the work, recording nothing — the path direct-constructed
// consumers and the dev (no otel.Init) path rely on.
func TestObserve_NilReceiverRunsWork(t *testing.T) {
	var mi *MsgInstruments
	ran := false
	mi.Observe(context.Background(), &nats.Msg{}, "S", "c", func(context.Context) string {
		ran = true
		return OutcomeSuccess
	})
	if !ran {
		t.Error("nil-receiver Observe did not run fn")
	}
}

// NewDedupCounter registers without error under a real provider and increments.
func TestNewDedupCounter_Increments(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { otel.SetMeterProvider(noopmetric.NewMeterProvider()) })

	ctr, err := NewDedupCounter()
	if err != nil {
		t.Fatalf("NewDedupCounter: %v", err)
	}
	ctr.Add(context.Background(), 2)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if got, ok := sumValue(rm, "ruby_core_idempotency_dedup_total"); !ok || got != 2 {
		t.Errorf("ruby_core_idempotency_dedup_total = %d (found=%v), want 2", got, ok)
	}
}
