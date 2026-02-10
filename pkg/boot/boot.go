// Package boot provides shared service bootstrap helpers for Vault and NATS.
package boot

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	vault "github.com/hashicorp/vault/api"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
)

// Config holds bootstrap configuration read from environment variables.
type Config struct {
	VaultAddr       string
	VaultToken      string
	NATSUrl         string
	VaultNKEYPath   string
	VaultTLSPath    string
	NATSRequireMTLS bool
}

// TLSMaterial holds PEM-encoded TLS certificate material fetched from Vault.
type TLSMaterial struct {
	Cert []byte
	Key  []byte
	CA   []byte
}

// vaultReader abstracts Vault read operations for testing.
type vaultReader interface {
	Read(path string) (*vault.Secret, error)
}

// LoadConfig reads bootstrap configuration from environment variables.
func LoadConfig(service string) Config {
	requireMTLS, _ := strconv.ParseBool(os.Getenv("NATS_REQUIRE_MTLS"))
	cfg := Config{
		VaultAddr:       envOrDefault("VAULT_ADDR", "http://127.0.0.1:8201"),
		VaultToken:      os.Getenv("VAULT_TOKEN"),
		NATSUrl:         envOrDefault("NATS_URL", "tls://localhost:4222"),
		VaultNKEYPath:   envOrDefault("VAULT_NKEY_PATH", "secret/data/ruby-core/nats/"+service),
		VaultTLSPath:    envOrDefault("VAULT_TLS_PATH", "secret/data/ruby-core/tls/"+service),
		NATSRequireMTLS: requireMTLS,
	}

	// Reject plaintext Vault in production (ADR-0015)
	if os.Getenv("ENVIRONMENT") == "production" && strings.HasPrefix(cfg.VaultAddr, "http://") {
		log.Fatalf("vault: VAULT_ADDR uses plaintext HTTP (%s); HTTPS required in production", cfg.VaultAddr)
	}

	return cfg
}

// newVaultClient creates a configured Vault client.
func newVaultClient(addr, token string) (*vault.Client, error) {
	if token == "" {
		return nil, fmt.Errorf("VAULT_TOKEN is not set")
	}

	cfg := vault.DefaultConfig()
	cfg.Address = addr

	client, err := vault.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}
	client.SetToken(token)
	return client, nil
}

// FetchNATSSeed retrieves the NATS NKEY seed from Vault KV v2.
func FetchNATSSeed(addr, token, path string) (string, error) {
	client, err := newVaultClient(addr, token)
	if err != nil {
		return "", err
	}

	var seed string
	err = withRetry(func() error {
		var fetchErr error
		seed, fetchErr = fetchSeed(client.Logical(), path)
		return fetchErr
	})
	return seed, err
}

// FetchNATSTLS retrieves TLS client certificate material from Vault KV v2.
func FetchNATSTLS(addr, token, path string) (*TLSMaterial, error) {
	client, err := newVaultClient(addr, token)
	if err != nil {
		return nil, err
	}

	var mat *TLSMaterial
	err = withRetry(func() error {
		var fetchErr error
		mat, fetchErr = fetchTLS(client.Logical(), path)
		return fetchErr
	})
	return mat, err
}

// fetchSeed reads and parses the NKEY seed from a Vault KV v2 path.
func fetchSeed(r vaultReader, path string) (string, error) {
	secret, err := r.Read(path)
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

// fetchTLS reads and parses TLS certificate material from a Vault KV v2 path.
func fetchTLS(r vaultReader, path string) (*TLSMaterial, error) {
	secret, err := r.Read(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if secret == nil || secret.Data == nil {
		return nil, fmt.Errorf("no data at %s", path)
	}

	data, ok := secret.Data["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected data format at %s", path)
	}

	cert, ok := data["cert"].(string)
	if !ok || cert == "" {
		return nil, fmt.Errorf("missing cert in %s", path)
	}

	key, ok := data["key"].(string)
	if !ok || key == "" {
		return nil, fmt.Errorf("missing key in %s", path)
	}

	ca, ok := data["ca"].(string)
	if !ok || ca == "" {
		return nil, fmt.Errorf("missing ca in %s", path)
	}

	return &TLSMaterial{
		Cert: []byte(cert),
		Key:  []byte(key),
		CA:   []byte(ca),
	}, nil
}

// ConnectNATS establishes a NATS connection using NKEY auth and mTLS.
func ConnectNATS(cfg Config, name, seed string, tlsMat *TLSMaterial) (*nats.Conn, error) {
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
		if tlsMat == nil {
			return nil, fmt.Errorf("TLS material is required for mTLS connection")
		}

		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(tlsMat.CA) {
			return nil, fmt.Errorf("failed to parse CA certificate from Vault")
		}

		clientCert, err := tls.X509KeyPair(tlsMat.Cert, tlsMat.Key)
		if err != nil {
			return nil, fmt.Errorf("parse client certificate from Vault: %w", err)
		}

		tlsCfg := &tls.Config{
			RootCAs:      pool,
			Certificates: []tls.Certificate{clientCert},
			MinVersion:   tls.VersionTLS13,
		}
		opts = append(opts, nats.Secure(tlsCfg))
	}

	nc, err := nats.Connect(cfg.NATSUrl, opts...)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return nc, nil
}

// withRetry retries fn up to 3 times with exponential backoff (1s, 2s, 4s).
func withRetry(fn func() error) error {
	delays := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}
	var err error
	for i := 0; i <= len(delays); i++ {
		err = fn()
		if err == nil {
			return nil
		}
		if i < len(delays) {
			log.Printf("vault: retry %d/%d after error: %v", i+1, len(delays), err)
			time.Sleep(delays[i])
		}
	}
	return err
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
