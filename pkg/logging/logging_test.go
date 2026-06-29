//go:build fast

package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// newBufLogger builds a logger that writes to buf, using the same traceHandler wrapper as
// NewLogger so the correlation behaviour under test matches production.
func newBufLogger(buf *bytes.Buffer, service string) *slog.Logger {
	base := slog.NewJSONHandler(buf, nil)
	return slog.New(&traceHandler{inner: base}).With(slog.String("service", service))
}

// With a valid span in the context, the JSON record carries trace_id/span_id matching the
// span context — and preserves the service attribute.
func TestNewLogger_TraceCorrelation(t *testing.T) {
	var buf bytes.Buffer
	logger := newBufLogger(&buf, "engine")

	traceID, _ := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
	spanID, _ := trace.SpanIDFromHex("b7ad6b7169203331")
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	logger.InfoContext(ctx, "processed event")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("log line is not valid JSON: %v (%q)", err, buf.String())
	}
	if got := rec["trace_id"]; got != traceID.String() {
		t.Errorf("trace_id = %v, want %s", got, traceID.String())
	}
	if got := rec["span_id"]; got != spanID.String() {
		t.Errorf("span_id = %v, want %s", got, spanID.String())
	}
	if got := rec["service"]; got != "engine" {
		t.Errorf("service = %v, want engine", got)
	}
}

// With no span in the context, no trace_id/span_id fields are emitted — the handler behaves
// exactly like the plain JSON handler.
func TestNewLogger_NoSpanNoTraceFields(t *testing.T) {
	var buf bytes.Buffer
	logger := newBufLogger(&buf, "engine")

	logger.InfoContext(context.Background(), "no span here")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("log line is not valid JSON: %v (%q)", err, buf.String())
	}
	if _, ok := rec["trace_id"]; ok {
		t.Error("trace_id present without an active span")
	}
	if _, ok := rec["span_id"]; ok {
		t.Error("span_id present without an active span")
	}
}
