package proxy

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	// ErrCAExpired indicates the local proxy CA certificate has expired.
	ErrCAExpired = errors.New("proxy CA certificate expired")
	// ErrCANotYetValid indicates the local proxy CA certificate is not yet valid.
	ErrCANotYetValid = errors.New("proxy CA certificate not yet valid")
	// ErrCAInsecurePermissions indicates CA asset path permissions are unsafe.
	ErrCAInsecurePermissions = errors.New("proxy CA asset permissions are insecure")
)

// DefaultCARotationWindow is the recommended proactive CA rotation threshold.
const DefaultCARotationWindow = 30 * 24 * time.Hour

// CA holds the local CA certificate and key for MITM TLS interception.
type CA struct {
	Cert    *x509.Certificate
	Key     *rsa.PrivateKey
	CertPEM []byte

	mu        sync.Mutex
	certCache map[string]*tls.Certificate
}

// GenerateCA creates a new RSA-2048 CA certificate and key.
// Saved to gauntletDir/ca.pem and gauntletDir/ca.key.
func GenerateCA(gauntletDir string) (*CA, error) {
	if err := os.MkdirAll(gauntletDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create gauntlet directory: %w", err)
	}
	// Tighten permissions for pre-existing directories as part of hardening.
	if err := os.Chmod(gauntletDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to set gauntlet directory permissions: %w", err)
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate CA key: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   "Gauntlet Local CA",
			Organization: []string{"Gauntlet"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(2 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("failed to create CA certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	certPath := filepath.Join(gauntletDir, "ca.pem")
	keyPath := filepath.Join(gauntletDir, "ca.key")

	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return nil, fmt.Errorf("failed to write CA cert: %w", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return nil, fmt.Errorf("failed to write CA key: %w", err)
	}
	if err := validateCAAssetPermissions(gauntletDir, certPath, keyPath); err != nil {
		return nil, err
	}

	return &CA{
		Cert:      cert,
		Key:       key,
		CertPEM:   certPEM,
		certCache: make(map[string]*tls.Certificate),
	}, nil
}

// LoadCA loads an existing CA from disk.
func LoadCA(gauntletDir string) (*CA, error) {
	certPath := filepath.Join(gauntletDir, "ca.pem")
	keyPath := filepath.Join(gauntletDir, "ca.key")
	if err := validateCAAssetPermissions(gauntletDir, certPath, keyPath); err != nil {
		return nil, err
	}

	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("CA cert not found at %s — run 'gauntlet enable' first: %w", certPath, err)
	}

	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("CA key not found at %s: %w", keyPath, err)
	}

	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, fmt.Errorf("failed to decode CA cert PEM")
	}

	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CA cert: %w", err)
	}
	now := time.Now().UTC()
	if now.Before(cert.NotBefore.UTC()) {
		return nil, fmt.Errorf("%w: valid from %s", ErrCANotYetValid, cert.NotBefore.UTC().Format(time.RFC3339))
	}
	if !now.Before(cert.NotAfter.UTC()) {
		return nil, fmt.Errorf("%w: expired at %s", ErrCAExpired, cert.NotAfter.UTC().Format(time.RFC3339))
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, fmt.Errorf("failed to decode CA key PEM")
	}

	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CA key: %w", err)
	}

	return &CA{
		Cert:      cert,
		Key:       key,
		CertPEM:   certPEM,
		certCache: make(map[string]*tls.Certificate),
	}, nil
}

// IssueHostCert issues a TLS certificate for the given hostname, signed by the CA.
// Results are cached in memory.
func (ca *CA) IssueHostCert(hostname string) (*tls.Certificate, error) {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	if cached, ok := ca.certCache[hostname]; ok {
		return cached, nil
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			CommonName: hostname,
		},
		DNSNames:    []string{hostname},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, ca.Cert, &key.PublicKey, ca.Key)
	if err != nil {
		return nil, err
	}

	tlsCert := &tls.Certificate{
		Certificate: [][]byte{certDER, ca.Cert.Raw},
		PrivateKey:  key,
	}

	ca.certCache[hostname] = tlsCert
	return tlsCert, nil
}

// EnvVars returns environment variables to inject so the TUT trusts the CA.
func (ca *CA) EnvVars(certPath string) []string {
	return []string{
		"SSL_CERT_FILE=" + certPath,
		"REQUESTS_CA_BUNDLE=" + certPath,
		"NODE_EXTRA_CA_CERTS=" + certPath,
		"CURL_CA_BUNDLE=" + certPath,
	}
}

// CARotationRecommended reports whether the CA should be proactively rotated
// within the given rotation window. Returned remaining duration is relative to
// the supplied `now` timestamp.
func CARotationRecommended(cert *x509.Certificate, now time.Time, rotationWindow time.Duration) (bool, time.Duration) {
	if cert == nil {
		return false, 0
	}
	if rotationWindow <= 0 {
		rotationWindow = DefaultCARotationWindow
	}
	remaining := cert.NotAfter.UTC().Sub(now.UTC())
	return remaining <= rotationWindow, remaining
}

func validateCAAssetPermissions(gauntletDir, certPath, keyPath string) error {
	dirInfo, err := os.Lstat(gauntletDir)
	if err != nil {
		return fmt.Errorf("failed to stat gauntlet CA directory %s: %w", gauntletDir, err)
	}
	if dirInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: gauntlet CA directory is a symlink (%s)", ErrCAInsecurePermissions, gauntletDir)
	}
	if !dirInfo.IsDir() {
		return fmt.Errorf("%w: gauntlet CA path is not a directory (%s)", ErrCAInsecurePermissions, gauntletDir)
	}
	if dirInfo.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%w: gauntlet CA directory is writable by group/other (%s mode=%#o)", ErrCAInsecurePermissions, gauntletDir, dirInfo.Mode().Perm())
	}

	if err := validateCAFilePermissions(certPath, false); err != nil {
		return err
	}
	if err := validateCAFilePermissions(keyPath, true); err != nil {
		return err
	}
	return nil
}

func validateCAFilePermissions(path string, privateKey bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("failed to stat CA asset %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: CA asset is a symlink (%s)", ErrCAInsecurePermissions, path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: CA asset is not a regular file (%s)", ErrCAInsecurePermissions, path)
	}
	perms := info.Mode().Perm()
	if privateKey {
		if perms&0o077 != 0 {
			return fmt.Errorf("%w: CA key permissions are too broad (%s mode=%#o)", ErrCAInsecurePermissions, path, perms)
		}
		return nil
	}
	if perms&0o022 != 0 {
		return fmt.Errorf("%w: CA certificate permissions are writable by group/other (%s mode=%#o)", ErrCAInsecurePermissions, path, perms)
	}
	return nil
}
