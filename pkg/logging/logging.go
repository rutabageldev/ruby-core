// Package logging provides a shared structured logger factory for Ruby Core services.
// All services call NewLogger to obtain a JSON-format slog.Logger pre-tagged with
// a "service" attribute.
//
// Calling slog.SetDefault(logger) in main() ensures that package-level slog functions
// (e.g., slog.Info, slog.Warn) throughout the process — including internal helpers in
// pkg/boot and pkg/idempotency — automatically emit structured JSON.
//
// Phase 7 migration path: replace slog.NewJSONHandler with the OTel SDK bridge
// (go.opentelemetry.io/contrib/bridges/otelslog) in this single file to enable
// log-trace correlation without touching any service code (ADR-0004).
package logging

import (
	"log/slog"
	"os"
)

// NewLogger returns a JSON-format structured logger pre-configured with a service attribute.
func NewLogger(service string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, nil)).
		With(slog.String("service", service))
}
