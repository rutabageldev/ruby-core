package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/primaryrutabaga/ruby-core/pkg/boot"
	"github.com/primaryrutabaga/ruby-core/pkg/logging"
	"github.com/primaryrutabaga/ruby-core/services/api/app"
)

var (
	version   = "dev"
	commitSHA = "unknown"
)

func main() {
	logger := logging.NewLogger("api")
	slog.SetDefault(logger)

	cfg := boot.LoadConfig("api")
	logger.Info("starting api", slog.String("version", version), slog.String("commit", commitSHA))

	// Read-only Postgres. The API never writes and never runs migrations (ADR-0040);
	// it connects with a SELECT-only role provisioned at VAULT_PG_PATH.
	pgPath := os.Getenv("VAULT_PG_PATH")
	if pgPath == "" {
		pgPath = "secret/data/ruby-core/postgres_readonly"
	}
	pgCfg, err := boot.FetchPostgresConfig(cfg.VaultAddr, cfg.VaultToken, pgPath)
	if err != nil {
		logger.Error("vault: fetch read-only postgres config failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	pool, err := pgxpool.New(context.Background(), pgCfg.DSN())
	if err != nil {
		logger.Error("postgres: connect failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer pool.Close()
	logger.Info("postgres: read-only pool ready", slog.String("db", pgCfg.DBName))

	// In-app bearer token (defense-in-depth behind Traefik edge auth + mTLS, ADR-0040).
	tokenPath := os.Getenv("VAULT_API_TOKEN_PATH")
	if tokenPath == "" {
		tokenPath = "secret/data/ruby-core/api"
	}
	bearer, err := boot.FetchKVField(cfg.VaultAddr, cfg.VaultToken, tokenPath, "token")
	if err != nil {
		logger.Error("vault: fetch api bearer token failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("vault: fetched api bearer token", slog.String("path", tokenPath))

	application, err := app.New(pool, bearer, logger)
	if err != nil {
		logger.Error("api: init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		logger.Info("shutting down")
		cancel()
	}()

	httpAddr := os.Getenv("HTTP_ADDR")
	if httpAddr == "" {
		httpAddr = ":8080"
	}
	if err := application.Run(ctx, httpAddr); err != nil {
		logger.Error("api: server error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
