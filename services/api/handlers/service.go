// Package handlers implements the ogen-generated oas.Handler interface for the
// ruby-core read API. Each domain (calendar, directory, childcare) adds its read
// endpoints here against the shared platform; this slice ships only the meta /ping
// placeholder (PLAN-0030).
package handlers

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Service implements oas.Handler. It holds the read-only Postgres pool that domain
// handlers query; the API never writes (ADR-0040).
type Service struct {
	pool    *pgxpool.Pool
	log     *slog.Logger
	service string
}

// New constructs the read-API handler. service is the short service name reported
// by the liveness endpoint.
func New(pool *pgxpool.Pool, log *slog.Logger, service string) *Service {
	return &Service{pool: pool, log: log, service: service}
}
