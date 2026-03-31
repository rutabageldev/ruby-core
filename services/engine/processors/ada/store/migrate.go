package store

import (
	"context"
	"embed"

	pkgstore "github.com/primaryrutabaga/ruby-core/pkg/store"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// MigrateUp applies all pending ada schema migrations.
// Uses schema_migrations_ada as the tracking table to avoid collisions
// with future processors that share the same database.
// dsn must be a pgx-compatible connection string (e.g. from boot.PostgresConfig.DSN()).
// Idempotent — safe to call on every engine startup.
func MigrateUp(ctx context.Context, dsn string) error {
	return pkgstore.MigrateUp(ctx, migrationsFS, "migrations", dsn, "schema_migrations_ada")
}
