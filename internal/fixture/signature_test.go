package fixture

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gauntlet-dev/gauntlet/internal/proxy/providers"
)

func TestPutModelFixture_SignsAndVerifiesWithTrustedKey(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	keyPath := filepath.Join(root, ".gauntlet", "fixture-signing-key.pem")
	if err := store.EnableFixtureSigning(keyPath); err != nil {
		t.Fatalf("EnableFixtureSigning: %v", err)
	}

	cr := &providers.CanonicalRequest{
		GauntletCanonicalVersion: 1,
		ProviderFamily:           "openai_compatible",
		Model:                    "gpt-4o-mini",
		Messages:                 []providers.CanonicalMessage{{Role: "user", Content: "hello"}},
		Sampling:                 providers.CanonicalSampling{},
	}
	canonicalBytes, err := CanonicalizeRequest(cr)
	if err != nil {
		t.Fatalf("canonicalize request: %v", err)
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
		RecordedAt:       time.Now().UTC(),
		RecordedBy:       "live",
		Provenance: &Provenance{
			RecorderIdentity: "ci-bot",
			CommitSHA:        "abc123",
		},
	}
	if err := store.PutModelFixture(f); err != nil {
		t.Fatalf("PutModelFixture: %v", err)
	}
	if f.Signature == nil {
		t.Fatal("expected signed fixture")
	}
	if f.Signature.Algorithm != fixtureSignatureAlgorithm {
		t.Fatalf("algorithm = %q", f.Signature.Algorithm)
	}
	if f.Signature.Value == "" {
		t.Fatal("signature value is empty")
	}

	verifyStore := NewStore(root)
	if err := verifyStore.ConfigureFixtureTrust(FixtureTrustOptions{
		RequireSignatures:         true,
		TrustedPublicKeyPaths:     []string{keyPath + ".pub.pem"},
		TrustedRecorderIdentities: []string{"ci-bot"},
	}); err != nil {
		t.Fatalf("ConfigureFixtureTrust: %v", err)
	}
	got, err := verifyStore.GetModelFixture(hash)
	if err != nil {
		t.Fatalf("GetModelFixture verify failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected fixture")
	}
}

