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

func TestGetModelFixture_FailsOnMalformedCanonicalRequest(t *testing.T) {
	store := NewStore(t.TempDir())
	hash := strings.Repeat("1", 64)
	path := filepath.Join(store.BaseDir, "models", hash+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `{
  "fixture_id": "` + hash + `",
  "hash_version": 1,
  "canonical_hash": "` + hash + `",
  "provider_family": "openai_compatible",
  "model": "gpt-4o-mini",
  "canonical_request": "not-json",
  "response": {"choices":[{"message":{"role":"assistant","content":"ok"}}]},
  "recorded_at": "2026-03-04T00:00:00Z",
  "recorded_by": "test"
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, err := store.GetModelFixture(hash)
	if err == nil {
		t.Fatal("expected malformed canonical_request error")
	}
}

func TestGetToolFixture_DetectsFlatAndNamespacedCollision(t *testing.T) {
	store := NewStore(t.TempDir())
	hash := strings.Repeat("2", 64)
	flat := filepath.Join(store.BaseDir, "tools", hash+".json")
	ns := filepath.Join(store.BaseDir, "tools", "order_lookup", hash+".json")
	if err := os.MkdirAll(filepath.Dir(flat), 0o755); err != nil {
		t.Fatalf("mkdir flat: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(ns), 0o755); err != nil {
		t.Fatalf("mkdir ns: %v", err)
	}
	if err := os.WriteFile(flat, []byte(`{"canonical_hash":"`+hash+`","tool_name":"order_lookup","args":{"order_id":"a"},"response":{"ok":true}}`), 0o644); err != nil {
		t.Fatalf("write flat: %v", err)
	}
	if err := os.WriteFile(ns, []byte(`{"canonical_hash":"`+hash+`","tool_name":"order_lookup","args":{"order_id":"b"},"response":{"ok":true}}`), 0o644); err != nil {
		t.Fatalf("write ns: %v", err)
	}
	_, err := store.GetToolFixture("order_lookup", hash)
	if err == nil {
		t.Fatal("expected collision error")
	}
	if !strings.Contains(err.Error(), "collision") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetModelFixture_EnforcesReplaySuiteContextWhenPresent(t *testing.T) {
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
		RecordedBy:       "test",
		Suite:            "smoke",
	}
	if err := store.PutModelFixture(f); err != nil {
		t.Fatalf("put model fixture: %v", err)
	}
	store.SetReplayContext("full", "")
	_, err = store.GetModelFixture(hash)
	if err == nil {
		t.Fatal("expected suite context mismatch")
	}
}
