package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync/atomic"
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

	// ctx scopes the renewal goroutine started by BootstrapNATSTLS when the
	// direct-PKI path is enabled. Canceling it at shutdown lets RenewLoop exit
	// cleanly. The signal handler installed below cancels it.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nc, err := boot.BootstrapNATSTLS(ctx, cfg, "ruby-core-gateway", seed)
	if err != nil {
		logger.Error("nats: bootstrap failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer nc.Close()
	logger.Info("connected to NATS", slog.String("url", cfg.NATSUrl))

	// Exit for a Docker restart if NATS is permanently lost (reconnects exhausted), #18.
	var natsLost atomic.Bool
	nc.SetClosedHandler(boot.OnNATSClosed(ctx, cancel, &natsLost, logger))

	// Home Assistant ingestion gate. All environments share one Home Assistant,
	// so only the production gateway may consume its event stream (state_changed
	// + ada_event). Non-prod gateways set HA_INGEST_ENABLED=false to stay off it
	// — otherwise every HA event fans out to all environments and is written
	// multiple times. Disabled => empty HA config => degraded mode (no WebSocket),
	// while the HTTP/health endpoints still run.
	var haCfg *boot.HAConfig
	if os.Getenv("HA_INGEST_ENABLED") == "false" {
		logger.Warn("HA ingestion disabled (HA_INGEST_ENABLED=false) — gateway will not connect to Home Assistant")
		haCfg = &boot.HAConfig{} // empty: HA client will not connect
	} else {
		// Fetch Home Assistant credentials from Vault.
		// Non-fatal: if the secret is absent the gateway starts in degraded mode
		// (health endpoint up, HA WebSocket client disabled) and logs a warning.
		haVaultPath := os.Getenv("VAULT_HA_PATH")
		if haVaultPath == "" {
			haVaultPath = "secret/data/ruby-core/ha"
		}
		haCfg, err = boot.FetchHAConfig(cfg.VaultAddr, cfg.VaultToken, haVaultPath)
		if err != nil {
			logger.Warn("vault: HA config unavailable — starting in degraded mode (no HA WebSocket)",
				slog.String("vault_path", haVaultPath),
				slog.String("error", err.Error()),
			)
			haCfg = &boot.HAConfig{} // empty: HA client will not connect
		} else {
			logger.Info("vault: fetched HA config", slog.String("ha_url", haCfg.URL))
		}
	}

	gateway, err := app.New(haCfg.URL, haCfg.Token, nc, logger)
	if err != nil {
		logger.Error("gateway: init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

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
	if natsLost.Load() {
		os.Exit(1)
	}
}
