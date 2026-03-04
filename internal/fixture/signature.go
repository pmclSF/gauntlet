package fixture

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const fixtureSignatureAlgorithm = "ed25519"

// FixtureSignature captures cryptographic metadata used to verify fixture
// authenticity and recorder identity in replay mode.
type FixtureSignature struct {
	Algorithm      string `json:"algorithm"`
	KeyFingerprint string `json:"key_fingerprint"`
	PublicKeyPEM   string `json:"public_key_pem"`
	SignerIdentity string `json:"signer_identity,omitempty"`
	PayloadSHA256  string `json:"payload_sha256"`
	Value          string `json:"value"`
}

// FixtureTrustOptions controls replay trust enforcement.
type FixtureTrustOptions struct {
	RequireSignatures         bool
	TrustedPublicKeyPaths     []string
	TrustedRecorderIdentities []string
}

type fixtureSigner struct {
	privateKey     ed25519.PrivateKey
	publicKeyPEM   []byte
	keyFingerprint string
}

// EnableFixtureSigning enables Ed25519 signing for newly written fixtures.
// If the key does not exist, it is generated.
func (s *Store) EnableFixtureSigning(signingKeyPath string) error {
	signingKeyPath = strings.TrimSpace(signingKeyPath)
	if signingKeyPath == "" {
		return fmt.Errorf("fixture signing key path is required")
	}
	priv, pubPEM, err := loadOrCreateSigningKey(signingKeyPath)
	if err != nil {
		return fmt.Errorf("failed to initialize fixture signing key %s: %w", signingKeyPath, err)
	}
	pub, err := parsePublicKeyPEM(pubPEM)
	if err != nil {
		return fmt.Errorf("failed to parse fixture signing public key %s.pub.pem: %w", signingKeyPath, err)
	}
	s.fixtureSigner = &fixtureSigner{
		privateKey:     priv,
		publicKeyPEM:   append([]byte{}, pubPEM...),
		keyFingerprint: publicKeyFingerprint(pub),
	}
	return nil
}

// ConfigureFixtureTrust configures replay-time fixture trust policy.
func (s *Store) ConfigureFixtureTrust(opts FixtureTrustOptions) error {
	s.requireFixtureSignatures = opts.RequireSignatures
	s.trustedKeyFingerprints = nil
	s.trustedRecorderIDs = nil

	fingerprints := map[string]bool{}
	for _, rawPath := range opts.TrustedPublicKeyPaths {
		path := strings.TrimSpace(rawPath)
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read trusted fixture public key %s: %w", path, err)
		}
		pub, err := parsePublicKeyPEM(data)
		if err != nil {
			return fmt.Errorf("failed to parse trusted fixture public key %s: %w", path, err)
		}
		fingerprints[publicKeyFingerprint(pub)] = true
	}
	if len(fingerprints) > 0 {
		s.trustedKeyFingerprints = fingerprints
	}

	ids := map[string]bool{}
	for _, raw := range opts.TrustedRecorderIdentities {
		id := strings.ToLower(strings.TrimSpace(raw))
		if id == "" {
			continue
		}
		ids[id] = true
	}
	if len(ids) > 0 {
		s.trustedRecorderIDs = ids
	}

	if opts.RequireSignatures && len(s.trustedKeyFingerprints) == 0 {
		return fmt.Errorf("fixture trust requires at least one trusted public key path")
	}
	if len(s.trustedRecorderIDs) > 0 && len(s.trustedKeyFingerprints) == 0 {
		return fmt.Errorf("trusted recorder identity checks require at least one trusted public key")
	}
	return nil
}

func (s *Store) signModelFixture(f *ModelFixture) error {
	if f == nil || s.fixtureSigner == nil {
		return nil
	}
	payload, err := modelFixtureSignaturePayload(f)
	if err != nil {
		return fmt.Errorf("failed to build model fixture signature payload: %w", err)
	}
	sum := sha256.Sum256(payload)
	sig := ed25519.Sign(s.fixtureSigner.privateKey, payload)
	f.Signature = &FixtureSignature{
		Algorithm:      fixtureSignatureAlgorithm,
		KeyFingerprint: s.fixtureSigner.keyFingerprint,
		PublicKeyPEM:   string(s.fixtureSigner.publicKeyPEM),
		SignerIdentity: recorderIdentityFromProvenance(f.Provenance),
		PayloadSHA256:  hex.EncodeToString(sum[:]),
		Value:          base64.StdEncoding.EncodeToString(sig),
	}
	return nil
}

