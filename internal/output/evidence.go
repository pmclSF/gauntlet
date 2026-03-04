package output

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// DefaultEvidenceManifestName is the default signed evidence bundle manifest.
	DefaultEvidenceManifestName = "evidence.manifest.json"
)

// EvidenceSignOptions controls evidence-bundle signing behavior.
type EvidenceSignOptions struct {
	ArtifactDir    string
	ManifestPath   string
	SigningKeyPath string
	GeneratedAt    time.Time
}

// EvidenceFileEntry contains a deterministic file digest record.
type EvidenceFileEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// EvidenceSignature contains signature metadata for the manifest payload.
type EvidenceSignature struct {
	Algorithm      string `json:"algorithm"`
	KeyPath        string `json:"key_path,omitempty"`
	KeyFingerprint string `json:"key_fingerprint"`
	PublicKeyPEM   string `json:"public_key_pem"`
	PayloadSHA256  string `json:"payload_sha256"`
	Value          string `json:"value"`
}

// EvidenceManifest is the signed evidence bundle summary.
type EvidenceManifest struct {
	Version     int                 `json:"version"`
	GeneratedAt string              `json:"generated_at"`
	ArtifactDir string              `json:"artifact_dir"`
	Entries     []EvidenceFileEntry `json:"entries"`
	Signature   EvidenceSignature   `json:"signature"`
}

type evidencePayload struct {
	Version     int                 `json:"version"`
	GeneratedAt string              `json:"generated_at"`
	ArtifactDir string              `json:"artifact_dir"`
	Entries     []EvidenceFileEntry `json:"entries"`
}

// SignEvidenceBundle writes a signed evidence manifest for all files under
// ArtifactDir (excluding the manifest file itself).
func SignEvidenceBundle(opts EvidenceSignOptions) (*EvidenceManifest, error) {
	artifactDir := filepath.Clean(strings.TrimSpace(opts.ArtifactDir))
	if artifactDir == "" || artifactDir == "." {
		return nil, fmt.Errorf("artifact directory is required")
	}
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create artifact directory %s: %w", artifactDir, err)
	}

	manifestPath := strings.TrimSpace(opts.ManifestPath)
	if manifestPath == "" {
		manifestPath = filepath.Join(artifactDir, DefaultEvidenceManifestName)
	}
	manifestPath = filepath.Clean(manifestPath)

	signingKeyPath := strings.TrimSpace(opts.SigningKeyPath)
	if signingKeyPath == "" {
		signingKeyPath = filepath.Join(filepath.Dir(artifactDir), ".gauntlet", "evidence-signing-key.pem")
	}
	signingKeyPath = filepath.Clean(signingKeyPath)

	generatedAt := opts.GeneratedAt.UTC()
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}

	entries, err := collectEvidenceEntries(artifactDir, manifestPath)
	if err != nil {
		return nil, err
	}

	payload := evidencePayload{
		Version:     1,
		GeneratedAt: generatedAt.Format(time.RFC3339),
		ArtifactDir: artifactDir,
		Entries:     entries,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal evidence payload: %w", err)
	}

	priv, pubPEM, err := loadOrCreateEd25519Key(signingKeyPath)
	if err != nil {
		return nil, err
	}
	sig := ed25519.Sign(priv, payloadJSON)
	pub := priv.Public().(ed25519.PublicKey)
	pubHash := sha256.Sum256(pub)
	payloadHash := sha256.Sum256(payloadJSON)

	manifest := &EvidenceManifest{
		Version:     payload.Version,
		GeneratedAt: payload.GeneratedAt,
		ArtifactDir: payload.ArtifactDir,
		Entries:     payload.Entries,
		Signature: EvidenceSignature{
			Algorithm:      "ed25519",
			KeyPath:        signingKeyPath,
			KeyFingerprint: hex.EncodeToString(pubHash[:]),
			PublicKeyPEM:   string(pubPEM),
			PayloadSHA256:  hex.EncodeToString(payloadHash[:]),
			Value:          base64.StdEncoding.EncodeToString(sig),
		},
	}

	if err := writeEvidenceManifest(manifestPath, manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

func collectEvidenceEntries(artifactDir, manifestPath string) ([]EvidenceFileEntry, error) {
	entries := make([]EvidenceFileEntry, 0, 16)
	err := filepath.WalkDir(artifactDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Clean(path) == manifestPath {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(data)
		rel, err := filepath.Rel(artifactDir, path)
		if err != nil {
			return err
		}
		entries = append(entries, EvidenceFileEntry{
			Path:   filepath.ToSlash(filepath.Clean(rel)),
			Size:   info.Size(),
			SHA256: hex.EncodeToString(hash[:]),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to collect evidence entries under %s: %w", artifactDir, err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})
	return entries, nil
}

func writeEvidenceManifest(path string, manifest *EvidenceManifest) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create manifest directory %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal evidence manifest: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write evidence manifest %s: %w", path, err)
	}
	return nil
}

func loadOrCreateEd25519Key(path string) (ed25519.PrivateKey, []byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return parseEd25519PrivateKey(path, data)
	}
	if !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("failed to read signing key %s: %w", path, err)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate signing key: %w", err)
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal signing key: %w", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})

	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal signing public key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, fmt.Errorf("failed to create signing key directory: %w", err)
	}
	if err := os.WriteFile(path, privPEM, 0o600); err != nil {
		return nil, nil, fmt.Errorf("failed to write signing key %s: %w", path, err)
	}
	if err := os.WriteFile(path+".pub.pem", pubPEM, 0o644); err != nil {
		return nil, nil, fmt.Errorf("failed to write signing public key %s.pub.pem: %w", path, err)
	}
	return priv, pubPEM, nil
}

func parseEd25519PrivateKey(path string, data []byte) (ed25519.PrivateKey, []byte, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, nil, fmt.Errorf("failed to parse signing key %s: invalid PEM", path)
	}
	keyAny, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse signing key %s: %w", path, err)
	}
	priv, ok := keyAny.(ed25519.PrivateKey)
	if !ok {
		return nil, nil, fmt.Errorf("failed to parse signing key %s: key is not ed25519 private key", path)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(priv.Public().(ed25519.PublicKey))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal signing public key %s: %w", path, err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	return priv, pubPEM, nil
}
