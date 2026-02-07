// Package boot provides shared service bootstrap helpers for Vault and NATS.
package boot

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strconv"
	"strings"

	vault "github.com/hashicorp/vault/api"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
)

// Config holds bootstrap configuration read from environment variables.
type Config struct {
	VaultAddr     string
	VaultToken    string
	NATSUrl       string
	VaultNKEYPath string

	// TLS paths for mTLS with NATS (ADR-0018)
	NATSCaPath      string
	NATSCertPath    string
	NATSKeyPath     string
	NATSRequireMTLS bool
}

// LoadConfig reads bootstrap configuration from environment variables.
func LoadConfig(service string) Config {
	requireMTLS, _ := strconv.ParseBool(os.Getenv("NATS_REQUIRE_MTLS"))
	return Config{
		VaultAddr:       envOrDefault("VAULT_ADDR", "http://127.0.0.1:8201"),
		VaultToken:      os.Getenv("VAULT_TOKEN"),
		NATSUrl:         envOrDefault("NATS_URL", "tls://localhost:4222"),
		VaultNKEYPath:   envOrDefault("VAULT_NKEY_PATH", "secret/data/ruby-core/nats/"+service),
		NATSCaPath:      os.Getenv("NATS_CA_PATH"),
		NATSCertPath:    os.Getenv("NATS_CLIENT_CERT_PATH"),
		NATSKeyPath:     os.Getenv("NATS_CLIENT_KEY_PATH"),
		NATSRequireMTLS: requireMTLS,
	}
}

// FetchNATSSeed retrieves the NATS NKEY seed from Vault KV v2.
func FetchNATSSeed(addr, token, path string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("VAULT_TOKEN is not set")
	}

	cfg := vault.DefaultConfig()
	cfg.Address = addr

	client, err := vault.NewClient(cfg)
	if err != nil {
		return "", fmt.Errorf("create client: %w", err)
	}
	client.SetToken(token)

	secret, err := client.Logical().Read(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if secret == nil || secret.Data == nil {
		return "", fmt.Errorf("no data at %s", path)
	}

	data, ok := secret.Data["data"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("unexpected data format at %s", path)
	}

	seed, ok := data["seed"].(string)
	if !ok || seed == "" {
		return "", fmt.Errorf("missing seed in %s", path)
	}
	return seed, nil
}

// ConnectNATS establishes a NATS connection using NKEY auth and mTLS.
func ConnectNATS(cfg Config, name, seed string) (*nats.Conn, error) {
	kp, err := nkeys.FromSeed([]byte(seed))
	if err != nil {
		return nil, fmt.Errorf("parse seed: %w", err)
	}

	pub, err := kp.PublicKey()
	if err != nil {
		return nil, fmt.Errorf("public key: %w", err)
	}

	opts := []nats.Option{
		nats.Nkey(pub, func(nonce []byte) ([]byte, error) {
			return kp.Sign(nonce)
		}),
		nats.Name(name),
	}

	useTLS := strings.HasPrefix(cfg.NATSUrl, "tls://") || cfg.NATSRequireMTLS
	if useTLS {
		if cfg.NATSCaPath == "" {
			return nil, fmt.Errorf("NATS_CA_PATH is required for TLS connection")
		}
		if cfg.NATSCertPath == "" {
			return nil, fmt.Errorf("NATS_CLIENT_CERT_PATH is required for mTLS connection")
		}
		if cfg.NATSKeyPath == "" {
			return nil, fmt.Errorf("NATS_CLIENT_KEY_PATH is required for mTLS connection")
		}

		caCert, err := os.ReadFile(cfg.NATSCaPath)
		if err != nil {
			return nil, fmt.Errorf("read CA cert %s: %w", cfg.NATSCaPath, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA cert %s", cfg.NATSCaPath)
		}

		clientCert, err := tls.LoadX509KeyPair(cfg.NATSCertPath, cfg.NATSKeyPath)
		if err != nil {
			return nil, fmt.Errorf("load client cert: %w", err)
		}

		tlsCfg := &tls.Config{
			RootCAs:      pool,
			Certificates: []tls.Certificate{clientCert},
			MinVersion:   tls.VersionTLS12,
		}
		opts = append(opts, nats.Secure(tlsCfg))
	}

	nc, err := nats.Connect(cfg.NATSUrl, opts...)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return nc, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
