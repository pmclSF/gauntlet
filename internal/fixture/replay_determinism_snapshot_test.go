package fixture

import (
	"encoding/json"
	"reflect"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/pmclSF/gauntlet/internal/proxy/providers"
)

type replayDeterminismSnapshot struct {
	ScenarioSetSHA256 string   `json:"scenario_set_sha256"`
	ModelHash         string   `json:"model_hash"`
	ToolHash          string   `json:"tool_hash"`
	EntryPaths        []string `json:"entry_paths"`
	EntriesSHA256     string   `json:"entries_sha256"`
	IndexSHA256       string   `json:"index_sha256"`
}

func TestReplayDeterminismSnapshot_PlatformInvariant(t *testing.T) {
	testedTargets := []string{
		"linux/amd64",
		"linux/arm64",
		"darwin/amd64",
		"darwin/arm64",
		"windows/amd64",
		"windows/arm64",
	}
	runtimeTarget := runtime.GOOS + "/" + runtime.GOARCH
	if !slices.Contains(testedTargets, runtimeTarget) {
		t.Fatalf("runtime target %s is not in replay determinism matrix %v", runtimeTarget, testedTargets)
	}

	got := buildReplayDeterminismSnapshot(t)
	gotDigest := mustSnapshotDigest(t, got)

	expected := replayDeterminismSnapshot{
		ScenarioSetSHA256: "03cb327218205a4b4e023bc6d96f6b70c1b24253524511577cb7e8a06598fca4",
		ModelHash:         "7682eb3fee3c9202b6f39928d2894c587da2430f2439c5125e3bff779f205c51",
		ToolHash:          "161b877fb15c2747406b214521070da52ed4ba2909b51d3a1a42b07440f46f9d",
		EntryPaths: []string{
			"models/7682eb3fee3c9202b6f39928d2894c587da2430f2439c5125e3bff779f205c51.json",
			"tools/161b877fb15c2747406b214521070da52ed4ba2909b51d3a1a42b07440f46f9d.json",
		},
		EntriesSHA256: "97cfe9c4ceb1b71c14c0924a090f1221e4feb5f650ab4c2b79225d27237eab3e",
		IndexSHA256:   "97cfe9c4ceb1b71c14c0924a090f1221e4feb5f650ab4c2b79225d27237eab3e",
	}
	const expectedDigest = "025cd774fb211c0611512568831c5800b9aef504d1e27b66d208131e208e5990"

	if !reflect.DeepEqual(got, expected) || gotDigest != expectedDigest {
		t.Fatalf(
			"replay determinism snapshot mismatch on %s:\n got=%+v\n got_digest=%s\n want=%+v\n want_digest=%s",
			runtimeTarget,
			got,
			gotDigest,
			expected,
			expectedDigest,
		)
	}
}

func buildReplayDeterminismSnapshot(t *testing.T) replayDeterminismSnapshot {
	t.Helper()

	store := NewStore(t.TempDir())
	suite := "snapshot_suite"
	scenarioDigest := ScenarioSetDigest([]string{"refund", "checkout", "checkout"})

	temp := 0.25
	maxTokens := 64
	topP := 0.9
	modelRequest := &providers.CanonicalRequest{
		GauntletCanonicalVersion: 1,
		ProviderFamily:           "openai_compatible",
		Model:                    "gpt-4o-mini",
		System:                   "You are deterministic.",
		Messages: []providers.CanonicalMessage{
			{Role: "user", Content: "Summarize order status for ord-1001."},
		},
		Sampling: providers.CanonicalSampling{
			Temperature: &temp,
			MaxTokens:   &maxTokens,
			TopP:        &topP,
			Stop:        []string{"\n\n"},
		},
		Tools: []providers.CanonicalTool{
			{
				Name:        "order_lookup",
				Description: "Look up an order.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"order_ref": map[string]interface{}{"type": "string"},
					},
				},
			},
		},
		Extra: map[string]interface{}{
			"trace_id":    "strip-me",
			"sdk_feature": "preserve-me",
		},
	}
	canonicalModelRequest, err := CanonicalizeRequest(modelRequest)
	if err != nil {
		t.Fatalf("CanonicalizeRequest: %v", err)
	}
	modelHash := HashCanonical(canonicalModelRequest)
	if err := store.PutModelFixture(&ModelFixture{
		FixtureID:         modelHash,
		HashVersion:       1,
		CanonicalHash:     modelHash,
		ProviderFamily:    "openai_compatible",
		Model:             "gpt-4o-mini",
		CanonicalRequest:  canonicalModelRequest,
		Response:          json.RawMessage(`{"choices":[{"message":{"role":"assistant","content":"Order is processing."}}]}`),
		RecordedAt:        time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		RecordedBy:        "snapshot-recorder",
		Suite:             suite,
		ScenarioSetSHA256: scenarioDigest,
	}); err != nil {
		t.Fatalf("PutModelFixture: %v", err)
	}

	toolArgs := map[string]interface{}{
		"order_ref":        "ord-1001",
		"include_payments": true,
	}
	canonicalToolCall, err := CanonicalizeToolCall("order_lookup", toolArgs)
	if err != nil {
		t.Fatalf("CanonicalizeToolCall: %v", err)
	}
	toolHash := HashCanonical(canonicalToolCall)
	toolArgsJSON, err := json.Marshal(toolArgs)
	if err != nil {
		t.Fatalf("marshal tool args: %v", err)
	}
	if err := store.PutToolFixture(&ToolFixture{
		FixtureID:         toolHash,
		HashVersion:       1,
		CanonicalHash:     toolHash,
		ToolName:          "order_lookup",
		ArgsHash:          toolHash,
		Args:              toolArgsJSON,
		Response:          json.RawMessage(`{"order_ref":"ord-1001","status":"processing"}`),
		RecordedAt:        time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC),
		Suite:             suite,
		ScenarioSetSHA256: scenarioDigest,
	}); err != nil {
		t.Fatalf("PutToolFixture: %v", err)
	}

	lock, _, err := WriteReplayLockfile(store, suite, scenarioDigest, "", time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("WriteReplayLockfile: %v", err)
	}

	entryPaths := make([]string, 0, len(lock.Entries))
	for _, entry := range lock.Entries {
		entryPaths = append(entryPaths, entry.Path)
	}
	entriesJSON, err := json.Marshal(lock.Entries)
	if err != nil {
		t.Fatalf("marshal lock entries: %v", err)
	}

	return replayDeterminismSnapshot{
		ScenarioSetSHA256: scenarioDigest,
		ModelHash:         modelHash,
		ToolHash:          toolHash,
		EntryPaths:        entryPaths,
		EntriesSHA256:     Hash(entriesJSON),
		IndexSHA256:       lock.IndexSHA256,
	}
}

func mustSnapshotDigest(t *testing.T, snapshot replayDeterminismSnapshot) string {
	t.Helper()
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	return Hash(data)
}
