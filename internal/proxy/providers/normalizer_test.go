package providers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
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

func TestDetectPriority_OpenAICompatibleWinsOnAmbiguousPath(t *testing.T) {
	body := []byte(`{"model":"claude-3-opus-20240229","messages":[{"role":"user","content":"hello"}]}`)
	normalizers := AllNormalizers()

	// Ambiguous path contains both Anthropic and OpenAI-compatible markers.
	n := Detect("gateway.internal", "/v1/messages/chat/completions", body, normalizers)
	if n.Family() != "openai_compatible" {
		t.Errorf("expected openai_compatible, got %s", n.Family())
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

func TestOpenAINormalizeCanonicalConsistency_IgnoresDenylistedFields(t *testing.T) {
	body1 := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role":"user","content":"hello"}],
		"request_id": "abc123",
		"stream": false
	}`)
	body2 := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role":"user","content":"hello"}],
		"request_id": "xyz789",
		"stream": false
	}`)

	n := &OpenAICompatibleNormalizer{}
	c1, err := n.Normalize("api.openai.com", "/v1/chat/completions", nil, body1)
	if err != nil {
		t.Fatalf("normalize body1: %v", err)
	}
	c2, err := n.Normalize("api.openai.com", "/v1/chat/completions", nil, body2)
	if err != nil {
		t.Fatalf("normalize body2: %v", err)
	}

	j1, err := json.Marshal(c1)
	if err != nil {
		t.Fatalf("marshal canonical1: %v", err)
	}
	j2, err := json.Marshal(c2)
	if err != nil {
		t.Fatalf("marshal canonical2: %v", err)
	}

	if string(j1) != string(j2) {
		t.Fatalf("canonical forms differ for request_id-only variation:\n%s\n%s", string(j1), string(j2))
	}
	if _, ok := c1.Extra["request_id"]; ok {
		t.Fatal("request_id should not be preserved in canonical extra fields")
	}
	if _, ok := c1.Extra["stream"]; ok {
		t.Fatal("stream should not be preserved in canonical extra fields")
	}
}

func TestExtractUsageTokensByProvider(t *testing.T) {
	tests := []struct {
		name                 string
		normalizer           ProviderNormalizer
		response             []byte
		wantPromptTokens     int
		wantCompletionTokens int
	}{
		{
			name:                 "openai",
			normalizer:           &OpenAICompatibleNormalizer{},
			response:             []byte(`{"usage":{"prompt_tokens":120,"completion_tokens":30}}`),
			wantPromptTokens:     120,
			wantCompletionTokens: 30,
		},
		{
			name:                 "anthropic",
			normalizer:           &AnthropicNormalizer{},
			response:             []byte(`{"usage":{"input_tokens":90,"output_tokens":25}}`),
			wantPromptTokens:     90,
			wantCompletionTokens: 25,
		},
		{
			name:                 "google",
			normalizer:           &GoogleNormalizer{},
			response:             []byte(`{"usageMetadata":{"promptTokenCount":75,"candidatesTokenCount":15}}`),
			wantPromptTokens:     75,
			wantCompletionTokens: 15,
		},
		{
			name:                 "cohere",
			normalizer:           &CohereNormalizer{},
			response:             []byte(`{"meta":{"billed_units":{"input_tokens":44,"output_tokens":12}}}`),
			wantPromptTokens:     44,
			wantCompletionTokens: 12,
		},
		{
			name:                 "bedrock",
			normalizer:           &BedrockNormalizer{},
			response:             []byte(`{"usage":{"inputTokens":33,"outputTokens":11}}`),
			wantPromptTokens:     33,
			wantCompletionTokens: 11,
		},
		{
			name:                 "unknown",
			normalizer:           &UnknownNormalizer{},
			response:             []byte(`{"usage":{"prompt_tokens":1,"completion_tokens":1}}`),
			wantPromptTokens:     0,
			wantCompletionTokens: 0,
		},
	}

	for _, tt := range tests {
		prompt, completion := tt.normalizer.ExtractUsage(tt.response)
		if prompt != tt.wantPromptTokens || completion != tt.wantCompletionTokens {
			t.Fatalf(
				"%s ExtractUsage() = (%d,%d), want (%d,%d)",
				tt.name,
				prompt,
				completion,
				tt.wantPromptTokens,
				tt.wantCompletionTokens,
			)
		}
	}
}

