package providers

import (
	"encoding/json"
	"testing"
)

func TestAnthropicNormalizer_CoversNormalizeAndPassthroughs(t *testing.T) {
	n := &AnthropicNormalizer{}
	body := []byte(`{
		"model":"claude-3-5-sonnet",
		"system":[{"type":"text","text":"policy A"},{"type":"text","text":"policy B"}],
		"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}],
		"tools":[
		  {"name":"z_tool","description":"z","input_schema":{"type":"object"}},
		  {"name":"a_tool","description":"a","input_schema":{"type":"object"}}
		],
		"temperature":0.2,
		"max_tokens":256,
		"top_p":0.9,
		"stop_sequences":["DONE"],
		"request_id":"ignored"
	}`)

	if !n.Detect("api.anthropic.com", "/v1/messages", body) {
		t.Fatal("expected detect to match api.anthropic.com")
	}
	cr, err := n.Normalize("api.anthropic.com", "/v1/messages", nil, body)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if cr.Model != "claude-3-5-sonnet" {
		t.Fatalf("model = %q", cr.Model)
	}
	if cr.System != "policy A\npolicy B" {
		t.Fatalf("system = %q", cr.System)
	}
	if len(cr.Tools) != 2 || cr.Tools[0].Name != "a_tool" || cr.Tools[1].Name != "z_tool" {
		t.Fatalf("tools not stably sorted: %+v", cr.Tools)
	}
	rawResp := []byte(`{"id":"msg_1","content":[{"type":"text","text":"ok"}]}`)
	denorm, err := n.DenormalizeResponse(rawResp, n.Family())
	if err != nil {
		t.Fatalf("DenormalizeResponse: %v", err)
	}
	if string(denorm) != string(rawResp) {
		t.Fatalf("denormalized response changed unexpectedly: %s", string(denorm))
	}
	fixtureResp, err := n.NormalizeResponseForFixture(rawResp)
	if err != nil {
		t.Fatalf("NormalizeResponseForFixture: %v", err)
	}
	if string(fixtureResp) != string(rawResp) {
		t.Fatalf("fixture response changed unexpectedly: %s", string(fixtureResp))
	}
}

func TestBedrockNormalizer_CoversNormalizeAndPassthroughs(t *testing.T) {
	n := &BedrockNormalizer{}
	body := []byte(`{
		"system":[{"text":"safe mode"}],
		"messages":[{"role":"user","content":[{"text":"hello "} , {"text":"world"}]}],
		"toolConfig":{"tools":[{"toolSpec":{"name":"lookup","description":"desc","inputSchema":{"json":{"type":"object"}}}}]},
		"inferenceConfig":{"temperature":0.1,"maxTokens":64,"topP":0.8,"stopSequences":["END"]}
	}`)
	path := "/model/anthropic.claude-3-sonnet/converse"
	if !n.Detect("bedrock-runtime.us-east-1.amazonaws.com", path, body) {
		t.Fatal("expected bedrock detect")
	}
	cr, err := n.Normalize("bedrock-runtime.us-east-1.amazonaws.com", path, nil, body)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if cr.Model != "anthropic.claude-3-sonnet" {
		t.Fatalf("model = %q", cr.Model)
	}
	if len(cr.Messages) != 1 || cr.Messages[0].Content != "hello world" {
		t.Fatalf("messages = %+v", cr.Messages)
	}
	if len(cr.Tools) != 1 || cr.Tools[0].Name != "lookup" {
		t.Fatalf("tools = %+v", cr.Tools)
	}

	resp := []byte(`{"usage":{"inputTokens":12,"outputTokens":4}}`)
	prompt, completion := n.ExtractUsage(resp)
	if prompt != 12 || completion != 4 {
		t.Fatalf("ExtractUsage = (%d,%d), want (12,4)", prompt, completion)
	}
	if out, err := n.NormalizeResponseForFixture(resp); err != nil || string(out) != string(resp) {
		t.Fatalf("NormalizeResponseForFixture = %q, err=%v", string(out), err)
	}
}

