package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/pmclSF/gauntlet/internal/fixture"
	"github.com/pmclSF/gauntlet/internal/proxy/providers"
)

// handleLive forwards a request to the real upstream endpoint, records the
// response as a fixture, and returns the response to the caller.
func (p *Proxy) handleLive(normalizer providers.ProviderNormalizer, method, hostname, path, rawQuery string, headers map[string]string, body []byte, canonical *providers.CanonicalRequest, canonicalBytes []byte, hash string, start time.Time) ([]byte, int, error) {
	// Normalize streaming requests so recording captures single-response fixtures.
	path, body = stripStreamFlag(path, canonical.ProviderFamily, body)

	respBody, statusCode, err := defaultTransport.Forward(context.Background(), method, hostname, path, rawQuery, headers, body)
	if err != nil {
		return nil, 0, fmt.Errorf("live request failed: %w", err)
	}
	promptTokens, completionTokens := normalizer.ExtractUsage(respBody)
	normalizedResponse, err := normalizer.NormalizeResponseForFixture(respBody)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to normalize live response: %w", err)
	}
	if err := fixture.ValidateModelResponse(canonical.ProviderFamily, normalizedResponse); err != nil {
		return nil, 0, fmt.Errorf("model response schema validation failed: %w", err)
	}

	// Redact before recording
	redactedResp, _ := p.Redactor.RedactJSON(normalizedResponse)

	// Record as fixture
	f := &fixture.ModelFixture{
		FixtureID:         hash,
		HashVersion:       1,
		CanonicalHash:     hash,
		ProviderFamily:    canonical.ProviderFamily,
		Model:             canonical.Model,
		CanonicalRequest:  canonicalBytes,
		Response:          redactedResp,
		ResponseCode:      statusCode,
		RecordedAt:        time.Now(),
		RecordedBy:        "live",
		Provenance:        fixture.BuildProvenance(headers, "proxy_live"),
		Suite:             p.Suite,
		ScenarioSetSHA256: p.ScenarioSetSHA256,
	}
	if err := p.Store.PutModelFixture(f); err != nil {
		log.Printf("WARN: failed to store fixture: %v", err)
	}

	p.recordTrace(TraceEntry{
		Timestamp:        start,
		ProviderFamily:   canonical.ProviderFamily,
		Model:            canonical.Model,
		CanonicalHash:    hash,
		FixtureHit:       false,
		DurationMs:       int(time.Since(start).Milliseconds()),
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
	})

	return normalizedResponse, statusCode, nil
}

// stripStreamFlag normalizes provider streaming requests for fixture recording.
// For OpenAI-compatible APIs, stream=true is removed (or forced false for
// Ollama-native /api endpoints). For Gemini, streaming path suffixes are
// rewritten to non-streaming equivalents.
func stripStreamFlag(path, providerFamily string, body []byte) (string, []byte) {
	if strings.EqualFold(strings.TrimSpace(providerFamily), "google") {
		return providers.RewriteGeminiStreamingPath(path), body
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(body, &parsed); err != nil {
		return path, body
	}
	raw, ok := parsed["stream"]
	if !ok {
		return path, body
	}
	val := strings.TrimSpace(string(raw))
	if val != "true" {
		return path, body
	}
	if strings.HasPrefix(path, "/api/chat") || strings.HasPrefix(path, "/api/generate") {
		parsed["stream"] = json.RawMessage("false")
	} else {
		delete(parsed, "stream")
	}
	// stream_options is only valid when stream=true on OpenAI-compatible APIs.
	// Remove it alongside stream to keep live recording requests valid.
	delete(parsed, "stream_options")
	out, err := json.Marshal(parsed)
	if err != nil {
		return path, body
	}
	return path, out
}