func (s *Store) signToolFixture(f *ToolFixture) error {
	if f == nil || s.fixtureSigner == nil {
		return nil
	}
	payload, err := toolFixtureSignaturePayload(f)
	if err != nil {
		return fmt.Errorf("failed to build tool fixture signature payload: %w", err)
	}
	sum := sha256.Sum256(payload)
	sig := ed25519.Sign(s.fixtureSigner.privateKey, payload)
	f.Signature = &FixtureSignature{
		Algorithm:      fixtureSignatureAlgorithm,
		KeyFingerprint: s.fixtureSigner.keyFingerprint,
		PublicKeyPEM:   string(s.fixtureSigner.publicKeyPEM),
		SignerIdentity: recorderIdentityFromProvenance(f.Provenance),
		PayloadSHA256:  hex.EncodeToString(sum[:]),
		Value:          base64.StdEncoding.EncodeToString(sig),
	}
	return nil
}

func signModelFixtureWithKeyPath(f *ModelFixture, signingKeyPath string) error {
	if f == nil {
		return nil
	}
	priv, pubPEM, err := loadOrCreateSigningKey(signingKeyPath)
	if err != nil {
		return fmt.Errorf("failed to initialize fixture signing key %s: %w", signingKeyPath, err)
	}
	pub, err := parsePublicKeyPEM(pubPEM)
	if err != nil {
		return fmt.Errorf("failed to parse fixture signing public key %s.pub.pem: %w", signingKeyPath, err)
	}
	payload, err := modelFixtureSignaturePayload(f)
	if err != nil {
		return fmt.Errorf("failed to build model fixture signature payload: %w", err)
	}
	sum := sha256.Sum256(payload)
	sig := ed25519.Sign(priv, payload)
	f.Signature = &FixtureSignature{
		Algorithm:      fixtureSignatureAlgorithm,
		KeyFingerprint: publicKeyFingerprint(pub),
		PublicKeyPEM:   string(pubPEM),
		SignerIdentity: recorderIdentityFromProvenance(f.Provenance),
		PayloadSHA256:  hex.EncodeToString(sum[:]),
		Value:          base64.StdEncoding.EncodeToString(sig),
	}
	return nil
}

func signToolFixtureWithKeyPath(f *ToolFixture, signingKeyPath string) error {
	if f == nil {
		return nil
	}
	priv, pubPEM, err := loadOrCreateSigningKey(signingKeyPath)
	if err != nil {
		return fmt.Errorf("failed to initialize fixture signing key %s: %w", signingKeyPath, err)
	}
	pub, err := parsePublicKeyPEM(pubPEM)
	if err != nil {
		return fmt.Errorf("failed to parse fixture signing public key %s.pub.pem: %w", signingKeyPath, err)
	}
	payload, err := toolFixtureSignaturePayload(f)
	if err != nil {
		return fmt.Errorf("failed to build tool fixture signature payload: %w", err)
	}
	sum := sha256.Sum256(payload)
	sig := ed25519.Sign(priv, payload)
	f.Signature = &FixtureSignature{
		Algorithm:      fixtureSignatureAlgorithm,
		KeyFingerprint: publicKeyFingerprint(pub),
		PublicKeyPEM:   string(pubPEM),
		SignerIdentity: recorderIdentityFromProvenance(f.Provenance),
		PayloadSHA256:  hex.EncodeToString(sum[:]),
		Value:          base64.StdEncoding.EncodeToString(sig),
	}
	return nil
}

func (s *Store) validateModelFixtureSignature(path string, f *ModelFixture) error {
	payload, err := modelFixtureSignaturePayload(f)
	if err != nil {
		return fmt.Errorf("model fixture %s signature payload generation failed: %w", path, err)
	}
	return s.validateFixtureSignature(path, payload, f.Signature, f.Provenance)
}

func (s *Store) validateToolFixtureSignature(path string, f *ToolFixture) error {
	payload, err := toolFixtureSignaturePayload(f)
	if err != nil {
		return fmt.Errorf("tool fixture %s signature payload generation failed: %w", path, err)
	}
	return s.validateFixtureSignature(path, payload, f.Signature, f.Provenance)
}

