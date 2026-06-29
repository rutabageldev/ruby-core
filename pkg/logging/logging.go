// Package logging provides a shared structured logger factory for Ruby Core services.
// All services call NewLogger to obtain a JSON-format slog.Logger pre-tagged with
// a "service" attribute.
//
// Calling slog.SetDefault(logger) in main() ensures that package-level slog functions
// (e.g., slog.Info, slog.Warn) throughout the process — including internal helpers in
// pkg/boot and pkg/idempotency — automatically emit structured JSON.
//
// Log-trace correlation (ADR-0004, PLAN-0009): the handler is wrapped by traceHandler,
// which injects trace_id/span_id from the active span in the record's context whenever a
// log call happens inside a span. We deliberately keep the stdout JSON handler rather than
// swapping to the otelslog OTLP bridge: the Foundation promtail scrapes container stdout
// into Loki, and the smoke/stability scripts grep `docker logs`, so log lines MUST stay on
// stdout. Correlation is achieved by stamping the IDs onto the JSON, not by re-routing logs
// over OTLP. With no active span (or no otel.Init), the fields are simply omitted — no-op.
package logging

import (
	"context"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"
)

// NewLogger returns a JSON-format structured logger pre-configured with a service attribute
// and automatic trace_id/span_id correlation.
func NewLogger(service string) *slog.Logger {
	base := slog.NewJSONHandler(os.Stdout, nil)
	return slog.New(&traceHandler{inner: base}).
		With(slog.String("service", service))
}

// traceHandler decorates an slog.Handler, adding trace_id and span_id from the active span
// (if any) in the record's context. When there is no recording span, it adds nothing and
// behaves exactly like the wrapped handler.
type traceHandler struct {
	inner slog.Handler
}

func (h *traceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *traceHandler) Handle(ctx context.Context, rec slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		rec.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.inner.Handle(ctx, rec)
}

func (h *traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &traceHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *traceHandler) WithGroup(name string) slog.Handler {
	return &traceHandler{inner: h.inner.WithGroup(name)}
}
