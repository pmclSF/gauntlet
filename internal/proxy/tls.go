package proxy

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CA holds the local CA certificate and key for MITM TLS interception.
type CA struct {
	Cert    *x509.Certificate
	Key     *rsa.PrivateKey
	CertPEM []byte

	mu       sync.Mutex
	certCache map[string]*tls.Certificate
}

// GenerateCA creates a new RSA-2048 CA certificate and key.
// Saved to gauntletDir/ca.pem and gauntletDir/ca.key.
func GenerateCA(gauntletDir string) (*CA, error) {
	if err := os.MkdirAll(gauntletDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create gauntlet directory: %w", err)
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
		DNSNames:  []string{hostname},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(24 * time.Hour),
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
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
