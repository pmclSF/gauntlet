package fixture

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestScanSensitiveJSON_DetectsBearerToken(t *testing.T) {
	data := []byte(`{"headers":{"Authorization":"Bearer abcdefghijklmnopqrstuvwxyz123456"}}`)
	findings, err := ScanSensitiveJSON(data, "response")
	if err != nil {
		t.Fatalf("ScanSensitiveJSON: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("expected sensitive finding for bearer token")
	}
}

func TestScanSensitiveJSON_DetectsHighEntropyString(t *testing.T) {
	data := []byte(`{"token":"aB3dE5fG7hJ9kL1mN2pQ4rS6tV8wX0yZ"}`)
	findings, err := ScanSensitiveJSON(data, "args")
	if err != nil {
		t.Fatalf("ScanSensitiveJSON: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("expected high-entropy finding")
	}
}

func TestPutToolFixture_BlocksSensitiveDataUnlessOverride(t *testing.T) {
	store := NewStore(t.TempDir())
	toolResp, err := json.Marshal(map[string]interface{}{
		"headers": map[string]interface{}{
			"Authorization": "Bearer abcdefghijklmnopqrstuvwxyz123456",
		},
	})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	toolArgs := []byte(`{"order_id":"ord-1"}`)
	f := &ToolFixture{
		FixtureID:     "abc",
		HashVersion:   1,
		CanonicalHash: "abc",
		ToolName:      "order_lookup",
		ArgsHash:      "abc",
		State:         "nominal",
		Args:          toolArgs,
		Response:      toolResp,
		RecordedAt:    time.Now().UTC(),
	}

	t.Setenv("GAUNTLET_ALLOW_SENSITIVE_FIXTURE", "")
	err = store.PutToolFixture(f)
	if err == nil {
		t.Fatal("expected sensitive fixture write to fail")
	}
	if !strings.Contains(err.Error(), "sensitive data detected in fixture") {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Setenv("GAUNTLET_ALLOW_SENSITIVE_FIXTURE", "1")
	if err := store.PutToolFixture(f); err != nil {
		t.Fatalf("expected override to allow fixture write, got: %v", err)
	}
}
