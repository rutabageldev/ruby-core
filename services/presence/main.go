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
	logger := logging.NewLogger("presence")
	slog.SetDefault(logger)

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

	tlsMat, err := boot.FetchNATSTLS(cfg.VaultAddr, cfg.VaultToken, cfg.VaultTLSPath)
	if err != nil {
		logger.Error("vault: fetch TLS material failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("vault: fetched TLS material", slog.String("path", cfg.VaultTLSPath))

	nc, err := boot.ConnectNATS(cfg, "ruby-core-presence", seed, tlsMat)
	if err != nil {
		logger.Error("nats: connect failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer nc.Close()
	logger.Info("connected to NATS", slog.String("url", cfg.NATSUrl))

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

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-sig
		logger.Info("shutting down")
		cancel()
	}()

	logger.Info("presence running")
	runConsumer(ctx, sub, h, consumerCfg.FetchBatch, logger)
	logger.Info("presence stopped")
}

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
			log.Warn("presence: fetch error", slog.String("error", err.Error()))
			continue
		}
		for _, msg := range msgs {
			if err := h.process(msg.Subject, msg.Data); err != nil {
				log.Warn("presence: process failed, naking",
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
