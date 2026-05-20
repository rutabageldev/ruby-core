//go:build fast

package boot

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// parseCertNotAfter
// ---------------------------------------------------------------------------

// generateTestCertPEM produces a self-signed cert valid for `validFor` and
// returns its PEM-encoded bytes. Used to test parseCertNotAfter without
// touching Vault.
func generateTestCertPEM(t *testing.T, validFor time.Duration) ([]byte, time.Time) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	notBefore := time.Now().Truncate(time.Second)
	notAfter := notBefore.Add(validFor)
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), notAfter
}

func TestParseCertNotAfter(t *testing.T) {
	pemBytes, expected := generateTestCertPEM(t, 24*time.Hour)
	got, err := parseCertNotAfter(pemBytes)
	if err != nil {
		t.Fatalf("parseCertNotAfter: %v", err)
	}
	if !got.Equal(expected) {
		t.Errorf("notAfter mismatch: got %v, want %v", got, expected)
	}
}

func TestParseCertNotAfter_NoPEMBlock(t *testing.T) {
	_, err := parseCertNotAfter([]byte("not a PEM"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestParseCertNotAfter_BadDER(t *testing.T) {
	bad := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("garbage")})
	_, err := parseCertNotAfter(bad)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// renewIn — TTL/2 scheduling with clamps
// ---------------------------------------------------------------------------

func TestRenewIn(t *testing.T) {
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		expiry  time.Time
		want    time.Duration
		comment string
	}{
		{"already expired", now.Add(-1 * time.Hour), 1 * time.Minute, "should retry soon, not negative"},
		{"expiring now", now, 1 * time.Minute, "edge case: remaining=0 falls into retry-soon"},
		{"30s out", now.Add(30 * time.Second), 1 * time.Minute, "very small TTL → minimum clamp 1m"},
		{"4 minutes out", now.Add(4 * time.Minute), 2 * time.Minute, "TTL/2 = 2m, above 1m floor"},
		{"1 hour out", now.Add(1 * time.Hour), 30 * time.Minute, "TTL/2 = 30m"},
		{"30 days out (typical)", now.Add(720 * time.Hour), 24 * time.Hour, "TTL/2 = 15d, clamped to 24h ceiling"},
		{"1 year out (max)", now.Add(8760 * time.Hour), 24 * time.Hour, "very long TTL, ceiling clamp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renewIn(tc.expiry, now)
			if got != tc.want {
				t.Errorf("renewIn = %v, want %v (%s)", got, tc.want, tc.comment)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// MaterialHolder — atomic replace + concurrent read
// ---------------------------------------------------------------------------

// buildMaterial builds a self-consistent TLSMaterial for tests by generating
// a self-signed cert + key + CA (same cert serves as its own CA for parser).
func buildMaterial(t *testing.T) *TLSMaterial {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "holder-test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(1 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return &TLSMaterial{Cert: certPEM, Key: keyPEM, CA: certPEM}
}

func TestMaterialHolder_ReplaceRoundTrip(t *testing.T) {
	mat := buildMaterial(t)
	expiry := time.Now().Add(1 * time.Hour)
	h, err := NewMaterialHolder(mat, expiry)
	if err != nil {
		t.Fatalf("NewMaterialHolder: %v", err)
	}
	if h.Cert() == nil {
		t.Fatal("Cert() returned nil after construction")
	}
	if h.CAPool() == nil {
		t.Fatal("CAPool() returned nil after construction")
	}
	if !h.Expiry().Equal(expiry) {
		t.Errorf("Expiry mismatch: got %v, want %v", h.Expiry(), expiry)
	}

	// Replace with new material
	mat2 := buildMaterial(t)
	expiry2 := time.Now().Add(2 * time.Hour)
	if err := h.Replace(mat2, expiry2); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if !h.Expiry().Equal(expiry2) {
		t.Errorf("Expiry after replace: got %v, want %v", h.Expiry(), expiry2)
	}
}

func TestMaterialHolder_ReplaceBadCert(t *testing.T) {
	mat := buildMaterial(t)
	h, err := NewMaterialHolder(mat, time.Now().Add(1*time.Hour))
	if err != nil {
		t.Fatalf("NewMaterialHolder: %v", err)
	}
	bad := &TLSMaterial{Cert: []byte("garbage"), Key: mat.Key, CA: mat.CA}
	if err := h.Replace(bad, time.Now()); err == nil {
		t.Fatal("expected error replacing with invalid cert PEM")
	}
	// Original cert remains accessible after failed Replace.
	if h.Cert() == nil {
		t.Fatal("Cert() returned nil after failed Replace")
	}
}

func TestMaterialHolder_ConcurrentReadWrite(t *testing.T) {
	mat := buildMaterial(t)
	h, err := NewMaterialHolder(mat, time.Now().Add(1*time.Hour))
	if err != nil {
		t.Fatalf("NewMaterialHolder: %v", err)
	}

	// 50 readers hammering h.Cert() / h.CAPool() while a writer Replaces.
	// Pure race detection — no assertions on values beyond "not nil."
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					if c := h.Cert(); c == nil {
						t.Error("Cert() returned nil during concurrent read")
						return
					}
					if p := h.CAPool(); p == nil {
						t.Error("CAPool() returned nil during concurrent read")
						return
					}
				}
			}
		}()
	}

	// 10 sequential replaces.
	for i := 0; i < 10; i++ {
		mat := buildMaterial(t)
		if err := h.Replace(mat, time.Now().Add(time.Duration(i+1)*time.Hour)); err != nil {
			t.Fatalf("Replace #%d: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Config.PKIDirectEnabled
// ---------------------------------------------------------------------------

func TestConfig_PKIDirectEnabled(t *testing.T) {
	if (Config{}).PKIDirectEnabled() {
		t.Error("empty Config should not be PKI-direct")
	}
	if !(Config{VaultPKIRole: "ruby-core-gateway"}).PKIDirectEnabled() {
		t.Error("Config with VaultPKIRole should be PKI-direct")
	}
}
