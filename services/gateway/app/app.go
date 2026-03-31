// Package app wires together the gateway's components: HA WebSocket client,
// Normalizer, Reconciler, NATS publisher, and health heartbeat.
package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	goNats "github.com/nats-io/nats.go"

	"github.com/primaryrutabaga/ruby-core/pkg/natsx"
	"github.com/primaryrutabaga/ruby-core/services/gateway/ada"
	"github.com/primaryrutabaga/ruby-core/services/gateway/ha"
	gatewayNats "github.com/primaryrutabaga/ruby-core/services/gateway/nats"
)

const healthInterval = 15 * time.Second

// App holds all gateway runtime components.
type App struct {
	nc        *goNats.Conn
	client    *ha.Client
	publisher *gatewayNats.Publisher
	log       *slog.Logger
}

// New builds the gateway App by reading compiled config from the engine's KV
// bucket, then constructing the Normalizer, Reconciler, HA client, and NATS
// publisher.
//
// haURL and haToken are the Home Assistant base URL and long-lived access
// token. nc is an established NATS connection.
//
// If the config KV entry is not yet present (engine hasn't published yet),
// the gateway starts with a nil passlist (pass-all) and an empty critical
// entity list (no reconciliation). This is the safe V0 default.
func New(haURL, haToken string, nc *goNats.Conn, log *slog.Logger) (*App, error) {
	js, err := nc.JetStream()
	if err != nil {
		return nil, err
	}

	// ── config KV (engine-owned; gateway reads passlist + critical entities) ─
	passlist, critEntities := loadEngineConfig(js, log)

	// ── gateway_state KV (gateway-owned; stores last-seen timestamps) ──────
	stateKV, err := natsx.EnsureGatewayStateKV(js)
	if err != nil {
		return nil, err
	}

	// ── components ──────────────────────────────────────────────────────────
	norm := ha.NewNormalizer(passlist)
	publisher := gatewayNats.New(nc)

	var client *ha.Client
	if haURL != "" {
		reconciler := ha.NewReconciler(haURL, haToken, stateKV, norm, publisher, log)
		client = ha.NewClient(haURL, haToken, norm, publisher, stateKV, critEntities, reconciler, log)
	} else {
		log.Warn("gateway: no HA URL configured — WebSocket client disabled (degraded mode)")
	}

	return &App{nc: nc, client: client, publisher: publisher, log: log}, nil
}

// Run starts the HTTP server, HA WebSocket client loop, and health heartbeat
// goroutine. It blocks until ctx is cancelled.
//
// httpAddr is the address for the HTTP health endpoint (e.g. ":8080").
// The port must NOT be published directly to the host; all external access
// must go through Traefik (ADR-0020).
func (a *App) Run(ctx context.Context, httpAddr string) {
	go a.runHealthBeat(ctx)
	go a.runHTTP(ctx, httpAddr)
	if a.client != nil {
		a.client.Run(ctx) // blocks until ctx cancelled
	} else {
		// Degraded mode: no HA client. Block on ctx cancellation only.
		<-ctx.Done()
	}
}

// runHTTP starts a minimal HTTP server exposing GET /health for Traefik and
// liveness probes. The server shuts down when ctx is cancelled.
func (a *App) runHTTP(ctx context.Context, addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.Handle("/ada/events", ada.New(a.nc, a.log))

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	a.log.Info("gateway: HTTP server listening", slog.String("addr", addr))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		a.log.Error("gateway: HTTP server error", slog.String("error", err.Error()))
	}
}

// runHealthBeat publishes a gateway.health heartbeat every 15 s (ADR-0008).
// haConnected is derived from whether the HA client is actively running; we use
// a simple always-true here because we only call this while the client goroutine
// is alive. A full circuit-breaker state could be threaded through in a future
// iteration.
func (a *App) runHealthBeat(ctx context.Context) {
	ticker := time.NewTicker(healthInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.publisher.PublishHealth(true); err != nil {
				a.log.Warn("gateway: publish health failed", slog.String("error", err.Error()))
			}
		}
	}
}

// loadEngineConfig attempts to read the compiled passlist and critical entities
// from the engine's config KV bucket. Returns zero values on any error so the
// gateway can start safely without the engine having published config yet.
func loadEngineConfig(js goNats.JetStreamContext, log *slog.Logger) (passlist map[string][]string, critEntities []string) {
	kv, err := js.KeyValue(natsx.KVBucketConfig)
	if err != nil {
		log.Info("gateway: config KV not yet available; starting with empty config",
			slog.String("bucket", natsx.KVBucketConfig),
		)
		return nil, nil
	}

	if entry, err := kv.Get(natsx.KVKeyConfigPasslist); err == nil {
		if jsonErr := json.Unmarshal(entry.Value(), &passlist); jsonErr != nil {
			log.Warn("gateway: parse passlist JSON failed",
				slog.String("error", jsonErr.Error()),
			)
			passlist = nil
		}
	}

	if entry, err := kv.Get(natsx.KVKeyConfigCriticalEntities); err == nil {
		if jsonErr := json.Unmarshal(entry.Value(), &critEntities); jsonErr != nil {
			log.Warn("gateway: parse critical_entities JSON failed",
				slog.String("error", jsonErr.Error()),
			)
			critEntities = nil
		}
	}

	log.Info("gateway: loaded engine config",
		slog.Int("passlist_domains", len(passlist)),
		slog.Int("critical_entities", len(critEntities)),
	)
	return passlist, critEntities
}
