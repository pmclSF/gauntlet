package fixture

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gauntlet-dev/gauntlet/internal/proxy/providers"
)

func TestMigrateFixtures_DryRunProducesReportAndManifest(t *testing.T) {
	store := NewStore(t.TempDir())
	cr := &providers.CanonicalRequest{
		GauntletCanonicalVersion: 1,
		ProviderFamily:           "openai_compatible",
		Model:                    "gpt-4o-mini",
		Messages:                 []providers.CanonicalMessage{{Role: "user", Content: "hello"}},
		Sampling:                 providers.CanonicalSampling{},
	}
	canonicalBytes, err := CanonicalizeRequest(cr)
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	hash := HashCanonical(canonicalBytes)
	f := &ModelFixture{
		FixtureID:        hash,
		HashVersion:      1,
		CanonicalHash:    hash,
		ProviderFamily:   "openai_compatible",
		Model:            "gpt-4o-mini",
		CanonicalRequest: canonicalBytes,
		Response:         json.RawMessage(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`),
		RecordedBy:       "test",
		RecordedAt:       time.Now().UTC(),
	}
	if err := store.PutModelFixture(f); err != nil {
		t.Fatalf("put fixture: %v", err)
	}

	reportPath := filepath.Join(store.BaseDir, "migrations", "report.json")
	manifestPath := filepath.Join(store.BaseDir, "migrations", "manifest.json")
	keyPath := filepath.Join(store.BaseDir, ".gauntlet", "migration-key.pem")

	report, err := MigrateFixtures(store, MigrationOptions{
		FromVersion:    1,
		ToVersion:      2,
		DryRun:         true,
		ReportPath:     reportPath,
		ManifestPath:   manifestPath,
		SigningKeyPath: keyPath,
		Now: func() time.Time {
			return time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("MigrateFixtures: %v", err)
	}

	if report.Summary.Migrated != 1 {
		t.Fatalf("migrated = %d, want 1", report.Summary.Migrated)
	}
	if report.Summary.VersionOnly != 1 {
		t.Fatalf("version_only = %d, want 1", report.Summary.VersionOnly)
	}
	if report.Summary.Rehashed != 0 {
		t.Fatalf("rehashed = %d, want 0", report.Summary.Rehashed)
	}

	gotFixture, err := store.GetModelFixture(hash)
	if err != nil {
		t.Fatalf("GetModelFixture: %v", err)
	}
	if gotFixture == nil {
		t.Fatal("expected fixture to still exist")
	}
	if gotFixture.HashVersion != 1 {
		t.Fatalf("dry-run should not rewrite fixture version, got %d", gotFixture.HashVersion)
	}

	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("expected report file: %v", err)
	}
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("expected manifest file: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("expected signing key file: %v", err)
	}
	if err := verifyManifestSignature(manifestPath); err != nil {
		t.Fatalf("manifest signature verification failed: %v", err)
	}
}

func TestMigrateFixtures_ApplyRehashesToolFixture(t *testing.T) {
	store := NewStore(t.TempDir())
	oldHash := strings.Repeat("a", 64)
	f := &ToolFixture{
		FixtureID:     oldHash,
		HashVersion:   1,
		CanonicalHash: oldHash,
		ToolName:      "order_lookup",
		ArgsHash:      oldHash,
		Args:          json.RawMessage(`{"order_id":"ord-001"}`),
		Response:      json.RawMessage(`{"status":"ok"}`),
		RecordedAt:    time.Now().UTC(),
	}
	if err := store.PutToolFixture(f); err != nil {
		t.Fatalf("put tool fixture: %v", err)
	}

	report, err := MigrateFixtures(store, MigrationOptions{
		FromVersion: 1,
		ToVersion:   2,
		Now: func() time.Time {
			return time.Date(2026, 3, 4, 11, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("MigrateFixtures: %v", err)
	}
	if report.Summary.Migrated != 1 || report.Summary.Rehashed != 1 {
		t.Fatalf("unexpected summary: %+v", report.Summary)
	}

	args := map[string]interface{}{"order_id": "ord-001"}
	canonical, err := CanonicalizeToolCall("order_lookup", args)
	if err != nil {
		t.Fatalf("CanonicalizeToolCall: %v", err)
	}
	newHash := HashCanonical(canonical)

	if _, err := os.Stat(filepath.Join(store.BaseDir, "tools", oldHash+".json")); !os.IsNotExist(err) {
		t.Fatalf("expected old fixture file to be removed; err=%v", err)
	}
	newFixture, err := store.GetToolFixture("order_lookup", newHash)
	if err != nil {
		t.Fatalf("GetToolFixture: %v", err)
	}
	if newFixture == nil {
		t.Fatal("expected migrated tool fixture at new hash")
	}
	if newFixture.HashVersion != 2 {
		t.Fatalf("hash_version = %d, want 2", newFixture.HashVersion)
	}
	if newFixture.CanonicalHash != newHash {
		t.Fatalf("canonical_hash = %q, want %q", newFixture.CanonicalHash, newHash)
	}
	if newFixture.Provenance == nil {
		t.Fatal("expected provenance to be populated during migration")
	}
}

func TestMigrateFixtures_FailsOnTargetConflict(t *testing.T) {
	store := NewStore(t.TempDir())
	cr := &providers.CanonicalRequest{
		GauntletCanonicalVersion: 1,
		ProviderFamily:           "openai_compatible",
		Model:                    "gpt-4o-mini",
		Messages:                 []providers.CanonicalMessage{{Role: "user", Content: "hello"}},
		Sampling:                 providers.CanonicalSampling{},
	}
	canonicalBytes, err := CanonicalizeRequest(cr)
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	newHash := HashCanonical(canonicalBytes)
	oldHash := strings.Repeat("b", 64)
	f := &ModelFixture{
		FixtureID:        oldHash,
		HashVersion:      1,
		CanonicalHash:    oldHash,
		ProviderFamily:   "openai_compatible",
		Model:            "gpt-4o-mini",
		CanonicalRequest: canonicalBytes,
		Response:         json.RawMessage(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`),
		RecordedBy:       "test",
		RecordedAt:       time.Now().UTC(),
	}
	modelDir := filepath.Join(store.BaseDir, "models")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("mkdir models: %v", err)
	}
	oldPath := filepath.Join(modelDir, oldHash+".json")
	oldBytes, _ := json.MarshalIndent(f, "", "  ")
	if err := os.WriteFile(oldPath, oldBytes, 0o644); err != nil {
		t.Fatalf("write old fixture: %v", err)
	}
	// Occupy target hash path with different content to trigger collision.
	if err := os.WriteFile(filepath.Join(modelDir, newHash+".json"), []byte(`{"different":"content"}`), 0o644); err != nil {
		t.Fatalf("write conflicting fixture: %v", err)
	}

	_, err = MigrateFixtures(store, MigrationOptions{
		FromVersion: 1,
		ToVersion:   2,
	})
	if err == nil {
		t.Fatal("expected target collision error")
	}
	if !strings.Contains(err.Error(), "already exists with different content") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func verifyManifestSignature(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var manifest MigrationManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return err
	}

	payload := migrationPayload{
		Version:      manifest.Version,
		GeneratedAt:  manifest.GeneratedAt,
		StoreBaseDir: manifest.StoreBaseDir,
		FromVersion:  manifest.FromVersion,
		ToVersion:    manifest.ToVersion,
		DryRun:       manifest.DryRun,
		Summary:      manifest.Summary,
		Entries:      manifest.Entries,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	block, _ := pem.Decode([]byte(manifest.Signature.PublicKeyPEM))
	if block == nil {
		return os.ErrInvalid
	}
	pubAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return err
	}
	pub, ok := pubAny.(ed25519.PublicKey)
	if !ok {
		return os.ErrInvalid
	}
	sig, err := base64.StdEncoding.DecodeString(manifest.Signature.Value)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, payloadJSON, sig) {
		return os.ErrInvalid
	}
	return nil
}
