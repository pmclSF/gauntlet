package fixture

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pmclSF/gauntlet/internal/proxy/providers"
)

func TestNearestModelFixtureCandidates_PrioritizesProviderAndModel(t *testing.T) {
	store := NewStore(t.TempDir())
	modelsDir := filepath.Join(store.BaseDir, "models")
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatalf("mkdir models: %v", err)
	}

	requested := strings.Repeat("0", 64)
	exactBoth := strings.Repeat("0", 63) + "1"
	providerOnly := strings.Repeat("0", 62) + "22"
	modelOnly := strings.Repeat("0", 63) + "3"
	other := strings.Repeat("f", 64)

	writeCandidateFixtureMeta(t, modelsDir, exactBoth, "openai_compatible", "gpt-4o-mini")
	writeCandidateFixtureMeta(t, modelsDir, providerOnly, "openai_compatible", "gpt-4.1")
	writeCandidateFixtureMeta(t, modelsDir, modelOnly, "anthropic", "gpt-4o-mini")
	writeCandidateFixtureMeta(t, modelsDir, other, "anthropic", "claude-3-5-sonnet")

	candidates, err := store.NearestModelFixtureCandidates("openai_compatible", "gpt-4o-mini", requested, 3)
	if err != nil {
		t.Fatalf("NearestModelFixtureCandidates: %v", err)
	}
	if len(candidates) != 3 {
		t.Fatalf("candidates len = %d, want 3", len(candidates))
	}
	if candidates[0].CanonicalHash != exactBoth {
		t.Fatalf("candidate[0] hash = %s, want %s", candidates[0].CanonicalHash, exactBoth)
	}
	if candidates[1].CanonicalHash != providerOnly {
		t.Fatalf("candidate[1] hash = %s, want %s", candidates[1].CanonicalHash, providerOnly)
	}
	if candidates[2].CanonicalHash != modelOnly {
		t.Fatalf("candidate[2] hash = %s, want %s", candidates[2].CanonicalHash, modelOnly)
	}
}

func TestModelReplayFixtureMissIncludesProviderModelAndCandidates(t *testing.T) {
	store := NewStore(t.TempDir())
	replay := &ModelReplay{Store: store, Suite: "smoke"}

	recorded := &providers.CanonicalRequest{
		GauntletCanonicalVersion: 1,
		ProviderFamily:           "openai_compatible",
		Model:                    "gpt-4o-mini",
		Messages:                 []providers.CanonicalMessage{{Role: "user", Content: "hello"}},
		Sampling:                 providers.CanonicalSampling{},
	}
	recordedCanonical, err := CanonicalizeRequest(recorded)
	if err != nil {
		t.Fatalf("canonicalize recorded: %v", err)
	}
	recordedHash := HashCanonical(recordedCanonical)
	if err := store.PutModelFixture(&ModelFixture{
		FixtureID:        recordedHash,
		HashVersion:      1,
		CanonicalHash:    recordedHash,
		ProviderFamily:   recorded.ProviderFamily,
		Model:            recorded.Model,
		CanonicalRequest: recordedCanonical,
		Response:         json.RawMessage(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`),
		RecordedAt:       time.Now().UTC(),
		RecordedBy:       "test",
	}); err != nil {
		t.Fatalf("put model fixture: %v", err)
	}

	missing := &providers.CanonicalRequest{
		GauntletCanonicalVersion: 1,
		ProviderFamily:           "openai_compatible",
		Model:                    "gpt-4o-mini",
		Messages:                 []providers.CanonicalMessage{{Role: "user", Content: "different"}},
		Sampling:                 providers.CanonicalSampling{},
	}
	_, err = replay.Replay(missing)
	if err == nil {
		t.Fatal("expected fixture miss")
	}
	var miss *ErrFixtureMiss
	if !errors.As(err, &miss) {
		t.Fatalf("expected ErrFixtureMiss, got %T", err)
	}
	if miss.ProviderFamily != "openai_compatible" {
		t.Fatalf("provider_family = %q, want openai_compatible", miss.ProviderFamily)
	}
	if miss.Model != "gpt-4o-mini" {
		t.Fatalf("model = %q, want gpt-4o-mini", miss.Model)
	}
	if len(miss.Candidates) == 0 {
		t.Fatal("expected nearest fixture candidates")
	}
	if miss.Candidates[0].CanonicalHash != recordedHash {
		t.Fatalf("nearest candidate hash = %s, want %s", miss.Candidates[0].CanonicalHash, recordedHash)
	}
	errText := miss.Error()
	if !strings.Contains(errText, "Provider family: openai_compatible") {
		t.Fatalf("error text missing provider family:\n%s", errText)
	}
	if !strings.Contains(errText, "Nearest recorded fixtures:") {
		t.Fatalf("error text missing candidate section:\n%s", errText)
	}
}

func TestErrFixtureMissError_IncludesModelVersionHint(t *testing.T) {
	errText := (&ErrFixtureMiss{
		FixtureType:      "model:gpt-4.1",
		ProviderFamily:   "openai_compatible",
		Model:            "gpt-4.1",
		CanonicalHash:    strings.Repeat("a", 64),
		CanonicalJSON:    `{"model":"gpt-4.1","messages":[]}`,
		RecordCmd:        "gauntlet record --suite smoke",
		ModelVersionHint: "may be a model version change: recorded with gpt-4o-mini, requesting gpt-4.1. Run: gauntlet record --suite smoke",
	}).Error()

	if !strings.Contains(errText, "Hint: may be a model version change") {
		t.Fatalf("expected model version hint in fixture miss error, got:\n%s", errText)
	}
}

func writeCandidateFixtureMeta(t *testing.T, modelsDir, hash, providerFamily, model string) {
	t.Helper()
	content := map[string]string{
		"canonical_hash":  hash,
		"provider_family": providerFamily,
		"model":           model,
	}
	data, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("marshal candidate fixture: %v", err)
	}
	path := filepath.Join(modelsDir, hash+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write candidate fixture %s: %v", path, err)
	}
}
