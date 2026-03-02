package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/nats-io/nats.go"

	"github.com/primaryrutabaga/ruby-core/pkg/boot"
	"github.com/primaryrutabaga/ruby-core/pkg/logging"
	"github.com/primaryrutabaga/ruby-core/pkg/natsx"
)

var (
	version   = "dev"
	commitSHA = "unknown"
)

func main() {
	logger := logging.NewLogger("notifier")
	slog.SetDefault(logger)

	cfg := boot.LoadConfig("notifier")

	logger.Info("starting notifier", slog.String("version", version), slog.String("commit", commitSHA))

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

	nc, err := boot.ConnectNATS(cfg, "ruby-core-notifier", seed, tlsMat)
	if err != nil {
		logger.Error("nats: connect failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer nc.Close()
	logger.Info("connected to NATS", slog.String("url", cfg.NATSUrl))

	// Fetch Home Assistant credentials from Vault.
	// Non-fatal: if absent, the notifier starts but all notifications will log
	// a warning and NAK (redelivery) until the secret is added to Vault.
	haVaultPath := os.Getenv("VAULT_HA_PATH")
	if haVaultPath == "" {
		haVaultPath = "secret/data/ruby-core/ha"
	}
	haCfg, err := boot.FetchHAConfig(cfg.VaultAddr, cfg.VaultToken, haVaultPath)
	if err != nil {
		logger.Warn("vault: HA config unavailable — notifications will fail until secret is added",
			slog.String("vault_path", haVaultPath),
			slog.String("error", err.Error()),
		)
		haCfg = &boot.HAConfig{} // empty: handler will log and NAK each command
	} else {
		logger.Info("vault: fetched HA config", slog.String("ha_url", haCfg.URL))
	}

	js, err := nc.JetStream()
	if err != nil {
		logger.Error("nats: jetstream context failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	consumerCfg := natsx.DefaultPullConsumerConfig("COMMANDS", "notifier_processor", "ruby_engine.commands.notify.>")
	sub, err := natsx.EnsurePullConsumer(js, consumerCfg)
	if err != nil {
		logger.Error("nats: ensure pull consumer failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("nats: pull consumer ready", slog.String("consumer", "notifier_processor"))

	h := newHandler(haCfg.URL, haCfg.Token, logger)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-sig
		logger.Info("shutting down")
		cancel()
	}()

	logger.Info("notifier running")
	runConsumer(ctx, sub, h, consumerCfg.FetchBatch, logger)
	logger.Info("notifier stopped")
}

// runConsumer is a simple pull consumer loop for the notifier. Unlike the engine,
// the notifier does not use idempotency dedup or DLQ routing — notifications are
// best-effort with JetStream redelivery backoff as the only retry mechanism.
func runConsumer(ctx context.Context, sub *nats.Subscription, h *handler, batchSize int, log *slog.Logger) {
	for {
		if ctx.Err() != nil {
			return
		}
		msgs, err := sub.Fetch(batchSize, nats.MaxWait(5*nats.DefaultTimeout))
		if err != nil {
			if errors.Is(err, nats.ErrTimeout) || errors.Is(err, context.Canceled) {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			log.Warn("notifier: fetch error", slog.String("error", err.Error()))
			continue
		}
		for _, msg := range msgs {
			if err := h.process(msg.Subject, msg.Data); err != nil {
				log.Warn("notifier: process failed, naking",
					slog.String("subject", msg.Subject),
					slog.String("error", err.Error()),
				)
				_ = msg.Nak()
			} else {
				_ = msg.Ack()
			}
		}
	}
}
