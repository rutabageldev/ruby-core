// PKI-direct issuance path for service mTLS material (PLAN-0008, Phase 17.6).
//
// Services authenticate via AppRole and issue cert+key directly from
// pki_int/issue/<role>; a renewal goroutine re-issues at TTL/2 and hot-swaps
// the cert into a mutex-guarded holder. NATS reconnect handshakes call the
// holder via tls.Config.GetClientCertificate and pick up rotated material
// without restart.
//
// This path supersedes FetchNATSTLS for services that set VAULT_PKI_ROLE.
// The legacy KV path remains callable as the rollback target until
// Phase 17.7 retires it.

package boot

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	vault "github.com/hashicorp/vault/api"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
)

// IssueConfig captures the inputs needed to issue a TLS cert from Vault PKI.
type IssueConfig struct {
	VaultAddr    string // e.g. https://vault:8200
	RoleIDPath   string // path to role-id file on disk
	SecretIDPath string // path to secret-id file on disk
	PKIRole      string // e.g. "ruby-core-gateway"
	CommonName   string // e.g. "gateway"
	IPSANs       string // comma-separated; empty for client-only roles
	TTL          string // e.g. "720h"
}

// MaterialHolder wraps TLS material with a mutex so a renewal goroutine can
// hot-swap fresh material while consumers (the NATS TLS handshake) read it
// concurrently. The parsed forms of the cert and CA pool are cached to avoid
// re-parsing PEM on every handshake.
type MaterialHolder struct {
	mu     sync.RWMutex
	raw    *TLSMaterial
	cert   *tls.Certificate
	pool   *x509.CertPool
	expiry time.Time // notAfter of the current cert
}

// NewMaterialHolder builds a holder from initial cert material. Returns an
// error if the material cannot be parsed.
func NewMaterialHolder(mat *TLSMaterial, expiry time.Time) (*MaterialHolder, error) {
	h := &MaterialHolder{}
	if err := h.Replace(mat, expiry); err != nil {
		return nil, err
	}
	return h, nil
}

// Replace atomically swaps in fresh material. The cert and CA pool are
// re-parsed; callers must hold the holder pointer (which is stable) — the
// NATS handshake closure that reads via Get sees the new material on the
// next handshake.
func (h *MaterialHolder) Replace(mat *TLSMaterial, expiry time.Time) error {
	cert, err := tls.X509KeyPair(mat.Cert, mat.Key)
	if err != nil {
		return fmt.Errorf("parse cert+key: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(mat.CA) {
		return fmt.Errorf("parse CA bundle")
	}
	h.mu.Lock()
	h.raw = mat
	h.cert = &cert
	h.pool = pool
	h.expiry = expiry
	h.mu.Unlock()
	return nil
}

// Cert returns the current parsed certificate. Safe for concurrent use.
func (h *MaterialHolder) Cert() *tls.Certificate {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.cert
}

// CAPool returns the current CA pool. Safe for concurrent use.
func (h *MaterialHolder) CAPool() *x509.CertPool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.pool
}

// Expiry returns the notAfter of the current cert.
func (h *MaterialHolder) Expiry() time.Time {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.expiry
}

// appRoleLogin reads role-id + secret-id from disk, performs AppRole login
// against Vault, and returns an authenticated client. The returned client's
// token has the policy bound to the AppRole (foundation-agent-ruby-core-<svc>).
func appRoleLogin(addr, roleIDPath, secretIDPath string) (*vault.Client, error) {
	// Paths come from VAULT_ROLE_ID_PATH / VAULT_SECRET_ID_PATH env vars set
	// by compose; they point at specific bind-mounted AppRole material at
	// well-known container paths (default /vault/role-id, /vault/secret-id).
	// gosec G304's path-traversal concern doesn't apply: we WANT the operator-
	// configured path. Wrong path → AppRole login fails → service fails to
	// start, which is the correct failure mode.
	roleID, err := os.ReadFile(roleIDPath) // #nosec G304 -- operator-configured AppRole material path
	if err != nil {
		return nil, fmt.Errorf("read role-id %s: %w", roleIDPath, err)
	}
	secretID, err := os.ReadFile(secretIDPath) // #nosec G304 -- operator-configured AppRole material path
	if err != nil {
		return nil, fmt.Errorf("read secret-id %s: %w", secretIDPath, err)
	}

	cfg := vault.DefaultConfig()
	cfg.Address = addr
	client, err := vault.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("create vault client: %w", err)
	}

	resp, err := client.Logical().Write("auth/approle/login", map[string]interface{}{
		"role_id":   strings.TrimSpace(string(roleID)),
		"secret_id": strings.TrimSpace(string(secretID)),
	})
	if err != nil {
		return nil, fmt.Errorf("approle login: %w", err)
	}
	if resp == nil || resp.Auth == nil || resp.Auth.ClientToken == "" {
		return nil, fmt.Errorf("approle login: empty response")
	}
	client.SetToken(resp.Auth.ClientToken)
	return client, nil
}

