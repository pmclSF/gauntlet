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

func TestWriteAndVerifyReplayLockfile(t *testing.T) {
	store := NewStore(t.TempDir())
	makeValidModelFixture(t, store, "smoke", "digest-1")

	lock, path, err := WriteReplayLockfile(store, "smoke", "digest-1", "", time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("WriteReplayLockfile: %v", err)
	}
	if path == "" {
		t.Fatal("expected lockfile path")
	}
	if lock.IndexSHA256 == "" {
		t.Fatal("expected index hash")
	}

	if err := VerifyReplayLockfile(store, "smoke", "digest-1", path); err != nil {
		t.Fatalf("VerifyReplayLockfile: %v", err)
	}
}

func TestVerifyReplayLockfile_DetectsTampering(t *testing.T) {
	store := NewStore(t.TempDir())
	hash := makeValidModelFixture(t, store, "smoke", "digest-1")
	_, path, err := WriteReplayLockfile(store, "smoke", "digest-1", "", time.Now().UTC())
	if err != nil {
		t.Fatalf("WriteReplayLockfile: %v", err)
	}

	fixturePath := filepath.Join(store.BaseDir, "models", hash+".json")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	data = []byte(strings.Replace(string(data), `"ok"`, `"tampered"`, 1))
	if err := os.WriteFile(fixturePath, data, 0o644); err != nil {
		t.Fatalf("write tampered fixture: %v", err)
	}

	err = VerifyReplayLockfile(store, "smoke", "digest-1", path)
	if err == nil {
		t.Fatal("expected integrity mismatch error")
	}
	if !strings.Contains(err.Error(), "integrity mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifyReplayLockfile_SuiteMismatch(t *testing.T) {
	store := NewStore(t.TempDir())
	makeValidModelFixture(t, store, "smoke", "digest-1")
	_, path, err := WriteReplayLockfile(store, "smoke", "digest-1", "", time.Now().UTC())
	if err != nil {
		t.Fatalf("WriteReplayLockfile: %v", err)
	}
	err = VerifyReplayLockfile(store, "full", "digest-1", path)
	if err == nil {
		t.Fatal("expected suite mismatch")
	}
}

func TestScenarioSetDigest_Deterministic(t *testing.T) {
	a := ScenarioSetDigest([]string{"b", "a"})
	b := ScenarioSetDigest([]string{"a", "b", "a"})
	if a != b {
		t.Fatalf("expected deterministic digest, got %s vs %s", a, b)
	}
}

func TestWriteReplayLockfile_NormalizesFixturesDirSlashes(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), `evals\fixtures`)
	store := NewStore(baseDir)
	makeValidModelFixture(t, store, "smoke", "digest-1")

	lock, _, err := WriteReplayLockfile(store, "smoke", "digest-1", "", time.Now().UTC())
	if err != nil {
		t.Fatalf("WriteReplayLockfile: %v", err)
	}
	if strings.Contains(lock.FixturesDir, `\`) {
		t.Fatalf("fixtures_dir should use forward slashes for cross-platform determinism: %s", lock.FixturesDir)
	}
}

func TestWriteReplayLockfile_FailsOnHashCollisionAcrossPaths(t *testing.T) {
	store := NewStore(t.TempDir())
	hash := strings.Repeat("c", 64)
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
	_, _, err := WriteReplayLockfile(store, "smoke", "digest-1", "", time.Now().UTC())
	if err == nil {
		t.Fatal("expected collision error")
	}
	if !strings.Contains(err.Error(), "collision") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func makeValidModelFixture(t *testing.T, store *Store, suite, scenarioDigest string) string {
	t.Helper()
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
		FixtureID:         hash,
		HashVersion:       1,
		CanonicalHash:     hash,
		ProviderFamily:    "openai_compatible",
		Model:             "gpt-4o-mini",
		CanonicalRequest:  canonicalBytes,
		Response:          json.RawMessage(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`),
		RecordedAt:        time.Now().UTC(),
		RecordedBy:        "test",
		Suite:             suite,
		ScenarioSetSHA256: scenarioDigest,
	}
	if err := store.PutModelFixture(f); err != nil {
		t.Fatalf("put model fixture: %v", err)
	}
	return hash
}
