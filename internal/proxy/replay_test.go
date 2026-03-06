package proxy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pmclSF/gauntlet/internal/fixture"
	"github.com/pmclSF/gauntlet/internal/proxy/providers"
)

// stubNormalizer is a minimal ProviderNormalizer for replay tests.
type stubNormalizer struct{}

func (s *stubNormalizer) Family() string { return "test" }
func (s *stubNormalizer) Detect(hostname, path string, body []byte) bool {
	return true
}
func (s *stubNormalizer) Normalize(hostname, path string, headers map[string]string, body []byte) (*providers.CanonicalRequest, error) {
	return &providers.CanonicalRequest{ProviderFamily: "test", Model: "test-model"}, nil
}
func (s *stubNormalizer) NormalizeResponseForFixture(resp []byte) ([]byte, error) {
	return resp, nil
}
func (s *stubNormalizer) ExtractUsage(resp []byte) (int, int) { return 0, 0 }
func (s *stubNormalizer) DenormalizeResponse(resp []byte, _ string) ([]byte, error) {
	return resp, nil
}

func replayTestCanonicalRequest() *providers.CanonicalRequest {
	return &providers.CanonicalRequest{
		ProviderFamily: "test",
		Model:          "test-model",
		Messages:       []providers.CanonicalMessage{{Role: "user", Content: "hello"}},
	}
}

func setupReplayFixture(t *testing.T, responseCode int, response json.RawMessage) (*fixture.Store, []byte, string) {
	t.Helper()
	cr := replayTestCanonicalRequest()
	canonicalBytes, err := fixture.CanonicalizeRequest(cr)
	if err != nil {
		t.Fatal(err)
	}
	hash := fixture.HashCanonical(canonicalBytes)

	dir := t.TempDir()
	store := fixture.NewStore(dir)
	f := &fixture.ModelFixture{
		FixtureID:        hash,
		HashVersion:      1,
		CanonicalHash:    hash,
		ProviderFamily:   "test",
		Model:            "test-model",
		CanonicalRequest: json.RawMessage(canonicalBytes),
		Response:         response,
		ResponseCode:     responseCode,
		RecordedAt:       time.Now(),
		RecordedBy:       "test",
	}
	modelsDir := filepath.Join(dir, "models")
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modelsDir, hash+".json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return store, canonicalBytes, hash
}

func TestHandleRecorded_ReplayStatusCodes(t *testing.T) {
	tests := []struct {
		name         string
		responseCode int
		wantStatus   int
	}{
		{"200 OK", 200, 200},
		{"400 Bad Request", 400, 400},
		{"429 Too Many Requests", 429, 429},
		{"500 Internal Server Error", 500, 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, canonicalBytes, hash := setupReplayFixture(t, tt.responseCode, json.RawMessage(`{"ok":true}`))

			p := &Proxy{Mode: ModeRecorded, Store: store}
			norm := &stubNormalizer{}
			canonical := replayTestCanonicalRequest()

			_, status, err := p.handleRecorded(norm, canonical, canonicalBytes, hash, time.Now())
			if err != nil {
				t.Fatalf("handleRecorded failed: %v", err)
			}
			if status != tt.wantStatus {
				t.Errorf("status = %d, want %d", status, tt.wantStatus)
			}
		})
	}
}

func TestHandleRecorded_BackwardCompat_NoResponseCode(t *testing.T) {
	// Fixtures recorded before ResponseCode was added have response_code=0 (zero value).
	// These should default to 200.
	store, canonicalBytes, hash := setupReplayFixture(t, 0, json.RawMessage(`{"ok":true}`))

	p := &Proxy{Mode: ModeRecorded, Store: store}
	norm := &stubNormalizer{}
	canonical := replayTestCanonicalRequest()

	_, status, err := p.handleRecorded(norm, canonical, canonicalBytes, hash, time.Now())
	if err != nil {
		t.Fatalf("handleRecorded failed: %v", err)
	}
	if status != 200 {
		t.Errorf("status = %d, want 200 (backward compat default)", status)
	}
}

func TestHandleRecorded_FixtureMiss(t *testing.T) {
	dir := t.TempDir()
	store := fixture.NewStore(dir)
	// Create models dir but no fixture file
	if err := os.MkdirAll(filepath.Join(dir, "models"), 0o755); err != nil {
		t.Fatal(err)
	}

	p := &Proxy{Mode: ModeRecorded, Store: store}
	norm := &stubNormalizer{}
	canonical := &providers.CanonicalRequest{ProviderFamily: "test", Model: "test-model"}
	canonicalBytes := []byte(`{"model":"test-model","messages":[]}`)

	_, _, err := p.handleRecorded(norm, canonical, canonicalBytes, "nonexistent-hash", time.Now())
	if err == nil {
		t.Fatal("expected fixture miss error, got nil")
	}
	if _, ok := err.(*fixture.ErrFixtureMiss); !ok {
		t.Errorf("expected *fixture.ErrFixtureMiss, got %T: %v", err, err)
	}
}

func TestCanonicalEquivalentIgnoringModel(t *testing.T) {
	left := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`)
	right := []byte(`{"model":"gpt-4-turbo","messages":[{"role":"user","content":"hi"}]}`)

	match, err := canonicalEquivalentIgnoringModel(left, right)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !match {
		t.Error("expected match when only model differs")
	}

	different := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"bye"}]}`)
	match, err = canonicalEquivalentIgnoringModel(left, different)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if match {
		t.Error("expected no match when messages differ")
	}
}
