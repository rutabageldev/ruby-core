package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/primaryrutabaga/ruby-core/pkg/audit"
	"github.com/primaryrutabaga/ruby-core/pkg/boot"
	"github.com/primaryrutabaga/ruby-core/pkg/config"
	"github.com/primaryrutabaga/ruby-core/pkg/idempotency"
	"github.com/primaryrutabaga/ruby-core/pkg/logging"
	"github.com/primaryrutabaga/ruby-core/pkg/natsx"
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

	tlsMat, err := boot.FetchNATSTLS(cfg.VaultAddr, cfg.VaultToken, cfg.VaultTLSPath)
	if err != nil {
		logger.Error("vault: fetch TLS material failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("vault: fetched TLS material", slog.String("path", cfg.VaultTLSPath))

	nc, err := boot.ConnectNATS(cfg, "ruby-core-engine", seed, tlsMat)
	if err != nil {
		logger.Error("nats: connect failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer nc.Close()
	logger.Info("connected to NATS", slog.String("url", cfg.NATSUrl))

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

	// --- Consumer ---

	consumerCfg := natsx.DefaultPullConsumerConfig("HA_EVENTS", "engine_processor", "ha.events.>")
	sub, err := natsx.EnsurePullConsumer(js, consumerCfg)
	if err != nil {
		logger.Error("nats: ensure pull consumer failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("nats: pull consumer ready",
		slog.String("consumer", "engine_processor"),
		slog.Int("max_ack_pending", consumerCfg.MaxAckPending),
		slog.Duration("ack_wait", consumerCfg.AckWait),
	)

	consumer, err := NewConsumer(sub, idStore, processEvent, consumerCfg.WorkerCount, consumerCfg.FetchBatch, consumerCfg.BackOff, logger, auditPub)
	if err != nil {
		logger.Error("consumer init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	dlqFwd, err := NewDLQForwarder(nc, js, "HA_EVENTS", "engine_processor", logger)
	if err != nil {
		logger.Error("dlq forwarder init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// --- Graceful shutdown ---

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-sig
		logger.Info("shutting down")
		cancel()
	}()

	var wg sync.WaitGroup
	wg.Add(2)

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

	logger.Info("consumer and DLQ forwarder started",
		slog.Int("workers", consumerCfg.WorkerCount),
		slog.Int("batch", consumerCfg.FetchBatch),
	)
	wg.Wait()
	logger.Info("engine stopped")
}

// processEvent is the Phase 3 stub message processor.
// It logs the raw payload and returns nil.
//
// ENGINE_FORCE_FAIL: if set to "true" at startup, every message fails with an error.
// This is a manual testing hook for DLQ verification (docs/ops/phase3-verification.md).
// It must never be set in production or added to .env.example.
var forceFail = os.Getenv("ENGINE_FORCE_FAIL") == "true"

func processEvent(data []byte) error {
	if forceFail {
		return errors.New("ENGINE_FORCE_FAIL: forced failure for DLQ verification")
	}
	slog.Info("event received — Phase 5 TODO: implement rule engine",
		slog.Int("bytes", len(data)),
	)
	return nil
}