func TestGetModelFixture_RejectsTamperedSignedFixture(t *testing.T) {
	root := t.TempDir()
	keyPath := filepath.Join(root, ".gauntlet", "fixture-signing-key.pem")
	store := NewStore(root)
	if err := store.EnableFixtureSigning(keyPath); err != nil {
		t.Fatalf("EnableFixtureSigning: %v", err)
	}

	cr := &providers.CanonicalRequest{
		GauntletCanonicalVersion: 1,
		ProviderFamily:           "openai_compatible",
		Model:                    "gpt-4o-mini",
		Messages:                 []providers.CanonicalMessage{{Role: "user", Content: "hello"}},
		Sampling:                 providers.CanonicalSampling{},
	}
	canonicalBytes, err := CanonicalizeRequest(cr)
	if err != nil {
		t.Fatalf("canonicalize request: %v", err)
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
		RecordedAt:       time.Now().UTC(),
		RecordedBy:       "live",
		Provenance:       &Provenance{RecorderIdentity: "ci-bot"},
	}
	if err := store.PutModelFixture(f); err != nil {
		t.Fatalf("PutModelFixture: %v", err)
	}

	path := filepath.Join(root, "models", hash+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	tampered := strings.Replace(string(raw), `"ok"`, `"tampered"`, 1)
	if err := os.WriteFile(path, []byte(tampered), 0o644); err != nil {
		t.Fatalf("write tampered fixture: %v", err)
	}

	verifyStore := NewStore(root)
	if err := verifyStore.ConfigureFixtureTrust(FixtureTrustOptions{
		RequireSignatures:     true,
		TrustedPublicKeyPaths: []string{keyPath + ".pub.pem"},
	}); err != nil {
		t.Fatalf("ConfigureFixtureTrust: %v", err)
	}
	_, err = verifyStore.GetModelFixture(hash)
	if err == nil {
		t.Fatal("expected signature verification failure")
	}
	if !strings.Contains(err.Error(), "payload hash mismatch") && !strings.Contains(err.Error(), "signature verification failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetModelFixture_RejectsUntrustedRecorderIdentity(t *testing.T) {
	root := t.TempDir()
	keyPath := filepath.Join(root, ".gauntlet", "fixture-signing-key.pem")
	store := NewStore(root)
	if err := store.EnableFixtureSigning(keyPath); err != nil {
		t.Fatalf("EnableFixtureSigning: %v", err)
	}

	cr := &providers.CanonicalRequest{
		GauntletCanonicalVersion: 1,
		ProviderFamily:           "openai_compatible",
		Model:                    "gpt-4o-mini",
		Messages:                 []providers.CanonicalMessage{{Role: "user", Content: "hello"}},
		Sampling:                 providers.CanonicalSampling{},
	}
	canonicalBytes, err := CanonicalizeRequest(cr)
	if err != nil {
		t.Fatalf("canonicalize request: %v", err)
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
		RecordedAt:       time.Now().UTC(),
		RecordedBy:       "live",
		Provenance:       &Provenance{RecorderIdentity: "ci-bot"},
	}
	if err := store.PutModelFixture(f); err != nil {
		t.Fatalf("PutModelFixture: %v", err)
	}

	verifyStore := NewStore(root)
	if err := verifyStore.ConfigureFixtureTrust(FixtureTrustOptions{
		RequireSignatures:         true,
		TrustedPublicKeyPaths:     []string{keyPath + ".pub.pem"},
		TrustedRecorderIdentities: []string{"release-bot"},
	}); err != nil {
		t.Fatalf("ConfigureFixtureTrust: %v", err)
	}
	_, err = verifyStore.GetModelFixture(hash)
	if err == nil {
		t.Fatal("expected untrusted identity failure")
	}
	if !strings.Contains(err.Error(), "not trusted") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetToolFixture_RejectsMissingSignatureWhenRequired(t *testing.T) {
	root := t.TempDir()
	keyPath := filepath.Join(root, ".gauntlet", "fixture-signing-key.pem")
	if _, _, err := loadOrCreateSigningKey(keyPath); err != nil {
		t.Fatalf("create signing key: %v", err)
	}

	store := NewStore(root)
	args := map[string]interface{}{"order_id": "ord-001"}
	canonical, err := CanonicalizeToolCall("order_lookup", args)
	if err != nil {
		t.Fatalf("canonicalize tool call: %v", err)
	}
	hash := HashCanonical(canonical)
	f := &ToolFixture{
		FixtureID:     hash,
		HashVersion:   1,
		CanonicalHash: hash,
		ToolName:      "order_lookup",
		ArgsHash:      hash,
		Args:          json.RawMessage(`{"order_id":"ord-001"}`),
		Response:      json.RawMessage(`{"status":"ok"}`),
		RecordedAt:    time.Now().UTC(),
		Provenance:    &Provenance{RecorderIdentity: "ci-bot"},
	}
	if err := store.PutToolFixture(f); err != nil {
		t.Fatalf("PutToolFixture: %v", err)
	}

	verifyStore := NewStore(root)
	if err := verifyStore.ConfigureFixtureTrust(FixtureTrustOptions{
		RequireSignatures:     true,
		TrustedPublicKeyPaths: []string{keyPath + ".pub.pem"},
	}); err != nil {
		t.Fatalf("ConfigureFixtureTrust: %v", err)
	}
	_, err = verifyStore.GetToolFixture("order_lookup", hash)
	if err == nil {
		t.Fatal("expected missing signature error")
	}
	if !strings.Contains(err.Error(), "missing signature") {
		t.Fatalf("unexpected error: %v", err)
	}
}
