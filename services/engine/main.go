package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/primaryrutabaga/ruby-core/pkg/boot"
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

	nc, err := boot.ConnectNATS(cfg, "ruby-core-engine", seed)
	if err != nil {
		log.Fatalf("nats: %v", err)
	}
	defer nc.Close()
	log.Printf("connected to NATS at %s", cfg.NATSUrl)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Printf("shutting down")
}
