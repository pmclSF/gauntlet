package output

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
	"testing"
	"time"
)

func TestSignEvidenceBundle_WritesManifestAndVerifiesSignature(t *testing.T) {
	root := t.TempDir()
	artifactsDir := filepath.Join(root, "evals", "runs")
	if err := os.MkdirAll(filepath.Join(artifactsDir, "run-1"), 0o755); err != nil {
		t.Fatalf("mkdir artifacts: %v", err)
	}
	writeFile(t, filepath.Join(artifactsDir, "run-1", "results.json"), `{"suite":"smoke"}`)
	writeFile(t, filepath.Join(artifactsDir, "run-1", "summary.md"), "ok")

	manifestPath := filepath.Join(artifactsDir, DefaultEvidenceManifestName)
	signingKeyPath := filepath.Join(root, ".gauntlet", "evidence-signing-key.pem")

	manifest, err := SignEvidenceBundle(EvidenceSignOptions{
		ArtifactDir:    artifactsDir,
		ManifestPath:   manifestPath,
		SigningKeyPath: signingKeyPath,
		GeneratedAt:    time.Date(2026, time.March, 4, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("SignEvidenceBundle: %v", err)
	}
	if manifest.Version != 1 {
		t.Fatalf("manifest version = %d, want 1", manifest.Version)
	}
	if manifest.GeneratedAt != "2026-03-04T12:00:00Z" {
		t.Fatalf("generated_at = %q", manifest.GeneratedAt)
	}
	if len(manifest.Entries) != 2 {
		t.Fatalf("entries len = %d, want 2", len(manifest.Entries))
	}
	if manifest.Entries[0].Path != "run-1/results.json" || manifest.Entries[1].Path != "run-1/summary.md" {
		t.Fatalf("unexpected entry ordering: %+v", manifest.Entries)
	}

	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var decoded EvidenceManifest
	if err := json.Unmarshal(manifestBytes, &decoded); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if err := verifyEvidenceManifestSignature(decoded); err != nil {
		t.Fatalf("signature verification failed: %v", err)
	}
}

func TestSignEvidenceBundle_ExcludesExistingManifestPath(t *testing.T) {
	root := t.TempDir()
	artifactsDir := filepath.Join(root, "evals", "runs")
	if err := os.MkdirAll(artifactsDir, 0o755); err != nil {
		t.Fatalf("mkdir artifacts: %v", err)
	}
	writeFile(t, filepath.Join(artifactsDir, "results.json"), `{"suite":"smoke"}`)
	manifestPath := filepath.Join(artifactsDir, DefaultEvidenceManifestName)
	writeFile(t, manifestPath, `{"old":"manifest"}`)

	manifest, err := SignEvidenceBundle(EvidenceSignOptions{
		ArtifactDir:    artifactsDir,
		ManifestPath:   manifestPath,
		SigningKeyPath: filepath.Join(root, ".gauntlet", "evidence-signing-key.pem"),
		GeneratedAt:    time.Date(2026, time.March, 4, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("SignEvidenceBundle: %v", err)
	}
	if len(manifest.Entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(manifest.Entries))
	}
	if manifest.Entries[0].Path != "results.json" {
		t.Fatalf("entry path = %q, want results.json", manifest.Entries[0].Path)
	}
}

func verifyEvidenceManifestSignature(manifest EvidenceManifest) error {
	payload := evidencePayload{
		Version:     manifest.Version,
		GeneratedAt: manifest.GeneratedAt,
		ArtifactDir: manifest.ArtifactDir,
		Entries:     manifest.Entries,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	payloadHash := sha256.Sum256(payloadJSON)
	if got := hex.EncodeToString(payloadHash[:]); got != manifest.Signature.PayloadSHA256 {
		return fmt.Errorf("payload sha mismatch: got %s want %s", got, manifest.Signature.PayloadSHA256)
	}

	block, _ := pem.Decode([]byte(manifest.Signature.PublicKeyPEM))
	if block == nil {
		return fmt.Errorf("invalid public key pem")
	}
	pubAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return err
	}
	pub, ok := pubAny.(ed25519.PublicKey)
	if !ok {
		return fmt.Errorf("public key is not ed25519")
	}
	pubHash := sha256.Sum256(pub)
	if got := hex.EncodeToString(pubHash[:]); got != manifest.Signature.KeyFingerprint {
		return fmt.Errorf("key fingerprint mismatch: got %s want %s", got, manifest.Signature.KeyFingerprint)
	}

	sig, err := base64.StdEncoding.DecodeString(manifest.Signature.Value)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, payloadJSON, sig) {
		return fmt.Errorf("ed25519 signature verify failed")
	}
	return nil
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
