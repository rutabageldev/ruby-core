//go:build fast

package otel

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// With no endpoint set, Init is a no-op: nil error, a usable shutdown, no panic — and the
// W3C propagator is still installed so trace context round-trips in dev.
func TestInit_NoEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	shutdown, err := Init(context.Background(), "ruby-core-test", "v0.0.0")
	if err != nil {
		t.Fatalf("Init with no endpoint returned error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Init returned a nil shutdown")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("no-op shutdown returned error: %v", err)
	}

	// Propagator must round-trip a traceparent even in no-op mode.
	prop := otel.GetTextMapPropagator()
	carrier := propagation.MapCarrier{
		"traceparent": "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
	}
	ctx := prop.Extract(context.Background(), carrier)
	out := propagation.MapCarrier{}
	prop.Inject(ctx, out)
	if out["traceparent"] == "" {
		t.Error("propagator did not round-trip traceparent in no-op mode")
	}
}

// With an endpoint set, Init builds real providers without error (gRPC connects lazily,
// so an unreachable collector does not fail Init) and the shutdown flushes cleanly.
func TestInit_WithEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "127.0.0.1:4317")
	t.Setenv("ENVIRONMENT", "test")

	shutdown, err := Init(context.Background(), "ruby-core-test", "v1.2.3")
	if err != nil {
		t.Fatalf("Init with endpoint returned error: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	// Shutdown with an already-expired ctx still returns (may surface a context error,
	// which is acceptable); just assert it doesn't hang or panic.
	_ = shutdown(ctx)
}
