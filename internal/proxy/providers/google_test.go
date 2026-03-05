package providers_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/pmclSF/gauntlet/internal/fixture"
	"github.com/pmclSF/gauntlet/internal/proxy/providers"
)

func TestGeminiStreamingRoundTripStoresAndReplaysMergedFixture(t *testing.T) {
	store := fixture.NewStore(t.TempDir())
	normalizer := &providers.GoogleNormalizer{}

	requestBody := []byte(`{
		"contents":[{"role":"USER","parts":[{"text":"say hello"}]}]
	}`)
	canonicalReq, err := normalizer.Normalize(
		"generativelanguage.googleapis.com",
		"/v1beta/models/gemini-2.0-flash:streamGenerateContent",
		nil,
		requestBody,
	)
	if err != nil {
		t.Fatalf("Normalize request: %v", err)
	}
	canonicalBytes, err := fixture.CanonicalizeRequest(canonicalReq)
	if err != nil {
		t.Fatalf("CanonicalizeRequest: %v", err)
	}
	hash := fixture.HashCanonical(canonicalBytes)

	streamingResponse := []byte(`{"candidates":[{"content":{"parts":[{"text":"hello "}]}}]}
{"candidates":[{"content":{"parts":[{"text":"world"}]}}]}
`)
	normalizedResponse, err := normalizer.NormalizeResponseForFixture(streamingResponse)
	if err != nil {
		t.Fatalf("NormalizeResponseForFixture: %v", err)
	}

	if err := store.PutModelFixture(&fixture.ModelFixture{
		FixtureID:        hash,
		HashVersion:      1,
		CanonicalHash:    hash,
		ProviderFamily:   "google",
		Model:            "gemini-2.0-flash",
		CanonicalRequest: canonicalBytes,
		Response:         normalizedResponse,
		RecordedAt:       time.Now().UTC(),
		RecordedBy:       "test",
	}); err != nil {
		t.Fatalf("PutModelFixture: %v", err)
	}

	replay := &fixture.ModelReplay{Store: store, Suite: "smoke"}
	got, err := replay.Replay(canonicalReq)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}

	var replayResponse map[string]interface{}
	if err := json.Unmarshal(got.Response, &replayResponse); err != nil {
		t.Fatalf("fixture replay response should be JSON object: %v", err)
	}
	candidates, ok := replayResponse["candidates"].([]interface{})
	if !ok || len(candidates) == 0 {
		t.Fatalf("expected non-streaming candidates array in replay response: %#v", replayResponse)
	}
	firstCandidate, ok := candidates[0].(map[string]interface{})
	if !ok {
		t.Fatalf("candidate[0] has unexpected type: %#v", candidates[0])
	}
	content, ok := firstCandidate["content"].(map[string]interface{})
	if !ok {
		t.Fatalf("candidate content missing: %#v", firstCandidate)
	}
	parts, ok := content["parts"].([]interface{})
	if !ok || len(parts) == 0 {
		t.Fatalf("candidate parts missing: %#v", content)
	}
	firstPart, ok := parts[0].(map[string]interface{})
	if !ok {
		t.Fatalf("part[0] has unexpected type: %#v", parts[0])
	}
	text, _ := firstPart["text"].(string)
	if text != "hello world" {
		t.Fatalf("merged replay text = %q, want %q", text, "hello world")
	}
}
