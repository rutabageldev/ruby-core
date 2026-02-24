package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/primaryrutabaga/ruby-core/pkg/boot"
	"github.com/primaryrutabaga/ruby-core/pkg/config"
	"github.com/primaryrutabaga/ruby-core/pkg/idempotency"
	"github.com/primaryrutabaga/ruby-core/pkg/natsx"
)

var (
	version   = "dev"
	commitSHA = "unknown"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[engine] ")

	cfg := boot.LoadConfig("engine")

	log.Printf("starting engine service version=%s commit=%s", version, commitSHA)

	seed, err := boot.FetchNATSSeed(cfg.VaultAddr, cfg.VaultToken, cfg.VaultNKEYPath)
	if err != nil {
		log.Fatalf("vault: %v", err)
	}
	log.Printf("vault: fetched NATS seed from %s", cfg.VaultNKEYPath)

	tlsMat, err := boot.FetchNATSTLS(cfg.VaultAddr, cfg.VaultToken, cfg.VaultTLSPath)
	if err != nil {
		log.Fatalf("vault: %v", err)
	}
	log.Printf("vault: fetched TLS material from %s", cfg.VaultTLSPath)

	nc, err := boot.ConnectNATS(cfg, "ruby-core-engine", seed, tlsMat)
	if err != nil {
		log.Fatalf("nats: %v", err)
	}
	defer nc.Close()
	log.Printf("connected to NATS at %s", cfg.NATSUrl)

	// --- Phase 3: JetStream setup ---

	js, err := nc.JetStream()
	if err != nil {
		log.Fatalf("nats: jetstream: %v", err)
	}

	if err := natsx.EnsureHAEventsStream(js); err != nil {
		log.Fatalf("nats: ensure HA_EVENTS stream: %v", err)
	}
	log.Printf("nats: HA_EVENTS stream ready")

	if err := natsx.EnsureDLQStream(js); err != nil {
		log.Fatalf("nats: ensure DLQ stream: %v", err)
	}
	log.Printf("nats: DLQ stream ready")

	kv, err := idempotency.CreateOrBindKVBucket(js, "idempotency", config.DefaultIdempotencyTTL)
	if err != nil {
		log.Fatalf("nats: idempotency KV bucket: %v", err)
	}
	idStore := idempotency.NewHybridStore(kv, config.DefaultIdempotencyTTL)
	defer func() { _ = idStore.Close() }()
	log.Printf("idempotency: hybrid store ready (TTL=%s)", config.DefaultIdempotencyTTL)

	consumerCfg := natsx.DefaultPullConsumerConfig("HA_EVENTS", "engine_processor", "ha.events.>")
	sub, err := natsx.EnsurePullConsumer(js, consumerCfg)
	if err != nil {
		log.Fatalf("nats: ensure pull consumer: %v", err)
	}
	log.Printf("nats: pull consumer engine_processor ready (MaxAckPending=%d, AckWait=%s)",
		consumerCfg.MaxAckPending, consumerCfg.AckWait)

	consumer, err := NewConsumer(sub, idStore, processEvent, consumerCfg.WorkerCount, consumerCfg.FetchBatch, consumerCfg.BackOff)
	if err != nil {
		log.Fatalf("engine: consumer init: %v", err)
	}

	dlqFwd, err := NewDLQForwarder(nc, js, "HA_EVENTS", "engine_processor")
	if err != nil {
		log.Fatalf("engine: dlq forwarder init: %v", err)
	}

	// --- Graceful shutdown ---

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-sig
		log.Printf("shutting down")
		cancel()
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		if err := consumer.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("consumer exited with error: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := dlqFwd.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("dlq forwarder exited with error: %v", err)
		}
	}()

	log.Printf("consumer and DLQ forwarder started (workers=%d, batch=%d)",
		consumerCfg.WorkerCount, consumerCfg.FetchBatch)
	wg.Wait()
	log.Printf("engine stopped")
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
	log.Printf("event received (%d bytes) — Phase 5 TODO: implement rule engine", len(data))
	return nil
}
