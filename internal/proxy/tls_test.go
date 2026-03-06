package proxy

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGenerateCA_SetsHardenedPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".gauntlet")
	if _, err := GenerateCA(dir); err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got&0o022 != 0 {
		t.Fatalf("gauntlet dir permissions too broad: %#o", got)
	}

	certInfo, err := os.Stat(filepath.Join(dir, "ca.pem"))
	if err != nil {
		t.Fatalf("stat ca.pem: %v", err)
	}
	if got := certInfo.Mode().Perm(); got&0o022 != 0 {
		t.Fatalf("ca.pem permissions too broad: %#o", got)
	}

	keyInfo, err := os.Stat(filepath.Join(dir, "ca.key"))
	if err != nil {
		t.Fatalf("stat ca.key: %v", err)
	}
	if got := keyInfo.Mode().Perm(); got&0o077 != 0 {
		t.Fatalf("ca.key permissions too broad: %#o", got)
	}
}

func TestLoadCA_FailsOnInsecureKeyPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".gauntlet")
	if _, err := GenerateCA(dir); err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}

	keyPath := filepath.Join(dir, "ca.key")
	if err := os.Chmod(keyPath, 0o644); err != nil {
		t.Fatalf("chmod ca.key: %v", err)
	}

	_, err := LoadCA(dir)
	if err == nil {
		t.Fatal("expected LoadCA failure for insecure key permissions")
	}
	if !errors.Is(err, ErrCAInsecurePermissions) {
		t.Fatalf("expected ErrCAInsecurePermissions, got: %v", err)
	}
}

func TestLoadCA_FailsOnExpiredCertificate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".gauntlet")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir dir: %v", err)
	}

	if err := writeTestCAAssets(dir, time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour)); err != nil {
		t.Fatalf("write test CA: %v", err)
	}

	_, err := LoadCA(dir)
	if err == nil {
		t.Fatal("expected LoadCA failure for expired certificate")
	}
	if !errors.Is(err, ErrCAExpired) {
		t.Fatalf("expected ErrCAExpired, got: %v", err)
	}
}

func TestCARotationRecommended(t *testing.T) {
	now := time.Date(2026, time.March, 4, 12, 0, 0, 0, time.UTC)
	cert := &x509.Certificate{NotAfter: now.Add(7 * 24 * time.Hour)}

	rotate, remaining := CARotationRecommended(cert, now, 30*24*time.Hour)
	if !rotate {
		t.Fatal("expected rotation recommendation for near-expiry cert")
	}
	if remaining <= 0 {
		t.Fatalf("expected positive remaining duration, got %s", remaining)
	}

	cert.NotAfter = now.Add(180 * 24 * time.Hour)
	rotate, _ = CARotationRecommended(cert, now, 30*24*time.Hour)
	if rotate {
		t.Fatal("did not expect rotation recommendation for far-future cert")
	}
}

func writeTestCAAssets(dir string, notBefore, notAfter time.Time) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(99),
		Subject: pkix.Name{
			CommonName:   "Gauntlet Local CA",
			Organization: []string{"Gauntlet"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	if err := os.WriteFile(filepath.Join(dir, "ca.pem"), certPEM, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "ca.key"), keyPEM, 0o600); err != nil {
		return err
	}
	return nil
}
