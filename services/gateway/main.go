package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/primaryrutabaga/ruby-core/pkg/boot"
	"github.com/primaryrutabaga/ruby-core/pkg/logging"
	"github.com/primaryrutabaga/ruby-core/services/gateway/app"
)

var (
	version   = "dev"
	commitSHA = "unknown"
)

func main() {
	logger := logging.NewLogger("gateway")
	// Set as the process default so that package-level slog calls (e.g. in pkg/boot)
	// also emit structured JSON without needing a logger parameter.
	slog.SetDefault(logger)

	// LoadConfig uses stdlib log.Fatalf internally — it is called before any
	// business logic and its fatal path is a pre-flight config check, not an
	// operational error. All other fatal paths below use structured logging.
	cfg := boot.LoadConfig("gateway")

	logger.Info("starting gateway", slog.String("version", version), slog.String("commit", commitSHA))

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

	nc, err := boot.ConnectNATS(cfg, "ruby-core-gateway", seed, tlsMat)
	if err != nil {
		logger.Error("nats: connect failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer nc.Close()
	logger.Info("connected to NATS", slog.String("url", cfg.NATSUrl))

	// Fetch Home Assistant credentials from Vault.
	// Non-fatal: if the secret is absent the gateway starts in degraded mode
	// (health endpoint up, HA WebSocket client disabled) and logs a warning.
	haVaultPath := os.Getenv("VAULT_HA_PATH")
	if haVaultPath == "" {
		haVaultPath = "secret/data/ruby-core/ha"
	}
	haCfg, err := boot.FetchHAConfig(cfg.VaultAddr, cfg.VaultToken, haVaultPath)
	if err != nil {
		logger.Warn("vault: HA config unavailable — starting in degraded mode (no HA WebSocket)",
			slog.String("vault_path", haVaultPath),
			slog.String("error", err.Error()),
		)
		haCfg = &boot.HAConfig{} // empty: HA client will not connect
	} else {
		logger.Info("vault: fetched HA config", slog.String("ha_url", haCfg.URL))
	}

	gateway, err := app.New(haCfg.URL, haCfg.Token, nc, logger)
	if err != nil {
		logger.Error("gateway: init failed", slog.String("error", err.Error()))
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
	gateway.Run(ctx, httpAddr)
}
