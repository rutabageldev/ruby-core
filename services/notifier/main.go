package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/primaryrutabaga/ruby-core/pkg/audit"
	"github.com/primaryrutabaga/ruby-core/pkg/boot"
	"github.com/primaryrutabaga/ruby-core/pkg/logging"
	"github.com/primaryrutabaga/ruby-core/pkg/natsx"
	rubyotel "github.com/primaryrutabaga/ruby-core/pkg/otel"
)

var (
	version   = "dev"
	commitSHA = "unknown"
)

func main() {
	logger := logging.NewLogger("notifier")
	slog.SetDefault(logger)

	// Initialize OpenTelemetry (no-op when OTEL_EXPORTER_OTLP_ENDPOINT is unset).
	otelShutdown, err := rubyotel.Init(context.Background(), "notifier", version)
	if err != nil {
		logger.Warn("otel: init failed, continuing without telemetry", slog.String("error", err.Error()))
	}
	defer func() {
		sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer scancel()
		_ = otelShutdown(sctx)
	}()

	cfg := boot.LoadConfig("notifier")

	logger.Info("starting notifier", slog.String("version", version), slog.String("commit", commitSHA))

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

	nc, err := boot.BootstrapNATSTLS(ctx, cfg, "ruby-core-notifier", seed)
	if err != nil {
		logger.Error("nats: bootstrap failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer nc.Close()
	logger.Info("connected to NATS", slog.String("url", cfg.NATSUrl))

	// Exit for a Docker restart if NATS is permanently lost (reconnects exhausted), #18.
	var natsLost atomic.Bool
	nc.SetClosedHandler(boot.OnNATSClosed(ctx, cancel, &natsLost, logger))

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

	auditPub := audit.NewPublisher(nc, "ruby_notifier", logger)
	defer auditPub.Close()

	h := newHandler(haCfg.URL, haCfg.Token, auditPub, logger)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sig
		logger.Info("shutting down")
		cancel()
	}()

	msgInstr, err := natsx.NewMsgInstruments("notifier")
	if err != nil {
		logger.Warn("otel: message instruments unavailable", slog.String("error", err.Error()))
	}

	logger.Info("notifier running")
	runConsumer(ctx, sub, h, consumerCfg.FetchBatch, "COMMANDS", "notifier_processor", msgInstr, logger)
	logger.Info("notifier stopped")
	if natsLost.Load() {
		os.Exit(1)
	}
}

// runConsumer is a simple pull consumer loop for the notifier. Unlike the engine,
// the notifier does not use idempotency dedup or DLQ routing — notifications are
// best-effort with JetStream redelivery backoff as the only retry mechanism.
func runConsumer(ctx context.Context, sub *nats.Subscription, h *handler, batchSize int, stream, consumer string, instr *natsx.MsgInstruments, log *slog.Logger) {
	for {
		if ctx.Err() != nil {
			return
		}
		msgs, err := sub.Fetch(batchSize, nats.MaxWait(5*nats.DefaultTimeout))
		if err != nil {
			// Survive a NATS bounce (#18): exit only on ctx cancellation; back off on a
			// transient error so we don't hot-spin while nats.go reconnects.
			switch natsx.ClassifyFetchErr(err, ctx.Err()) {
			case natsx.FetchStop:
				return
			case natsx.FetchBackoff:
				log.Warn("notifier: transient fetch error, retrying", slog.String("error", err.Error()))
				select {
				case <-ctx.Done():
					return
				case <-time.After(natsx.FetchRetryBackoff):
				}
			}
			continue
		}
		for _, msg := range msgs {
			m := msg
			instr.Observe(ctx, m, stream, consumer, func(sctx context.Context) string {
				if err := h.process(sctx, m.Subject, m.Data); err != nil {
					log.Warn("notifier: process failed, naking",
						slog.String("subject", m.Subject),
						slog.String("error", err.Error()),
					)
					_ = m.Nak()
					return natsx.OutcomeFailure
				}
				_ = m.Ack()
				return natsx.OutcomeSuccess
			})
		}
	}
}