func (s *Store) validateFixtureSignature(path string, payload []byte, sig *FixtureSignature, provenance *Provenance) error {
	if sig == nil {
		if s.requireFixtureSignatures || len(s.trustedRecorderIDs) > 0 || len(s.trustedKeyFingerprints) > 0 {
			return fmt.Errorf("fixture %s missing signature", path)
		}
		return nil
	}

	algo := strings.ToLower(strings.TrimSpace(sig.Algorithm))
	if algo != fixtureSignatureAlgorithm {
		return fmt.Errorf("fixture %s uses unsupported signature algorithm %q", path, sig.Algorithm)
	}

	pub, err := parsePublicKeyPEM([]byte(sig.PublicKeyPEM))
	if err != nil {
		return fmt.Errorf("fixture %s has invalid signature public key: %w", path, err)
	}

	fp := publicKeyFingerprint(pub)
	if strings.TrimSpace(sig.KeyFingerprint) == "" {
		return fmt.Errorf("fixture %s signature missing key_fingerprint", path)
	}
	if sig.KeyFingerprint != fp {
		return fmt.Errorf("fixture %s signature key fingerprint mismatch", path)
	}
	if len(s.trustedKeyFingerprints) > 0 && !s.trustedKeyFingerprints[fp] {
		return fmt.Errorf("fixture %s signed by untrusted key fingerprint %s", path, fp)
	}

	sum := sha256.Sum256(payload)
	payloadHash := hex.EncodeToString(sum[:])
	if strings.TrimSpace(sig.PayloadSHA256) == "" {
		return fmt.Errorf("fixture %s signature missing payload_sha256", path)
	}
	if sig.PayloadSHA256 != payloadHash {
		return fmt.Errorf("fixture %s signature payload hash mismatch", path)
	}

	rawSig, err := base64.StdEncoding.DecodeString(sig.Value)
	if err != nil {
		return fmt.Errorf("fixture %s has invalid base64 signature: %w", path, err)
	}
	if !ed25519.Verify(pub, payload, rawSig) {
		return fmt.Errorf("fixture %s signature verification failed", path)
	}

	signerID := strings.TrimSpace(sig.SignerIdentity)
	provenanceID := recorderIdentityFromProvenance(provenance)
	effectiveID := signerID
	if effectiveID == "" {
		effectiveID = provenanceID
	}
	if signerID != "" && provenanceID != "" && !strings.EqualFold(signerID, provenanceID) {
		return fmt.Errorf("fixture %s recorder identity mismatch between signature (%q) and provenance (%q)", path, signerID, provenanceID)
	}
	if s.requireFixtureSignatures && strings.TrimSpace(effectiveID) == "" {
		return fmt.Errorf("fixture %s signature missing signer identity", path)
	}
	if len(s.trustedRecorderIDs) > 0 {
		if strings.TrimSpace(effectiveID) == "" {
			return fmt.Errorf("fixture %s missing recorder identity required by trust policy", path)
		}
		if !s.trustedRecorderIDs[strings.ToLower(strings.TrimSpace(effectiveID))] {
			return fmt.Errorf("fixture %s recorder identity %q is not trusted", path, effectiveID)
		}
	}

	return nil
}

func modelFixtureSignaturePayload(f *ModelFixture) ([]byte, error) {
	clone := *f
	clone.Signature = nil
	return json.Marshal(&clone)
}

func toolFixtureSignaturePayload(f *ToolFixture) ([]byte, error) {
	clone := *f
	clone.Signature = nil
	return json.Marshal(&clone)
}

func parsePublicKeyPEM(data []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("invalid PEM")
	}
	pubAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	pub, ok := pubAny.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("key is not ed25519 public key")
	}
	return pub, nil
}

func publicKeyFingerprint(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:])
}

func recorderIdentityFromProvenance(p *Provenance) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.RecorderIdentity)
}

// DefaultFixtureSigningKeyPath returns the default private signing key path for
// fixture recording.
func DefaultFixtureSigningKeyPath(configPath string) string {
	base := filepath.Dir(strings.TrimSpace(configPath))
	if base == "" {
		base = "."
	}
	return filepath.Join(base, ".gauntlet", "fixture-signing-key.pem")
}
