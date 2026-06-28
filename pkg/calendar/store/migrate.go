package store

import (
	"context"
	"embed"

	pkgstore "github.com/primaryrutabaga/ruby-core/pkg/store"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// MigrateUp applies all pending calendar + household-overlay schema migrations.
// Uses schema_migrations_calendar as the tracking table to avoid collisions with
// other processors that share the same database (ADR-0029).
// dsn must be a pgx-compatible connection string (e.g. from boot.PostgresConfig.DSN()).
// Idempotent — safe to call on every engine startup. Owned by the engine; the read
// API never migrates (ADR-0040).
func MigrateUp(ctx context.Context, dsn string) error {
	return pkgstore.MigrateUp(ctx, migrationsFS, "migrations", dsn, "schema_migrations_calendar")
}
