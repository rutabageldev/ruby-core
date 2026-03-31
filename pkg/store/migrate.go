// Package store provides shared database utilities for ruby-core services.
// Each processor owns its migrations under its own directory and passes its
// embedded FS and a unique table name to MigrateUp to avoid collisions when
// multiple processors share the same database.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	// Register the pgx5 driver for database/sql so sql.Open("pgx5", dsn) works.
	_ "github.com/jackc/pgx/v5/stdlib"
)

// MigrateUp applies all pending migrations from the given embedded filesystem.
// dsn must be a pgx-compatible connection string (e.g. from boot.PostgresConfig.DSN()).
// tableName must be unique per processor (e.g. "schema_migrations_ada") to avoid
// collisions when multiple processors share the same database.
// Idempotent — safe to call on every engine startup.
//
// Note: golang-migrate's pgx/v5 driver wraps database/sql via pgx/v5/stdlib.
// A separate *sql.DB is opened for the migration step only; the application
// pool (*pgxpool.Pool) is not touched.
func MigrateUp(_ context.Context, fs embed.FS, dir, dsn, tableName string) error {
	db, err := sql.Open("pgx/v5", dsn)
	if err != nil {
		return fmt.Errorf("store: open migration db: %w", err)
	}
	defer func() { _ = db.Close() }()

	src, err := iofs.New(fs, dir)
	if err != nil {
		return fmt.Errorf("store: load migration source: %w", err)
	}

	driver, err := migratepgx.WithInstance(db, &migratepgx.Config{
		MigrationsTable: tableName,
	})
	if err != nil {
		return fmt.Errorf("store: create migrate driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "ruby_core", driver)
	if err != nil {
		return fmt.Errorf("store: create migrator: %w", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("store: run migrations: %w", err)
	}
	return nil
}