func TestCohereAndUnknownNormalizers_CoverAllMethods(t *testing.T) {
	cohere := &CohereNormalizer{}
	body := []byte(`{
		"model":"command-r",
		"preamble":"system text",
		"chat_history":[{"role":"USER","message":"hi"}],
		"message":"latest",
		"tools":[{"name":"lookup","description":"desc","parameter_definitions":{"id":{"type":"string"}}}],
		"temperature":0.3,
		"max_tokens":128,
		"p":0.7,
		"stop_sequences":["END"]
	}`)
	if !cohere.Detect("api.cohere.com", "/v1/chat", body) {
		t.Fatal("expected cohere detect")
	}
	cr, err := cohere.Normalize("api.cohere.com", "/v1/chat", nil, body)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if cr.Model != "command-r" || cr.System != "system text" {
		t.Fatalf("canonical model/system unexpected: %+v", cr)
	}
	if len(cr.Messages) != 2 {
		t.Fatalf("messages len = %d, want 2", len(cr.Messages))
	}
	resp := []byte(`{"meta":{"billed_units":{"input_tokens":9,"output_tokens":3}}}`)
	prompt, completion := cohere.ExtractUsage(resp)
	if prompt != 9 || completion != 3 {
		t.Fatalf("cohere ExtractUsage = (%d,%d), want (9,3)", prompt, completion)
	}
	if out, err := cohere.DenormalizeResponse(resp, cohere.Family()); err != nil || string(out) != string(resp) {
		t.Fatalf("cohere DenormalizeResponse = %q, err=%v", string(out), err)
	}

	unknown := &UnknownNormalizer{}
	if !unknown.Detect("example.com", "/v1/anything", []byte(`{}`)) {
		t.Fatal("unknown normalizer should always detect")
	}
	uc, err := unknown.Normalize("example.com", "/v1/anything", nil, []byte(`{"model":"m","foo":"bar"}`))
	if err != nil {
		t.Fatalf("unknown Normalize: %v", err)
	}
	if uc.ProviderFamily != "unknown" || uc.Model != "m" {
		t.Fatalf("unexpected unknown canonical request: %+v", uc)
	}
	if _, ok := uc.Extra["foo"]; !ok {
		t.Fatalf("expected unknown extra fields to be preserved: %+v", uc.Extra)
	}
	if _, err := unknown.Normalize("example.com", "/v1/anything", nil, []byte(`not-json`)); err == nil {
		t.Fatal("expected unknown Normalize to fail on invalid json")
	}
}

func TestNormalizerForFamilyAndLocalhostHelpers(t *testing.T) {
	tests := map[string]string{
		"openai_compatible": "openai_compatible",
		"anthropic":         "anthropic",
		"google":            "google",
		"bedrock_converse":  "bedrock_converse",
		"cohere":            "cohere",
		"unknown-family":    "unknown",
	}
	for family, want := range tests {
		if got := NormalizerForFamily(family).Family(); got != want {
			t.Fatalf("NormalizerForFamily(%q) = %q, want %q", family, got, want)
		}
	}

	if !isLocalhost("127.0.0.1:8080") || !isLocalhost("[::1]:8080") || !isLocalhost("localhost") {
		t.Fatal("expected localhost variants to be detected")
	}
	if isLocalhost("api.openai.com") {
		t.Fatal("unexpected localhost match for external host")
	}

	if !isLocalModelPath("/api/chat") || !isLocalModelPath("/v1/completions") {
		t.Fatal("expected known local model paths to match")
	}
	if isLocalModelPath("/v1/messages") {
		t.Fatal("unexpected local model path match")
	}

	// Ensure unknown fallback in Detect remains stable.
	n := Detect("example.com", "/unrecognized", []byte(`{"foo":"bar"}`), []ProviderNormalizer{})
	if n.Family() != "unknown" {
		t.Fatalf("Detect fallback family = %q, want unknown", n.Family())
	}
}

func TestUnknownNormalizerRoundTripJSON(t *testing.T) {
	n := &UnknownNormalizer{}
	cr, err := n.Normalize("x", "/y", nil, []byte(`{"model":"m","n":1}`))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	data, err := json.Marshal(cr)
	if err != nil {
		t.Fatalf("marshal canonical request: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty canonical json")
	}
}
