package fixture

import (
	"testing"

	"github.com/pmclSF/gauntlet/internal/proxy/providers"
)

func TestCanonicalizeRequest(t *testing.T) {
	temp := 0.0
	maxTok := 100
	cr := &providers.CanonicalRequest{
		GauntletCanonicalVersion: 1,
		ProviderFamily:           "openai_compatible",
		Model:                    "gpt-4o",
		System:                   "You are helpful",
		Messages: []providers.CanonicalMessage{
			{Role: "user", Content: "hello"},
		},
		Sampling: providers.CanonicalSampling{
			Temperature: &temp,
			MaxTokens:   &maxTok,
		},
	}

	canonical, err := CanonicalizeRequest(cr)
	if err != nil {
		t.Fatal(err)
	}

	if len(canonical) == 0 {
		t.Error("expected non-empty canonical JSON")
	}

	// Same input should produce same output (deterministic)
	canonical2, err := CanonicalizeRequest(cr)
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical) != string(canonical2) {
		t.Error("canonicalization should be deterministic")
	}
}

func TestCanonicalizeToolCall(t *testing.T) {
	args := map[string]interface{}{
		"order_id":   "ord-001",
		"created_at": "2025-01-01T00:00:00Z",
		"request_id": "req-123",
		"extra":      "data",
	}

	canonical, err := CanonicalizeToolCall("order_lookup", args)
	if err != nil {
		t.Fatal(err)
	}

	if len(canonical) == 0 {
		t.Error("expected non-empty canonical JSON")
	}

	// Deterministic
	canonical2, err := CanonicalizeToolCall("order_lookup", args)
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical) != string(canonical2) {
		t.Error("tool call canonicalization should be deterministic")
	}
	s := string(canonical)
	if contains(s, "req-123") {
		t.Error("tool canonicalization should strip request_id")
	}
	if !contains(s, "ord-001") {
		t.Error("tool canonicalization should preserve order_id")
	}
	if !contains(s, "2025-01-01T00:00:00Z") {
		t.Error("tool canonicalization should preserve created_at")
	}
}

func TestStripDenylistHeaders(t *testing.T) {
	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer sk-1234",
		"X-Request-ID":  "req-abc",
		"User-Agent":    "openai-python/1.0",
		"Date":          "2025-01-15",
		"X-Custom":      "preserved",
	}

	cleaned := StripDenylistHeaders(headers)

	if _, ok := cleaned["Authorization"]; ok {
		t.Error("Authorization should be stripped")
	}
	if _, ok := cleaned["X-Request-ID"]; ok {
		t.Error("X-Request-ID should be stripped")
	}
	if _, ok := cleaned["User-Agent"]; ok {
		t.Error("User-Agent should be stripped")
	}
	if _, ok := cleaned["Date"]; ok {
		t.Error("Date should be stripped")
	}
	if v, ok := cleaned["Content-Type"]; !ok || v != "application/json" {
		t.Error("Content-Type should be preserved")
	}
	if v, ok := cleaned["X-Custom"]; !ok || v != "preserved" {
		t.Error("X-Custom should be preserved")
	}
}

func TestShouldStripField(t *testing.T) {
	// Exact match denylist
	if !shouldStripField("request_id") {
		t.Error("request_id should be stripped")
	}
	if !shouldStripField("user") {
		t.Error("user should be stripped")
	}
	if !shouldStripField("session_id") {
		t.Error("session_id should be stripped")
	}
	if !shouldStripField("stream") {
		t.Error("stream should be stripped")
	}

	// Suffix denylist
	if !shouldStripField("created_at") {
		t.Error("created_at should be stripped (suffix _at)")
	}
	if !shouldStripField("updated_ts") {
		t.Error("updated_ts should be stripped (suffix _ts)")
	}

	// Prefix denylist
	if !shouldStripField("metadata.key") {
		t.Error("metadata.key should be stripped (prefix metadata)")
	}

	// Unknown fields preserved
	if shouldStripField("model") {
		t.Error("model should NOT be stripped")
	}
	if shouldStripField("messages") {
		t.Error("messages should NOT be stripped")
	}
	if shouldStripField("new_sdk_field") {
		t.Error("unknown field should NOT be stripped (denylist, not allowlist)")
	}
}

func TestShouldStripToolField(t *testing.T) {
	if !shouldStripToolField("request_id") {
		t.Error("request_id should be stripped for tool args")
	}
	if !shouldStripToolField("timestamp") {
		t.Error("timestamp should be stripped for tool args")
	}
	if !shouldStripToolField("trace_id") {
		t.Error("trace_id should be stripped for tool args")
	}
	if !shouldStripToolField("session_id") {
		t.Error("session_id should be stripped for tool args")
	}
	if !shouldStripToolField("metadata.trace") {
		t.Error("metadata.* should be stripped for tool args")
	}
	if !shouldStripToolField("extra_headers.auth") {
		t.Error("extra_headers.* should be stripped for tool args")
	}
	if shouldStripToolField("order_id") {
		t.Error("order_id should NOT be stripped for tool args")
	}
	if shouldStripToolField("created_at") {
		t.Error("created_at should NOT be stripped for tool args")
	}
}

func TestHashCanonical(t *testing.T) {
	data := []byte(`{"model":"gpt-4o","messages":[]}`)
	hash := HashCanonical(data)

	if len(hash) != 64 {
		t.Errorf("expected SHA-256 hex hash (64 chars), got %d chars", len(hash))
	}

	// Same input should produce same hash
	hash2 := HashCanonical(data)
	if hash != hash2 {
		t.Error("same input should produce same hash")
	}

	// Different input should produce different hash
	hash3 := HashCanonical([]byte(`{"model":"gpt-3.5","messages":[]}`))
	if hash == hash3 {
		t.Error("different input should produce different hash")
	}
}

func TestDenylistPreservesUnknownFieldsInExtra(t *testing.T) {
	cr := &providers.CanonicalRequest{
		GauntletCanonicalVersion: 1,
		ProviderFamily:           "openai_compatible",
		Model:                    "gpt-4o",
		System:                   "test",
		Messages:                 []providers.CanonicalMessage{{Role: "user", Content: "hello"}},
		Sampling:                 providers.CanonicalSampling{},
		Extra: map[string]interface{}{
			"new_sdk_field":    "important",
			"custom_parameter": 42,
			"request_id":       "should-be-stripped",
		},
	}

	canonical, err := CanonicalizeRequest(cr)
	if err != nil {
		t.Fatal(err)
	}

	s := string(canonical)
	if !contains(s, "new_sdk_field") {
		t.Error("unknown field 'new_sdk_field' should be preserved in canonical output")
	}
	if !contains(s, "custom_parameter") {
		t.Error("unknown field 'custom_parameter' should be preserved in canonical output")
	}
	if contains(s, "should-be-stripped") {
		t.Error("denylisted field 'request_id' value should not appear in canonical output")
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && len(s) >= len(substr) && stringContains(s, substr)
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