// IssueNATSCert authenticates via AppRole, issues a fresh cert from the
// configured PKI role, and returns the material plus its notAfter time.
// Wrapped in withRetry — transient Vault failures retry up to 3x.
func IssueNATSCert(cfg IssueConfig) (*TLSMaterial, time.Time, error) {
	var (
		mat    *TLSMaterial
		expiry time.Time
	)
	err := withRetry(func() error {
		client, err := appRoleLogin(cfg.VaultAddr, cfg.RoleIDPath, cfg.SecretIDPath)
		if err != nil {
			return err
		}
		payload := map[string]interface{}{
			"common_name": cfg.CommonName,
			"ttl":         cfg.TTL,
		}
		if cfg.IPSANs != "" {
			payload["ip_sans"] = cfg.IPSANs
		}
		resp, err := client.Logical().Write("pki_int/issue/"+cfg.PKIRole, payload)
		if err != nil {
			return fmt.Errorf("issue %s: %w", cfg.PKIRole, err)
		}
		if resp == nil || resp.Data == nil {
			return fmt.Errorf("issue %s: empty response", cfg.PKIRole)
		}
		certPEM, ok := resp.Data["certificate"].(string)
		if !ok || certPEM == "" {
			return fmt.Errorf("issue %s: missing certificate", cfg.PKIRole)
		}
		keyPEM, ok := resp.Data["private_key"].(string)
		if !ok || keyPEM == "" {
			return fmt.Errorf("issue %s: missing private_key", cfg.PKIRole)
		}
		issuingCA, ok := resp.Data["issuing_ca"].(string)
		if !ok || issuingCA == "" {
			return fmt.Errorf("issue %s: missing issuing_ca", cfg.PKIRole)
		}
		mat = &TLSMaterial{
			Cert: []byte(certPEM),
			Key:  []byte(keyPEM),
			CA:   []byte(issuingCA),
		}
		// expiration field is a json.Number (int seconds since epoch).
		if expNum, ok := resp.Data["expiration"]; ok {
			switch v := expNum.(type) {
			case float64:
				expiry = time.Unix(int64(v), 0)
			default:
				// Some Vault clients return json.Number; fall back to parsing.
				if s := fmt.Sprintf("%v", v); s != "" {
					if t, err := time.Parse(time.RFC3339, s); err == nil {
						expiry = t
					}
				}
			}
		}
		if expiry.IsZero() {
			// Vault didn't return an expiration field; parse it from the cert PEM.
			if t, perr := parseCertNotAfter([]byte(certPEM)); perr == nil {
				expiry = t
			}
		}
		return nil
	})
	if err != nil {
		return nil, time.Time{}, err
	}
	return mat, expiry, nil
}

// parseCertNotAfter extracts the NotAfter time from the first cert in a PEM bundle.
func parseCertNotAfter(pemData []byte) (time.Time, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return time.Time{}, fmt.Errorf("no PEM block")
	}
	x, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse cert: %w", err)
	}
	return x.NotAfter, nil
}

// RenewLoop ticks at half the current cert's remaining lifetime (or 1 hour,
// whichever is smaller for early-cycle safety) and re-issues fresh material
// via IssueNATSCert. On each tick:
//   - Computes the next tick from the new expiry.
//   - On success: swaps the holder; logs at INFO with new expiry.
//   - On failure: logs at WARN with the error and retries on the next tick
//     using the existing cert (no panic, no exit). The next tick is computed
//     from the previous expiry so we keep trying before the cert dies.
//
// The loop exits when ctx is canceled. RenewLoop is intended to run in a
// goroutine for the lifetime of the service process.
func RenewLoop(ctx context.Context, cfg IssueConfig, holder *MaterialHolder) {
	for {
		next := renewIn(holder.Expiry(), time.Now())
		slog.Info("pki: renewal scheduled",
			slog.String("role", cfg.PKIRole),
			slog.Duration("in", next),
			slog.Time("current_expiry", holder.Expiry()),
		)
		timer := time.NewTimer(next)
		select {
		case <-ctx.Done():
			timer.Stop()
			slog.Info("pki: renew loop exiting", slog.String("role", cfg.PKIRole))
			return
		case <-timer.C:
		}

		mat, expiry, err := IssueNATSCert(cfg)
		if err != nil {
			slog.Warn("pki: renewal failed; will retry on next tick",
				slog.String("role", cfg.PKIRole),
				slog.String("error", err.Error()),
			)
			continue
		}
		if err := holder.Replace(mat, expiry); err != nil {
			slog.Warn("pki: renewal parse failed; will retry on next tick",
				slog.String("role", cfg.PKIRole),
				slog.String("error", err.Error()),
			)
			continue
		}
		slog.Info("pki: renewed",
			slog.String("role", cfg.PKIRole),
			slog.Time("new_expiry", expiry),
		)
	}
}

