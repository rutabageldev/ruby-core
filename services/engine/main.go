package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/primaryrutabaga/ruby-core/pkg/audit"
	"github.com/primaryrutabaga/ruby-core/pkg/boot"
	calendarstore "github.com/primaryrutabaga/ruby-core/pkg/calendar/store"
	"github.com/primaryrutabaga/ruby-core/pkg/config"
	"github.com/primaryrutabaga/ruby-core/pkg/idempotency"
	"github.com/primaryrutabaga/ruby-core/pkg/logging"
	"github.com/primaryrutabaga/ruby-core/pkg/natsx"
	rubyotel "github.com/primaryrutabaga/ruby-core/pkg/otel"
	engineconfig "github.com/primaryrutabaga/ruby-core/services/engine/config"
	"github.com/primaryrutabaga/ruby-core/services/engine/processors/ada"
	adastore "github.com/primaryrutabaga/ruby-core/services/engine/processors/ada/store"
	"github.com/primaryrutabaga/ruby-core/services/engine/processors/calendar"
	"github.com/primaryrutabaga/ruby-core/services/engine/processors/presence_notify"
)

var (
	version   = "dev"
	commitSHA = "unknown"
)

func main() {
	logger := logging.NewLogger("engine")
	// Set as the process default so that package-level slog calls (e.g. in pkg/boot,
	// pkg/idempotency) also emit structured JSON without needing a logger parameter.
	slog.SetDefault(logger)

	// Initialize OpenTelemetry (OTLP gRPC export to the Foundation collector). No-op when
	// OTEL_EXPORTER_OTLP_ENDPOINT is unset (dev), so it never blocks or fails startup. The
	// deferred shutdown flushes pending spans/metrics on graceful exit (skipped on os.Exit
	// crash paths, which is acceptable).
	otelShutdown, err := rubyotel.Init(context.Background(), "engine", version)
	if err != nil {
		logger.Warn("otel: init failed, continuing without telemetry", slog.String("error", err.Error()))
	}
	defer func() {
		sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer scancel()
		_ = otelShutdown(sctx)
	}()

	// LoadConfig uses stdlib log.Fatalf internally — it is called before any
	// business logic and its fatal path is a pre-flight config check, not an
	// operational error. All other fatal paths below use structured logging.
	cfg := boot.LoadConfig("engine")

	logger.Info("starting engine", slog.String("version", version), slog.String("commit", commitSHA))

	seed, err := boot.FetchNATSSeed(cfg.VaultAddr, cfg.VaultToken, cfg.VaultNKEYPath)
	if err != nil {
		logger.Error("vault: fetch NATS seed failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("vault: fetched NATS seed", slog.String("path", cfg.VaultNKEYPath))

	// ctx scopes the renewal goroutine started by BootstrapNATSTLS when the
	// direct-PKI path is enabled. Canceling it at shutdown lets RenewLoop exit
	// cleanly. The signal handler installed below cancels it.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nc, err := boot.BootstrapNATSTLS(ctx, cfg, "ruby-core-engine", seed)
	if err != nil {
		logger.Error("nats: bootstrap failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer nc.Close()
	logger.Info("connected to NATS", slog.String("url", cfg.NATSUrl))

	// Exit for a Docker restart if NATS is permanently lost (reconnects exhausted), #18.
	// The cancel also unblocks the DLQ forwarder so wg.Wait() returns instead of hanging.
	var natsLost atomic.Bool
	nc.SetClosedHandler(boot.OnNATSClosed(ctx, cancel, &natsLost, logger))

	// --- Phase 3: JetStream setup ---

	js, err := nc.JetStream()
	if err != nil {
		logger.Error("nats: jetstream context failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if err := natsx.EnsureHAEventsStream(js); err != nil {
		logger.Error("nats: ensure HA_EVENTS stream failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("nats: HA_EVENTS stream ready")

	if err := natsx.EnsureDLQStream(js); err != nil {
		logger.Error("nats: ensure DLQ stream failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("nats: DLQ stream ready")

	// --- Phase 4: Audit stream ---

	if err := natsx.EnsureAuditStream(js); err != nil {
		logger.Error("nats: ensure AUDIT_EVENTS stream failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("nats: AUDIT_EVENTS stream ready")

	if err := natsx.EnsureCommandsStream(js); err != nil {
		logger.Error("nats: ensure COMMANDS stream failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("nats: COMMANDS stream ready")

	if err := natsx.EnsurePresenceStream(js); err != nil {
		logger.Error("nats: ensure PRESENCE stream failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("nats: PRESENCE stream ready")

	// --- Phase 3: Idempotency ---

	kv, err := idempotency.CreateOrBindKVBucket(js, "idempotency", config.DefaultIdempotencyTTL)
	if err != nil {
		logger.Error("nats: idempotency KV bucket failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	idStore := idempotency.NewHybridStore(kv, config.DefaultIdempotencyTTL)
	defer func() { _ = idStore.Close() }()
	logger.Info("idempotency: hybrid store ready", slog.Duration("ttl", config.DefaultIdempotencyTTL))

	// --- Phase 4: Audit publisher ---

	auditPub := audit.NewPublisher(nc, "ruby_engine", logger)
	defer auditPub.Close()
	logger.Info("audit: publisher ready")

	// --- Phase 5: Load rules and publish compiled config to NATS KV ---

	ruleCfg, err := engineconfig.Load()
	if err != nil {
		logger.Error("config: rule loading failed — cannot start without valid rules", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info(
		"config: rules loaded",
		slog.Int("critical_entities", len(ruleCfg.CriticalEntities)),
		slog.Int("passlist_domains", len(ruleCfg.Passlist)),
	)

	configKV, err := natsx.EnsureConfigKV(js)
	if err != nil {
		logger.Error("nats: ensure config KV bucket failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("nats: config KV bucket ready")

	passlistJSON, err := json.Marshal(ruleCfg.Passlist)
	if err != nil {
		logger.Error("config: marshal passlist", slog.String("error", err.Error()))
		os.Exit(1)
	}
	criticalJSON, err := json.Marshal(ruleCfg.CriticalEntities)
	if err != nil {
		logger.Error("config: marshal critical entities", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if _, err := configKV.Put(natsx.KVKeyConfigPasslist, passlistJSON); err != nil {
		logger.Error("nats: publish passlist to config KV", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if _, err := configKV.Put(natsx.KVKeyConfigCriticalEntities, criticalJSON); err != nil {
		logger.Error("nats: publish critical entities to config KV", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("config: passlist and critical entities published to NATS KV")

	// --- Consumer ---

	consumerCfg := natsx.DefaultPullConsumerConfig("HA_EVENTS", "engine_processor", "ha.events.>")
	sub, err := natsx.EnsurePullConsumer(js, consumerCfg)
	if err != nil {
		logger.Error("nats: ensure pull consumer failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info(
		"nats: pull consumer ready",
		slog.String("consumer", "engine_processor"),
		slog.Int("max_ack_pending", consumerCfg.MaxAckPending),
		slog.Duration("ack_wait", consumerCfg.AckWait),
	)

	host := NewProcessorHost(logger)
	host.Register(presence_notify.New(logger))
	host.Register(ada.New(logger))
	host.Register(calendar.New(logger))

	// --- Conditional Postgres boot (ADR-0029) ---
	// If any registered processor implements StatefulProcessor and RequiresStorage,
	// fetch Postgres credentials from Vault, run migrations, and connect the pool.
	// Stateless-only deployments skip this block entirely.

	var pool *pgxpool.Pool
	var haCfg *boot.HAConfig

	if host.RequiresStorage() {
		pgVaultPath := os.Getenv("VAULT_PG_PATH")
		if pgVaultPath == "" {
			pgVaultPath = "secret/data/ruby-core/postgres"
		}
		pgCfg, err := boot.FetchPostgresConfig(cfg.VaultAddr, cfg.VaultToken, pgVaultPath)
		if err != nil {
			logger.Error("vault: fetch postgres config failed", slog.String("error", err.Error()))
			os.Exit(1)
		}

		if err := adastore.MigrateUp(context.Background(), pgCfg.DSN()); err != nil {
			logger.Error("postgres: ada migration failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		logger.Info("postgres: ada migrations applied")

		if err := calendarstore.MigrateUp(context.Background(), pgCfg.DSN()); err != nil {
			logger.Error("postgres: calendar migration failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		logger.Info("postgres: calendar migrations applied")

		pool, err = pgxpool.New(context.Background(), pgCfg.DSN())
		if err != nil {
			logger.Error("postgres: connect failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		defer pool.Close()
		logger.Info("connected to postgres", slog.String("host", pgCfg.Host))

		// HA push gate. All environments share one Home Assistant, so only the
		// prod engine may project Ada sensor state to it — otherwise a non-prod
		// engine clobbers prod's sensors on startup/refresh/safety-net (ADR-0033).
		// Mirrors the gateway's HA_INGEST_ENABLED ingest gate (#72): when false,
		// skip the HA fetch and run with an empty HA client (pushes become no-ops).
		if os.Getenv("HA_INGEST_ENABLED") == "false" {
			logger.Warn("HA push disabled (HA_INGEST_ENABLED=false) — engine will not push Ada sensors to Home Assistant")
			haCfg = &boot.HAConfig{} // empty: ada HA client pushes become no-ops
		} else {
			haVaultPath := os.Getenv("VAULT_HA_PATH")
			if haVaultPath == "" {
				haVaultPath = "secret/data/ruby-core/ha"
			}
			haCfg, err = boot.FetchHAConfig(cfg.VaultAddr, cfg.VaultToken, haVaultPath)
			if err != nil {
				logger.Error("vault: fetch HA config failed", slog.String("error", err.Error()))
				os.Exit(1)
			}
			logger.Info("vault: fetched HA config", slog.String("ha_url", haCfg.URL))
		}
	}

	if err := host.Initialize(ruleCfg, nc, js, pool, haCfg); err != nil {
		logger.Error("processor host: init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer host.Shutdown()

	processFn := host.Process
	if forceFail {
		logger.Warn("ENGINE_FORCE_FAIL is set — all events will be rejected; do not use in production")
		processFn = forceFailProcess
	}

	consumer, err := NewConsumer(sub, idStore, processFn, consumerCfg.WorkerCount, consumerCfg.FetchBatch, consumerCfg.BackOff, logger, auditPub)
	if err != nil {
		logger.Error("consumer init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	dlqFwd, err := NewDLQForwarder(nc, js, "HA_EVENTS", "engine_processor", logger)
	if err != nil {
		logger.Error("dlq forwarder init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// --- PRESENCE consumer (shares processor host) ---

	presenceCfg := natsx.DefaultPullConsumerConfig("PRESENCE", "engine_presence_processor", "ruby_presence.events.>")
	presenceSub, err := natsx.EnsurePullConsumer(js, presenceCfg)
	if err != nil {
		logger.Error("nats: ensure presence pull consumer failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info(
		"nats: presence pull consumer ready",
		slog.String("consumer", "engine_presence_processor"),
	)

	presenceConsumer, err := NewConsumer(presenceSub, idStore, processFn, presenceCfg.WorkerCount, presenceCfg.FetchBatch, presenceCfg.BackOff, logger, auditPub)
	if err != nil {
		logger.Error("presence consumer init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// --- Observability: attach OTel message instruments to both consumers ---
	// nil instruments/counter are safe (no-op); both consumers share one set, labeled
	// per-consumer at record time.
	msgInstr, err := natsx.NewMsgInstruments("engine")
	if err != nil {
		logger.Warn("otel: message instruments unavailable", slog.String("error", err.Error()))
	}
	dedupCtr, err := natsx.NewDedupCounter()
	if err != nil {
		logger.Warn("otel: dedup counter unavailable", slog.String("error", err.Error()))
	}
	consumer.stream, consumer.consumerName = "HA_EVENTS", "engine_processor"
	consumer.instruments, consumer.dedup = msgInstr, dedupCtr
	presenceConsumer.stream, presenceConsumer.consumerName = "PRESENCE", "engine_presence_processor"
	presenceConsumer.instruments, presenceConsumer.dedup = msgInstr, dedupCtr

	// --- Graceful shutdown ---

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sig
		logger.Info("shutting down")
		cancel()
	}()

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		if err := consumer.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("consumer exited with error", slog.String("error", err.Error()))
		}
	}()
	go func() {
		defer wg.Done()
		if err := dlqFwd.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("dlq forwarder exited with error", slog.String("error", err.Error()))
		}
	}()
	go func() {
		defer wg.Done()
		if err := presenceConsumer.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("presence consumer exited with error", slog.String("error", err.Error()))
		}
	}()

	logger.Info(
		"consumer and DLQ forwarder started",
		slog.Int("workers", consumerCfg.WorkerCount),
		slog.Int("batch", consumerCfg.FetchBatch),
	)
	wg.Wait()
	logger.Info("engine stopped")
	if natsLost.Load() {
		os.Exit(1)
	}
}

// ENGINE_FORCE_FAIL: if set to "true" at startup, every event is rejected with
// an error, triggering NAK and DLQ routing.
// This is a manual testing hook for DLQ verification (docs/ops/phase3-verification.md).
// It must never be set in production or added to .env.example.
var forceFail = os.Getenv("ENGINE_FORCE_FAIL") == "true"

// forceFailProcess wraps a process func so every call returns an error.
// Used only when ENGINE_FORCE_FAIL=true.
func forceFailProcess(_ string, _ []byte) error {
	return errors.New("ENGINE_FORCE_FAIL: forced failure for DLQ verification")
}
