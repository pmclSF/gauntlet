package fixture

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pmclSF/gauntlet/internal/proxy/providers"
)

func TestSignFixtures_SignsModelAndToolFixturesInPlace(t *testing.T) {
	root := t.TempDir()
	store := NewStore(filepath.Join(root, "fixtures"))
	if err := os.MkdirAll(filepath.Join(store.BaseDir, "models"), 0o755); err != nil {
		t.Fatalf("mkdir models: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(store.BaseDir, "tools"), 0o755); err != nil {
		t.Fatalf("mkdir tools: %v", err)
	}

	cr := &providers.CanonicalRequest{
		GauntletCanonicalVersion: 1,
		ProviderFamily:           "openai_compatible",
		Model:                    "gpt-4o-mini",
		Messages:                 []providers.CanonicalMessage{{Role: "user", Content: "hello"}},
		Sampling:                 providers.CanonicalSampling{},
	}
	canonical, err := CanonicalizeRequest(cr)
	if err != nil {
		t.Fatalf("canonicalize request: %v", err)
	}
	modelHash := HashCanonical(canonical)
	model := &ModelFixture{
		FixtureID:        modelHash,
		HashVersion:      1,
		CanonicalHash:    modelHash,
		ProviderFamily:   "openai_compatible",
		Model:            "gpt-4o-mini",
		CanonicalRequest: canonical,
		Response:         json.RawMessage(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`),
		RecordedAt:       time.Now().UTC(),
		RecordedBy:       "test",
		Provenance:       BuildProvenance(nil, "test"),
	}
	modelPath := filepath.Join(store.BaseDir, "models", modelHash+".json")
	modelBytes, err := json.MarshalIndent(model, "", "  ")
	if err != nil {
		t.Fatalf("marshal model: %v", err)
	}
	if err := os.WriteFile(modelPath, modelBytes, 0o644); err != nil {
		t.Fatalf("write model fixture: %v", err)
	}

	args := map[string]interface{}{"order_id": "ord-001"}
	toolCanonical, err := CanonicalizeToolCall("order_lookup", args)
	if err != nil {
		t.Fatalf("canonicalize tool: %v", err)
	}
	toolHash := HashCanonical(toolCanonical)
	tool := &ToolFixture{
		FixtureID:     toolHash,
		HashVersion:   1,
		CanonicalHash: toolHash,
		ToolName:      "order_lookup",
		ArgsHash:      toolHash,
		Args:          json.RawMessage(`{"order_id":"ord-001"}`),
		Response:      json.RawMessage(`{"status":"ok"}`),
		RecordedAt:    time.Now().UTC(),
		Provenance:    BuildProvenance(nil, "test"),
	}
	toolPath := filepath.Join(store.BaseDir, "tools", toolHash+".json")
	toolBytes, err := json.MarshalIndent(tool, "", "  ")
	if err != nil {
		t.Fatalf("marshal tool: %v", err)
	}
	if err := os.WriteFile(toolPath, toolBytes, 0o644); err != nil {
		t.Fatalf("write tool fixture: %v", err)
	}

	signingKeyPath := filepath.Join(root, ".gauntlet", "fixture-signing-key.pem")
	modelsSigned, toolsSigned, err := SignFixtures(store, signingKeyPath)
	if err != nil {
		t.Fatalf("SignFixtures: %v", err)
	}
	if modelsSigned != 1 {
		t.Fatalf("modelsSigned = %d, want 1", modelsSigned)
	}
	if toolsSigned != 1 {
		t.Fatalf("toolsSigned = %d, want 1", toolsSigned)
	}

	verify := NewStore(store.BaseDir)
	if err := verify.ConfigureFixtureTrust(FixtureTrustOptions{
		RequireSignatures:     true,
		TrustedPublicKeyPaths: []string{signingKeyPath + ".pub.pem"},
	}); err != nil {
		t.Fatalf("ConfigureFixtureTrust: %v", err)
	}
	gotModel, err := verify.GetModelFixture(modelHash)
	if err != nil {
		t.Fatalf("GetModelFixture: %v", err)
	}
	if gotModel == nil || gotModel.Signature == nil {
		t.Fatal("expected signed model fixture")
	}
	gotTool, err := verify.GetToolFixture("order_lookup", toolHash)
	if err != nil {
		t.Fatalf("GetToolFixture: %v", err)
	}
	if gotTool == nil || gotTool.Signature == nil {
		t.Fatal("expected signed tool fixture")
	}
}
