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
	VaultTLSPath  string
	NATSRequireMTLS bool
}

// TLSMaterial holds PEM-encoded TLS certificate material fetched from Vault.
type TLSMaterial struct {
	Cert []byte
	Key  []byte
	CA   []byte
}

// LoadConfig reads bootstrap configuration from environment variables.
func LoadConfig(service string) Config {
	requireMTLS, _ := strconv.ParseBool(os.Getenv("NATS_REQUIRE_MTLS"))
	return Config{
		VaultAddr:       envOrDefault("VAULT_ADDR", "http://127.0.0.1:8201"),
		VaultToken:      os.Getenv("VAULT_TOKEN"),
		NATSUrl:         envOrDefault("NATS_URL", "tls://localhost:4222"),
		VaultNKEYPath:   envOrDefault("VAULT_NKEY_PATH", "secret/data/ruby-core/nats/"+service),
		VaultTLSPath:    envOrDefault("VAULT_TLS_PATH", "secret/data/ruby-core/tls/"+service),
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

// FetchNATSTLS retrieves TLS client certificate material from Vault KV v2.
func FetchNATSTLS(addr, token, path string) (*TLSMaterial, error) {
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

	secret, err := client.Logical().Read(path)
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