func TestOpenAINormalizeResponseForFixture_StripsOllamaTimingFields(t *testing.T) {
	n := &OpenAICompatibleNormalizer{}
	raw := []byte(`{
		"model":"llama3.2",
		"created_at":"2026-03-05T00:00:00Z",
		"message":{"role":"assistant","content":"ok"},
		"done_reason":"stop",
		"total_duration":123,
		"load_duration":77,
		"prompt_eval_duration":12,
		"eval_duration":34,
		"prompt_eval_count":11,
		"eval_count":7
	}`)

	normalized, err := n.NormalizeResponseForFixture(raw)
	if err != nil {
		t.Fatalf("NormalizeResponseForFixture: %v", err)
	}
	text := string(normalized)
	for _, key := range []string{
		"created_at",
		"total_duration",
		"load_duration",
		"prompt_eval_duration",
		"eval_duration",
		"prompt_eval_count",
		"eval_count",
	} {
		if strings.Contains(text, key) {
			t.Fatalf("expected %s to be stripped from normalized fixture response: %s", key, text)
		}
	}
	if !strings.Contains(text, `"done_reason":"stop"`) {
		t.Fatalf("expected done_reason to be preserved: %s", text)
	}
}

func TestGoogleNormalizeResponse_StreamingAndNonStreamingEquivalent(t *testing.T) {
	n := &GoogleNormalizer{}
	nonStreaming := []byte(`{
		"candidates":[{"content":{"parts":[{"text":"hello world"}]}}]
	}`)
	streamingNDJSON := []byte(`{"candidates":[{"content":{"parts":[{"text":"hello "}]}}]}
{"candidates":[{"content":{"parts":[{"text":"world"}]}}]}
`)

	nonStreamingNormalized, err := n.NormalizeResponseForFixture(nonStreaming)
	if err != nil {
		t.Fatalf("NormalizeResponseForFixture(non-stream): %v", err)
	}
	streamingNormalized, err := n.NormalizeResponseForFixture(streamingNDJSON)
	if err != nil {
		t.Fatalf("NormalizeResponseForFixture(stream): %v", err)
	}

	var a map[string]interface{}
	if err := json.Unmarshal(nonStreamingNormalized, &a); err != nil {
		t.Fatalf("unmarshal non-stream normalized: %v", err)
	}
	var b map[string]interface{}
	if err := json.Unmarshal(streamingNormalized, &b); err != nil {
		t.Fatalf("unmarshal stream normalized: %v", err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("expected normalized streaming and non-streaming responses to match\nnon-stream=%s\nstream=%s", string(nonStreamingNormalized), string(streamingNormalized))
	}
}

func TestGoogleNormalizeResponse_StripsNonDeterministicFields(t *testing.T) {
	n := &GoogleNormalizer{}
	respA := []byte(`{
		"responseId":"resp-1",
		"createTime":"2026-03-05T00:00:00Z",
		"updateTime":"2026-03-05T00:00:02Z",
		"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":20},
		"candidates":[{"content":{"parts":[{"text":"ok"}]}}]
	}`)
	respB := []byte(`{
		"responseId":"resp-2",
		"createTime":"2026-03-06T00:00:00Z",
		"updateTime":"2026-03-06T00:00:03Z",
		"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":20},
		"candidates":[{"content":{"parts":[{"text":"ok"}]}}]
	}`)

	normA, err := n.NormalizeResponseForFixture(respA)
	if err != nil {
		t.Fatalf("NormalizeResponseForFixture(A): %v", err)
	}
	normB, err := n.NormalizeResponseForFixture(respB)
	if err != nil {
		t.Fatalf("NormalizeResponseForFixture(B): %v", err)
	}

	textA := string(normA)
	if strings.Contains(textA, "responseId") || strings.Contains(textA, "createTime") || strings.Contains(textA, "usageMetadata") {
		t.Fatalf("expected response metadata to be stripped: %s", textA)
	}
	if !strings.Contains(textA, `"prompt_tokens":100`) || !strings.Contains(textA, `"completion_tokens":20`) {
		t.Fatalf("expected extracted token counters to remain available: %s", textA)
	}

	hashA := sha256.Sum256(normA)
	hashB := sha256.Sum256(normB)
	if hex.EncodeToString(hashA[:]) != hex.EncodeToString(hashB[:]) {
		t.Fatalf("expected hashes to match after stripping non-deterministic fields\nA=%s\nB=%s", string(normA), string(normB))
	}
}

func TestRewriteGeminiStreamingPath(t *testing.T) {
	in := "/v1beta/models/gemini-2.0-flash:streamGenerateContent"
	got := RewriteGeminiStreamingPath(in)
	want := "/v1beta/models/gemini-2.0-flash:generateContent"
	if got != want {
		t.Fatalf("RewriteGeminiStreamingPath() = %q, want %q", got, want)
	}
}

func TestGoogleNormalize_MarksOriginalStreamingPath(t *testing.T) {
	n := &GoogleNormalizer{}
	body := []byte(`{"contents":[{"role":"USER","parts":[{"text":"hello"}]}]}`)
	canonical, err := n.Normalize(
		"generativelanguage.googleapis.com",
		"/v1beta/models/gemini-2.0-flash:streamGenerateContent",
		nil,
		body,
	)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if got, ok := canonical.Extra["original_path_was_streaming"].(bool); !ok || !got {
		t.Fatalf("expected original_path_was_streaming=true, got %#v", canonical.Extra["original_path_was_streaming"])
	}
}

func TestOpenAIInlineImagePaddingProducesSameHash(t *testing.T) {
	n := &OpenAICompatibleNormalizer{}
	bodyA := []byte(`{
		"model":"llama3.2",
		"messages":[{"role":"user","content":[{"type":"text","text":"describe this image"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AQI="}}]}]
	}`)
	bodyB := []byte(`{
		"model":"llama3.2",
		"messages":[{"role":"user","content":[{"type":"text","text":"describe this image"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AQI"}}]}]
	}`)

	c1, err := n.Normalize("localhost:11434", "/v1/chat/completions", nil, bodyA)
	if err != nil {
		t.Fatalf("Normalize(A): %v", err)
	}
	c2, err := n.Normalize("localhost:11434", "/v1/chat/completions", nil, bodyB)
	if err != nil {
		t.Fatalf("Normalize(B): %v", err)
	}

	j1, _ := json.Marshal(c1)
	j2, _ := json.Marshal(c2)
	h1 := sha256.Sum256(j1)
	h2 := sha256.Sum256(j2)
	if hex.EncodeToString(h1[:]) != hex.EncodeToString(h2[:]) {
		t.Fatalf("expected canonical hashes to match for padding variants\nA=%s\nB=%s", string(j1), string(j2))
	}
}

func TestOpenAIAndGeminiInlineImageShareImageHash(t *testing.T) {
	openai := &OpenAICompatibleNormalizer{}
	google := &GoogleNormalizer{}
	openaiBody := []byte(`{
		"model":"llama3.2",
		"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AQI="}}]}]
	}`)
	googleBody := []byte(`{
		"contents":[{"role":"USER","parts":[{"inlineData":{"mimeType":"image/png","data":"AQI="}}]}]
	}`)

	openaiCanonical, err := openai.Normalize("localhost:11434", "/v1/chat/completions", nil, openaiBody)
	if err != nil {
		t.Fatalf("openai normalize: %v", err)
	}
	googleCanonical, err := google.Normalize("generativelanguage.googleapis.com", "/v1beta/models/gemini-2.0-flash:generateContent", nil, googleBody)
	if err != nil {
		t.Fatalf("google normalize: %v", err)
	}

	openaiParts := openaiCanonical.Messages[0].Content.([]CanonicalContentPart)
	googleParts := googleCanonical.Messages[0].Content.([]CanonicalContentPart)
	if openaiParts[0].ImageHash != googleParts[0].ImageHash {
		t.Fatalf("image hashes differ: openai=%s google=%s", openaiParts[0].ImageHash, googleParts[0].ImageHash)
	}
}

func TestOllamaImagesArrayNormalizesToImageHash(t *testing.T) {
	n := &OpenAICompatibleNormalizer{}
	body := []byte(`{
		"model":"llama3.2",
		"messages":[{"role":"user","content":"describe","images":["AQI="]}]
	}`)

	canonical, err := n.Normalize("localhost:11434", "/api/chat", nil, body)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	parts := canonical.Messages[0].Content.([]CanonicalContentPart)
	if len(parts) != 2 {
		t.Fatalf("parts len = %d, want 2", len(parts))
	}
	if parts[1].Type != "image" || parts[1].ImageHash == "" {
		t.Fatalf("expected second part to be image hash, got %+v", parts[1])
	}
}

func TestMixedTextAndImagePartsNormalize(t *testing.T) {
	n := &OpenAICompatibleNormalizer{}
	body := []byte(`{
		"model":"llama3.2",
		"messages":[{"role":"user","content":[{"type":"text","text":"describe this image"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AQI="}}]}]
	}`)

	canonical, err := n.Normalize("localhost:11434", "/v1/chat/completions", nil, body)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	parts := canonical.Messages[0].Content.([]CanonicalContentPart)
	if len(parts) != 2 {
		t.Fatalf("parts len = %d, want 2", len(parts))
	}
	if parts[0].Type != "text" || parts[0].Text != "describe this image" {
		t.Fatalf("unexpected first part: %+v", parts[0])
	}
	if parts[1].Type != "image" || parts[1].ImageHash == "" {
		t.Fatalf("unexpected second part: %+v", parts[1])
	}
}

func floatPtr(f float64) *float64 { return &f }
func intPtr(i int) *int           { return &i }
