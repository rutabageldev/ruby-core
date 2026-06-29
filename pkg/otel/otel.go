// Package otel initializes OpenTelemetry for ruby-core services: OTLP gRPC export of
// traces and metrics to the Foundation OTel Collector, plus the global W3C propagator
// for cross-service trace context (ADR-0004, PLAN-0009). It degrades to no-op providers
// when OTEL_EXPORTER_OTLP_ENDPOINT is unset (dev), so a service never fails to start
// because the Collector is absent.
package otel

import (
	"context"
	"errors"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// noop is a shutdown that does nothing — returned on the dev/no-Collector path.
func noop(context.Context) error { return nil }

// Init sets the global TracerProvider, MeterProvider, and TextMapPropagator, and returns
// a shutdown func that flushes and closes the providers — callers MUST defer it.
//
// If OTEL_EXPORTER_OTLP_ENDPOINT is empty, Init installs no-op providers (only the
// propagator is set, so trace context still round-trips) and returns a no-op shutdown
// with a nil error — the dev path. When the endpoint is set, the OTLP gRPC exporters
// connect lazily (no blocking dial), so an unreachable Collector never hangs startup;
// export failures are background-logged by the SDK and self-heal on reconnect.
func Init(ctx context.Context, serviceName, serviceVersion string) (func(context.Context) error, error) {
	// Global regardless of export, so trace context injects/extracts across services
	// even in no-op (dev) mode.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		// Dev / no Collector: leave the global no-op providers in place.
		return noop, nil
	}

	env := os.Getenv("ENVIRONMENT")
	if env == "" {
		env = "development"
	}
	// Schemaless avoids schema-URL merge conflicts with resource.Default(); the
	// Collector reads these as service_name / service_version / deployment_environment.
	res := resource.NewSchemaless(
		attribute.String("service.name", serviceName),
		attribute.String("service.version", serviceVersion),
		attribute.String("deployment.environment", env),
	)

	traceExp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return noop, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	metricExp, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint(endpoint),
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		return tp.Shutdown, err
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	return func(c context.Context) error {
		return errors.Join(tp.Shutdown(c), mp.Shutdown(c))
	}, nil
}
