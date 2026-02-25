package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/primaryrutabaga/ruby-core/pkg/boot"
	"github.com/primaryrutabaga/ruby-core/pkg/config"
	"github.com/primaryrutabaga/ruby-core/pkg/logging"
	"github.com/primaryrutabaga/ruby-core/pkg/natsx"
)

var (
	version   = "dev"
	commitSHA = "unknown"
)

// defaultAuditDataDir is the default directory for the NDJSON archive.
// Overridable via AUDIT_DATA_DIR environment variable.
const defaultAuditDataDir = "/data/audit"

func main() {
	logger := logging.NewLogger("audit-sink")
	// Set as the process default so that package-level slog calls (e.g. in pkg/boot)
	// also emit structured JSON without needing a logger parameter.
	slog.SetDefault(logger)

	// LoadConfig uses stdlib log.Fatalf internally — it is called before any
	// business logic and its fatal path is a pre-flight config check, not an
	// operational error. All other fatal paths below use structured logging.
	cfg := boot.LoadConfig("audit-sink")

	logger.Info("starting audit-sink", slog.String("version", version), slog.String("commit", commitSHA))

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

	nc, err := boot.ConnectNATS(cfg, "ruby-core-audit-sink", seed, tlsMat)
	if err != nil {
		logger.Error("nats: connect failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer nc.Close()
	logger.Info("connected to NATS", slog.String("url", cfg.NATSUrl))

	js, err := nc.JetStream()
	if err != nil {
		logger.Error("nats: jetstream context failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// EnsureAuditStream is idempotent; the engine also calls it on startup.
	// Calling it here ensures the stream exists even if the engine is not running.
	if err := natsx.EnsureAuditStream(js); err != nil {
		logger.Error("nats: ensure AUDIT_EVENTS stream failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("nats: AUDIT_EVENTS stream ready")

	// Set up pull consumer on AUDIT_EVENTS.
	consumerCfg := natsx.PullConsumerConfig{
		Stream:        "AUDIT_EVENTS",
		Durable:       "audit_sink_consumer",
		FilterSubject: "audit.>",
		MaxDeliver:    config.DefaultMaxDeliver,
		MaxAckPending: config.DefaultAuditSinkMaxAckPending,
		AckWait:       config.DefaultAckWait,
		BackOff:       config.DefaultBackOff,
		WorkerCount:   config.DefaultAuditSinkWorkerCount,
		FetchBatch:    config.DefaultAuditSinkFetchBatch,
	}
	sub, err := natsx.EnsurePullConsumer(js, consumerCfg)
	if err != nil {
		logger.Error("nats: ensure pull consumer failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("nats: pull consumer ready",
		slog.String("consumer", "audit_sink_consumer"),
		slog.Int("max_ack_pending", consumerCfg.MaxAckPending),
	)

	// Open NDJSON writer.
	dataDir := envOrDefault("AUDIT_DATA_DIR", defaultAuditDataDir)
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		logger.Error("mkdir failed", slog.String("path", dataDir), slog.String("error", err.Error()))
		os.Exit(1)
	}
	auditFile := filepath.Join(dataDir, "audit.ndjson")
	writer, err := NewNDJSONWriter(auditFile)
	if err != nil {
		logger.Error("open audit file failed", slog.String("file", auditFile), slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() { _ = writer.Close() }()
	logger.Info("audit-sink: writer ready", slog.String("file", auditFile))

	// Graceful shutdown.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-sig
		logger.Info("shutting down")
		cancel()
	}()

	logger.Info("audit-sink: starting fetch loop",
		slog.Int("workers", consumerCfg.WorkerCount),
		slog.Int("batch", consumerCfg.FetchBatch),
	)

	if err := runFetchLoop(ctx, sub, writer, consumerCfg, logger); err != nil {
		logger.Error("fetch loop exited with error", slog.String("error", err.Error()))
	}
	logger.Info("audit-sink stopped")
}

// runFetchLoop is the main message processing loop for the audit-sink.
// It fetches batches of audit events and writes each to the NDJSON file.
// Write failures are logged but do not cause a NAK — audit-sink always ACKs to
// avoid retry loops on persistent filesystem errors.
func runFetchLoop(ctx context.Context, sub *nats.Subscription, writer *NDJSONWriter, cfg natsx.PullConsumerConfig, logger *slog.Logger) error {
	sem := make(chan struct{}, cfg.WorkerCount)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		msgs, err := sub.Fetch(cfg.FetchBatch, nats.MaxWait(2*time.Second))
		if err != nil {
			if errors.Is(err, nats.ErrTimeout) {
				continue
			}
			if errors.Is(err, context.Canceled) ||
				errors.Is(err, nats.ErrConnectionClosed) ||
				errors.Is(err, nats.ErrSubscriptionClosed) {
				return nil
			}
			return fmt.Errorf("audit-sink: fetch: %w", err)
		}

		for _, msg := range msgs {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return nil
			}
			go func(m *nats.Msg) {
				defer func() { <-sem }()
				handleAuditMsg(m, writer, logger)
			}(msg)
		}
	}
}

// handleAuditMsg writes the message payload to the NDJSON file and ACKs.
// Write failures are logged but the message is still ACKed to prevent retry loops
// on persistent filesystem errors (disk full, etc.). The AUDIT_EVENTS stream
// retains messages for 72h as a recovery window.
func handleAuditMsg(msg *nats.Msg, writer *NDJSONWriter, logger *slog.Logger) {
	if err := writer.Write(msg.Data); err != nil {
		logger.Warn("audit-sink: write failed, acking to avoid retry loop",
			slog.String("subject", msg.Subject),
			slog.String("error", err.Error()),
		)
	} else {
		logger.Info("audit-sink: event archived",
			slog.String("subject", msg.Subject),
			slog.Int("bytes", len(msg.Data)),
		)
	}
	_ = msg.Ack()
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