// renewIn returns the duration until the next renewal attempt, computed as
// half the remaining cert lifetime, clamped to [1m, 24h] to avoid degenerate
// tight loops or overly-long sleeps.
func renewIn(expiry, now time.Time) time.Duration {
	remaining := expiry.Sub(now)
	if remaining <= 0 {
		return 1 * time.Minute // cert already expired; retry soon
	}
	d := remaining / 2
	if d < 1*time.Minute {
		return 1 * time.Minute
	}
	if d > 24*time.Hour {
		return 24 * time.Hour
	}
	return d
}

// ConnectNATSDynamicTLS connects to NATS using NKEY auth and mTLS, where the
// client cert is read through a MaterialHolder on every handshake. Reconnect
// handshakes pick up rotated material without service restart.
//
// Mirrors ConnectNATS's NKEY + name + tls plumbing but replaces the static
// tls.Config.Certificates with a per-handshake GetClientCertificate callback.
// Go's tls.Config.Clone (called by nats.go before each handshake) preserves
// function pointers, so the closure runs against the holder's current cert
// on every handshake — verified live during the Stage 2 spike.
func ConnectNATSDynamicTLS(cfg Config, name, seed string, holder *MaterialHolder) (*nats.Conn, error) {
	kp, err := nkeys.FromSeed([]byte(seed))
	if err != nil {
		return nil, fmt.Errorf("parse seed: %w", err)
	}
	pub, err := kp.PublicKey()
	if err != nil {
		return nil, fmt.Errorf("public key: %w", err)
	}

	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    holder.CAPool(),
		GetClientCertificate: func(_ *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			c := holder.Cert()
			if c == nil {
				return nil, fmt.Errorf("no client cert available")
			}
			return c, nil
		},
	}

	opts := []nats.Option{
		nats.Nkey(pub, func(nonce []byte) ([]byte, error) {
			return kp.Sign(nonce)
		}),
		nats.Name(name),
		nats.Secure(tlsCfg),
	}

	nc, err := nats.Connect(cfg.NATSUrl, opts...)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return nc, nil
}

// PKIDirectEnabled returns true when the service is configured to use the
// direct-PKI path (Phase 17.6) rather than the legacy KV bundle (FetchNATSTLS).
func (c Config) PKIDirectEnabled() bool {
	return c.VaultPKIRole != ""
}

// BootstrapNATSTLS wires up the mTLS NATS connection using whichever path is
// configured: direct-PKI when VAULT_PKI_ROLE is set, or the legacy KV bundle
// otherwise. When direct-PKI is used, a renewal goroutine is started against
// ctx — caller cancels ctx at shutdown.
//
// The returned conn handles its own reconnection; rotated certs from the
// renewal loop are picked up automatically on the next handshake.
func BootstrapNATSTLS(ctx context.Context, cfg Config, name, seed string) (*nats.Conn, error) {
	if cfg.PKIDirectEnabled() {
		issueCfg := IssueConfig{
			VaultAddr:    cfg.VaultAddr,
			RoleIDPath:   cfg.VaultRoleIDPath,
			SecretIDPath: cfg.VaultSecretIDPath,
			PKIRole:      cfg.VaultPKIRole,
			CommonName:   cfg.Service,
			TTL:          cfg.VaultPKITTL,
		}
		mat, expiry, err := IssueNATSCert(issueCfg)
		if err != nil {
			return nil, fmt.Errorf("pki: initial issue: %w", err)
		}
		holder, err := NewMaterialHolder(mat, expiry)
		if err != nil {
			return nil, fmt.Errorf("pki: parse material: %w", err)
		}
		slog.Info("pki: cert issued",
			slog.String("role", issueCfg.PKIRole),
			slog.String("cn", issueCfg.CommonName),
			slog.Time("expiry", expiry),
		)
		nc, err := ConnectNATSDynamicTLS(cfg, name, seed, holder)
		if err != nil {
			return nil, err
		}
		go RenewLoop(ctx, issueCfg, holder)
		return nc, nil
	}

	// Legacy KV path (rollback target until Phase 17.7).
	tlsMat, err := FetchNATSTLS(cfg.VaultAddr, cfg.VaultToken, cfg.VaultTLSPath)
	if err != nil {
		return nil, fmt.Errorf("vault: fetch TLS: %w", err)
	}
	slog.Info("vault: fetched TLS material (legacy KV path)", slog.String("path", cfg.VaultTLSPath))
	return ConnectNATS(cfg, name, seed, tlsMat)
}
