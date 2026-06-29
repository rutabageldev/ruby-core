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
	logger := logging.NewLogger("presence")
	slog.SetDefault(logger)

	// Initialize OpenTelemetry (no-op when OTEL_EXPORTER_OTLP_ENDPOINT is unset).
	otelShutdown, err := rubyotel.Init(context.Background(), "presence", version)
	if err != nil {
		logger.Warn("otel: init failed, continuing without telemetry", slog.String("error", err.Error()))
	}
	defer func() {
		sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer scancel()
		_ = otelShutdown(sctx)
	}()

	cfg := boot.LoadConfig("presence")

	logger.Info("starting presence", slog.String("version", version), slog.String("commit", commitSHA))

	presenceCfg, err := LoadPresenceConfig()
	if err != nil {
		logger.Error("config: presence config invalid", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("presence: config loaded",
		slog.String("person_id", presenceCfg.PersonID),
		slog.String("phone_entity", presenceCfg.PhoneEntity),
		slog.String("wifi_entity", presenceCfg.WifiEntity),
		slog.Duration("debounce", presenceCfg.DebounceDur),
	)

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

	nc, err := boot.BootstrapNATSTLS(ctx, cfg, "ruby-core-presence", seed)
	if err != nil {
		logger.Error("nats: bootstrap failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer nc.Close()
	logger.Info("connected to NATS", slog.String("url", cfg.NATSUrl))

	// Exit for a Docker restart if NATS is permanently lost (reconnects exhausted), #18.
	var natsLost atomic.Bool
	nc.SetClosedHandler(boot.OnNATSClosed(ctx, cancel, &natsLost, logger))

	haVaultPath := os.Getenv("VAULT_HA_PATH")
	if haVaultPath == "" {
		haVaultPath = "secret/data/ruby-core/ha"
	}
	haCfg, err := boot.FetchHAConfig(cfg.VaultAddr, cfg.VaultToken, haVaultPath)
	if err != nil {
		logger.Warn("vault: HA config unavailable — WiFi corroboration will treat all uncertain states as not connected",
			slog.String("vault_path", haVaultPath),
			slog.String("error", err.Error()),
		)
		haCfg = &boot.HAConfig{}
	} else {
		logger.Info("vault: fetched HA config", slog.String("ha_url", haCfg.URL))
	}

	js, err := nc.JetStream()
	if err != nil {
		logger.Error("nats: jetstream context failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	kv, err := natsx.EnsurePresenceKV(js)
	if err != nil {
		logger.Error("nats: ensure presence KV failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("nats: presence KV ready")

	h := newHandler(presenceCfg, haCfg.URL, haCfg.Token, nc, kv, logger)
	h.initState()

	// Filter subject: ha.events.{domain}.{name}
	filterSubject := "ha.events." + presenceCfg.phoneEntityDomain() + "." + presenceCfg.phoneEntityName()
	durableName := "presence_" + presenceCfg.PersonID

	consumerCfg := natsx.DefaultPullConsumerConfig("HA_EVENTS", durableName, filterSubject)
	sub, err := natsx.EnsurePullConsumer(js, consumerCfg)
	if err != nil {
		logger.Error("nats: ensure pull consumer failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("nats: pull consumer ready",
		slog.String("consumer", durableName),
		slog.String("filter", filterSubject),
	)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sig
		logger.Info("shutting down")
		cancel()
	}()

	logger.Info("presence running")
	msgInstr, err := natsx.NewMsgInstruments("presence")
	if err != nil {
		logger.Warn("otel: message instruments unavailable", slog.String("error", err.Error()))
	}

	runConsumer(ctx, sub, h, consumerCfg.FetchBatch, consumerCfg.Stream, consumerCfg.Durable, msgInstr, logger)
	logger.Info("presence stopped")
	if natsLost.Load() {
		os.Exit(1)
	}
}

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
				log.Warn("presence: transient fetch error, retrying", slog.String("error", err.Error()))
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
			instr.Observe(ctx, m, stream, consumer, func(_ context.Context) string {
				if err := h.process(m.Subject, m.Data); err != nil {
					log.Warn("presence: process failed, naking",
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
