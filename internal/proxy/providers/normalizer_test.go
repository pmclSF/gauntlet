package providers

import (
	"encoding/json"
	"testing"
)

func TestDetectOpenAI(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`)
	normalizers := AllNormalizers()

	n := Detect("api.openai.com", "/v1/chat/completions", body, normalizers)
	if n.Family() != "openai_compatible" {
		t.Errorf("expected openai_compatible, got %s", n.Family())
	}
}

func TestDetectAnthropic(t *testing.T) {
	body := []byte(`{"model":"claude-3-opus-20240229","messages":[{"role":"user","content":"hello"}]}`)
	normalizers := AllNormalizers()

	n := Detect("api.anthropic.com", "/v1/messages", body, normalizers)
	if n.Family() != "anthropic" {
		t.Errorf("expected anthropic, got %s", n.Family())
	}
}

func TestDetectGoogle(t *testing.T) {
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	normalizers := AllNormalizers()

	n := Detect("generativelanguage.googleapis.com", "/v1/models/gemini-pro:generateContent", body, normalizers)
	if n.Family() != "google" {
		t.Errorf("expected google, got %s", n.Family())
	}
}

func TestDetectBedrock(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"text":"hello"}]}]}`)
	normalizers := AllNormalizers()

	n := Detect("bedrock-runtime.us-east-1.amazonaws.com", "/model/anthropic.claude-3-sonnet/converse", body, normalizers)
	if n.Family() != "bedrock_converse" {
		t.Errorf("expected bedrock_converse, got %s", n.Family())
	}
}

func TestDetectCohere(t *testing.T) {
	body := []byte(`{"message":"hello","chat_history":[]}`)
	normalizers := AllNormalizers()

	n := Detect("api.cohere.com", "/v1/chat", body, normalizers)
	if n.Family() != "cohere" {
		t.Errorf("expected cohere, got %s", n.Family())
	}
}

func TestOpenAINormalize(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "system", "content": "You are helpful"},
			{"role": "user", "content": "hello"}
		],
		"temperature": 0,
		"max_tokens": 100,
		"tools": [
			{"type": "function", "function": {"name": "b_tool", "description": "B", "parameters": {}}},
			{"type": "function", "function": {"name": "a_tool", "description": "A", "parameters": {}}}
		]
	}`)

	n := &OpenAICompatibleNormalizer{}
	canonical, err := n.Normalize("api.openai.com", "/v1/chat/completions", nil, body)
	if err != nil {
		t.Fatal(err)
	}

	if canonical.Model != "gpt-4o" {
		t.Errorf("expected model gpt-4o, got %s", canonical.Model)
	}
	if canonical.System != "You are helpful" {
		t.Errorf("expected system prompt, got %s", canonical.System)
	}
	if len(canonical.Messages) != 1 {
		t.Errorf("expected 1 user message (system extracted), got %d", len(canonical.Messages))
	}
	if canonical.Sampling.Temperature == nil || *canonical.Sampling.Temperature != 0 {
		t.Error("expected temperature 0")
	}

	// Tools should be sorted by name
	if len(canonical.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(canonical.Tools))
	}
	if canonical.Tools[0].Name != "a_tool" {
		t.Errorf("expected tools sorted by name, got %s first", canonical.Tools[0].Name)
	}
}

func TestCanonicalRequestJSON(t *testing.T) {
	canonical := &CanonicalRequest{
		GauntletCanonicalVersion: 1,
		ProviderFamily:           "openai_compatible",
		Model:                    "gpt-4o",
		System:                   "test",
		Messages:                 []CanonicalMessage{{Role: "user", Content: "hello"}},
		Tools:                    nil,
		Sampling:                 CanonicalSampling{Temperature: floatPtr(0), MaxTokens: intPtr(100)},
	}

	data, err := json.Marshal(canonical)
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}

	if parsed["provider_family"] != "openai_compatible" {
		t.Error("expected provider_family in JSON output")
	}
}

func floatPtr(f float64) *float64 { return &f }
func intPtr(i int) *int           { return &i }
