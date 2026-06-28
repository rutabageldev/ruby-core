// Package app wires the ruby-core read API: the ogen-generated server (mounted
// under /v1), the in-app bearer auth, RFC 9457 errors, the unauthenticated health
// probe, and the docs/raw-spec routes. See ADR-0040 and PLAN-0030.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/primaryrutabaga/ruby-core/services/api/handlers"
	"github.com/primaryrutabaga/ruby-core/services/api/oas"
)

// App is the assembled HTTP application.
type App struct {
	handler http.Handler
	log     *slog.Logger
}

// New builds the read API over a read-only Postgres pool and the in-app bearer token.
func New(pool *pgxpool.Pool, bearerToken string, log *slog.Logger) (*App, error) {
	svc := handlers.New(pool, log, "api")
	auth := newTokenAuth(bearerToken)

	oasServer, err := oas.NewServer(svc, auth)
	if err != nil {
		return nil, fmt.Errorf("ogen: new server: %w", err)
	}

	mux := http.NewServeMux()

	// Unauthenticated liveness probe, kept outside the versioned generated surface
	// so Traefik and Uptime Kuma can poll it (ADR-0040).
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Generated API. ogen routes bare spec paths (e.g. /ping); the /v1 version
	// prefix is stripped here so the spec stays version-agnostic per path.
	mux.Handle("/v1/", http.StripPrefix("/v1", oasServer))

	// Operator docs + raw spec, behind the same bearer (ADR-0040).
	mux.Handle("/openapi.yaml", auth.middleware(http.HandlerFunc(serveOpenAPI)))
	mux.Handle("/docs", auth.middleware(http.HandlerFunc(serveDocs)))

	return &App{handler: mux, log: log}, nil
}

// Run serves until ctx is cancelled, then shuts down gracefully.
func (a *App) Run(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:         addr,
		Handler:      a.handler,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	go func() { //nolint:gosec // G118: parent ctx is already cancelled at shutdown; Shutdown needs a fresh deadline
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	a.log.Info("api: http server listening", slog.String("addr", addr))
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Handler exposes the assembled mux for tests.
func (a *App) Handler() http.Handler { return a.handler }
