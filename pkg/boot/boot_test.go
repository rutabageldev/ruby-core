package boot

import (
	"fmt"
	"testing"

	vault "github.com/hashicorp/vault/api"
)

// mockVaultReader implements vaultReader for testing.
type mockVaultReader struct {
	secret *vault.Secret
	err    error
}

func (m *mockVaultReader) Read(path string) (*vault.Secret, error) {
	return m.secret, m.err
}

// ---------------------------------------------------------------------------
// fetchSeed tests
// ---------------------------------------------------------------------------

func TestFetchSeed(t *testing.T) {
	testCases := []struct {
		name    string
		reader  vaultReader
		wantErr string
		want    string
	}{
		{
			name: "valid KV v2 response",
			reader: &mockVaultReader{
				secret: &vault.Secret{
					Data: map[string]interface{}{
						"data": map[string]interface{}{
							"seed":       "SUAEXAMPLE",
							"public_key": "UEXAMPLE",
						},
					},
				},
			},
			want: "SUAEXAMPLE",
		},
		{
			name:    "vault read error",
			reader:  &mockVaultReader{err: fmt.Errorf("connection refused")},
			wantErr: "read secret/test: connection refused",
		},
		{
			name:    "nil secret",
			reader:  &mockVaultReader{secret: nil},
			wantErr: "no data at secret/test",
		},
		{
			name:    "nil data in secret",
			reader:  &mockVaultReader{secret: &vault.Secret{Data: nil}},
			wantErr: "no data at secret/test",
		},
		{
			name: "missing data wrapper",
			reader: &mockVaultReader{
				secret: &vault.Secret{
					Data: map[string]interface{}{
						"not_data": "something",
					},
				},
			},
			wantErr: "unexpected data format at secret/test",
		},
		{
			name: "wrong type for data wrapper",
			reader: &mockVaultReader{
				secret: &vault.Secret{
					Data: map[string]interface{}{
						"data": "not-a-map",
					},
				},
			},
			wantErr: "unexpected data format at secret/test",
		},
		{
			name: "missing seed field",
			reader: &mockVaultReader{
				secret: &vault.Secret{
					Data: map[string]interface{}{
						"data": map[string]interface{}{
							"public_key": "UEXAMPLE",
						},
					},
				},
			},
			wantErr: "missing seed in secret/test",
		},
		{
			name: "empty seed value",
			reader: &mockVaultReader{
				secret: &vault.Secret{
					Data: map[string]interface{}{
						"data": map[string]interface{}{
							"seed": "",
						},
					},
				},
			},
			wantErr: "missing seed in secret/test",
		},
		{
			name: "seed is wrong type",
			reader: &mockVaultReader{
				secret: &vault.Secret{
					Data: map[string]interface{}{
						"data": map[string]interface{}{
							"seed": 12345,
						},
					},
				},
			},
			wantErr: "missing seed in secret/test",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := fetchSeed(tc.reader, "secret/test")
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if err.Error() != tc.wantErr {
					t.Errorf("error = %q, want %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("seed = %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// fetchTLS tests
// ---------------------------------------------------------------------------

func TestFetchTLS(t *testing.T) {
	validData := map[string]interface{}{
		"data": map[string]interface{}{
			"cert": "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----",
			"key":  "-----BEGIN PRIVATE KEY-----\ntest\n-----END PRIVATE KEY-----",
			"ca":   "-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----",
		},
	}

	testCases := []struct {
		name    string
		reader  vaultReader
		wantErr string
	}{
		{
			name: "valid response",
			reader: &mockVaultReader{
				secret: &vault.Secret{Data: validData},
			},
		},
		{
			name:    "vault read error",
			reader:  &mockVaultReader{err: fmt.Errorf("connection refused")},
			wantErr: "read secret/test: connection refused",
		},
		{
			name:    "nil secret",
			reader:  &mockVaultReader{secret: nil},
			wantErr: "no data at secret/test",
		},
		{
			name: "missing data wrapper",
			reader: &mockVaultReader{
				secret: &vault.Secret{
					Data: map[string]interface{}{"not_data": "x"},
				},
			},
			wantErr: "unexpected data format at secret/test",
		},
		{
			name: "missing cert",
			reader: &mockVaultReader{
				secret: &vault.Secret{
					Data: map[string]interface{}{
						"data": map[string]interface{}{
							"key": "k",
							"ca":  "c",
						},
					},
				},
			},
			wantErr: "missing cert in secret/test",
		},
		{
			name: "missing key",
			reader: &mockVaultReader{
				secret: &vault.Secret{
					Data: map[string]interface{}{
						"data": map[string]interface{}{
							"cert": "c",
							"ca":   "c",
						},
					},
				},
			},
			wantErr: "missing key in secret/test",
		},
		{
			name: "missing ca",
			reader: &mockVaultReader{
				secret: &vault.Secret{
					Data: map[string]interface{}{
						"data": map[string]interface{}{
							"cert": "c",
							"key":  "k",
						},
					},
				},
			},
			wantErr: "missing ca in secret/test",
		},
		{
			name: "empty cert value",
			reader: &mockVaultReader{
				secret: &vault.Secret{
					Data: map[string]interface{}{
						"data": map[string]interface{}{
							"cert": "",
							"key":  "k",
							"ca":   "c",
						},
					},
				},
			},
			wantErr: "missing cert in secret/test",
		},
		{
			name: "cert is wrong type",
			reader: &mockVaultReader{
				secret: &vault.Secret{
					Data: map[string]interface{}{
						"data": map[string]interface{}{
							"cert": 123,
							"key":  "k",
							"ca":   "c",
						},
					},
				},
			},
			wantErr: "missing cert in secret/test",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mat, err := fetchTLS(tc.reader, "secret/test")
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if err.Error() != tc.wantErr {
					t.Errorf("error = %q, want %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if mat == nil {
				t.Fatal("expected non-nil TLSMaterial")
			}
			if len(mat.Cert) == 0 {
				t.Error("expected non-empty Cert")
			}
			if len(mat.Key) == 0 {
				t.Error("expected non-empty Key")
			}
			if len(mat.CA) == 0 {
				t.Error("expected non-empty CA")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ConnectNATS pre-connection validation tests
// ---------------------------------------------------------------------------

func TestConnectNATS_TLSValidation(t *testing.T) {
	// Generate a real NKEY seed for testing
	validSeed := "SUAIBDPBAUTWCWBKIO6XHQNINK5FWJW4OHLXC3HQ2KFE4PEJUA44CNHTC4"

	testCases := []struct {
		name    string
		cfg     Config
		seed    string
		tlsMat  *TLSMaterial
		wantErr string
	}{
		{
			name:    "nil TLS material with tls:// URL",
			cfg:     Config{NATSUrl: "tls://nats:4222"},
			seed:    validSeed,
			tlsMat:  nil,
			wantErr: "TLS material is required for mTLS connection",
		},
		{
			name:    "nil TLS material with NATS_REQUIRE_MTLS",
			cfg:     Config{NATSUrl: "nats://nats:4222", NATSRequireMTLS: true},
			seed:    validSeed,
			tlsMat:  nil,
			wantErr: "TLS material is required for mTLS connection",
		},
		{
			name: "invalid CA PEM",
			cfg:  Config{NATSUrl: "tls://nats:4222"},
			seed: validSeed,
			tlsMat: &TLSMaterial{
				Cert: []byte("cert"),
				Key:  []byte("key"),
				CA:   []byte("not-valid-pem"),
			},
			wantErr: "failed to parse CA certificate from Vault",
		},
		{
			name:    "invalid seed",
			cfg:     Config{NATSUrl: "nats://nats:4222"},
			seed:    "not-a-valid-seed",
			tlsMat:  nil,
			wantErr: "parse seed: illegal base32 data at input byte 0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ConnectNATS(tc.cfg, "test-svc", tc.seed, tc.tlsMat)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if err.Error() != tc.wantErr {
				t.Errorf("error = %q, want %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// LoadConfig tests
// ---------------------------------------------------------------------------

func TestLoadConfig(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		// Clear relevant env vars
		for _, key := range []string{"VAULT_ADDR", "VAULT_TOKEN", "NATS_URL", "VAULT_NKEY_PATH", "VAULT_TLS_PATH", "NATS_REQUIRE_MTLS", "ENVIRONMENT"} {
			t.Setenv(key, "")
		}

		cfg := LoadConfig("gateway")

		if cfg.VaultAddr != "http://127.0.0.1:8201" {
			t.Errorf("VaultAddr = %q, want default", cfg.VaultAddr)
		}
		if cfg.NATSUrl != "tls://localhost:4222" {
			t.Errorf("NATSUrl = %q, want default", cfg.NATSUrl)
		}
		if cfg.VaultNKEYPath != "secret/data/ruby-core/nats/gateway" {
			t.Errorf("VaultNKEYPath = %q, want default", cfg.VaultNKEYPath)
		}
		if cfg.VaultTLSPath != "secret/data/ruby-core/tls/gateway" {
			t.Errorf("VaultTLSPath = %q, want default", cfg.VaultTLSPath)
		}
		if cfg.NATSRequireMTLS {
			t.Error("NATSRequireMTLS should default to false")
		}
	})

	t.Run("env overrides", func(t *testing.T) {
		t.Setenv("VAULT_ADDR", "https://vault.prod:8200")
		t.Setenv("VAULT_TOKEN", "s.mytoken")
		t.Setenv("NATS_URL", "tls://nats.prod:4222")
		t.Setenv("VAULT_NKEY_PATH", "secret/data/custom/nkey")
		t.Setenv("VAULT_TLS_PATH", "secret/data/custom/tls")
		t.Setenv("NATS_REQUIRE_MTLS", "true")
		t.Setenv("ENVIRONMENT", "")

		cfg := LoadConfig("engine")

		if cfg.VaultAddr != "https://vault.prod:8200" {
			t.Errorf("VaultAddr = %q, want override", cfg.VaultAddr)
		}
		if cfg.VaultToken != "s.mytoken" {
			t.Errorf("VaultToken = %q, want override", cfg.VaultToken)
		}
		if cfg.NATSUrl != "tls://nats.prod:4222" {
			t.Errorf("NATSUrl = %q, want override", cfg.NATSUrl)
		}
		if cfg.VaultNKEYPath != "secret/data/custom/nkey" {
			t.Errorf("VaultNKEYPath = %q, want override", cfg.VaultNKEYPath)
		}
		if cfg.VaultTLSPath != "secret/data/custom/tls" {
			t.Errorf("VaultTLSPath = %q, want override", cfg.VaultTLSPath)
		}
		if !cfg.NATSRequireMTLS {
			t.Error("NATSRequireMTLS should be true")
		}
	})
}

// ---------------------------------------------------------------------------
// newVaultClient tests
// ---------------------------------------------------------------------------

func TestNewVaultClient(t *testing.T) {
	t.Run("empty token", func(t *testing.T) {
		_, err := newVaultClient("http://localhost:8200", "")
		if err == nil {
			t.Fatal("expected error for empty token")
		}
		if err.Error() != "VAULT_TOKEN is not set" {
			t.Errorf("error = %q, want %q", err.Error(), "VAULT_TOKEN is not set")
		}
	})

	t.Run("valid client creation", func(t *testing.T) {
		// Prevent Vault client from reading real VAULT_ADDR env
		t.Setenv("VAULT_ADDR", "")
		client, err := newVaultClient("http://localhost:8200", "test-token")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})
}

// ---------------------------------------------------------------------------
// withRetry tests
// ---------------------------------------------------------------------------

func TestWithRetry(t *testing.T) {
	t.Run("succeeds on first attempt", func(t *testing.T) {
		calls := 0
		err := withRetry(func() error {
			calls++
			return nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if calls != 1 {
			t.Errorf("calls = %d, want 1", calls)
		}
	})

	t.Run("succeeds on retry", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping retry test in short mode")
		}

		calls := 0
		err := withRetry(func() error {
			calls++
			if calls < 3 {
				return fmt.Errorf("transient error")
			}
			return nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if calls != 3 {
			t.Errorf("calls = %d, want 3", calls)
		}
	})

	t.Run("exhausts retries", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping retry test in short mode")
		}

		calls := 0
		err := withRetry(func() error {
			calls++
			return fmt.Errorf("persistent error")
		})
		if err == nil {
			t.Fatal("expected error after exhausting retries")
		}
		if calls != 4 { // 1 initial + 3 retries
			t.Errorf("calls = %d, want 4", calls)
		}
	})
}

// ---------------------------------------------------------------------------
// envOrDefault tests
// ---------------------------------------------------------------------------

func TestEnvOrDefault(t *testing.T) {
	t.Run("returns env value when set", func(t *testing.T) {
		t.Setenv("TEST_BOOT_VAR", "custom")
		if got := envOrDefault("TEST_BOOT_VAR", "fallback"); got != "custom" {
			t.Errorf("got %q, want %q", got, "custom")
		}
	})

	t.Run("returns fallback when unset", func(t *testing.T) {
		t.Setenv("TEST_BOOT_UNSET", "")
		if got := envOrDefault("TEST_BOOT_UNSET", "fallback"); got != "fallback" {
			t.Errorf("got %q, want %q", got, "fallback")
		}
	})
}
