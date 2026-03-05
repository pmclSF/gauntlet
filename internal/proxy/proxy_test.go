package proxy

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pmclSF/gauntlet/internal/fixture"
	"github.com/pmclSF/gauntlet/internal/proxy/providers"
)

func TestModeConstants(t *testing.T) {
	tests := []struct {
		mode Mode
		want string
	}{
		{ModeRecorded, "recorded"},
		{ModeLive, "live"},
		{ModePassthrough, "passthrough"},
	}
	for _, tt := range tests {
		if string(tt.mode) != tt.want {
			t.Errorf("Mode constant %q: got %q, want %q", tt.want, string(tt.mode), tt.want)
		}
	}
}

func TestNewProxy(t *testing.T) {
	addr := "127.0.0.1:9090"
	mode := ModeRecorded

	p := NewProxy(addr, mode, nil, nil)

	if p.Addr != addr {
		t.Errorf("Addr: got %q, want %q", p.Addr, addr)
	}
	if p.Mode != mode {
		t.Errorf("Mode: got %q, want %q", p.Mode, mode)
	}
	if p.Store != nil {
		t.Error("Store: expected nil for nil input")
	}
	if p.CA != nil {
		t.Error("CA: expected nil for nil input")
	}
	if p.Normalizers == nil {
		t.Error("Normalizers: expected non-nil (AllNormalizers)")
	}
	if p.Redactor == nil {
		t.Error("Redactor: expected non-nil (DefaultRedactor)")
	}
	if p.MaxHeaderBytes <= 0 {
		t.Errorf("MaxHeaderBytes: got %d, want positive default", p.MaxHeaderBytes)
	}
	if p.MaxBodyBytes <= 0 {
		t.Errorf("MaxBodyBytes: got %d, want positive default", p.MaxBodyBytes)
	}
	if p.MaxRequestsPerConn <= 0 {
		t.Errorf("MaxRequestsPerConn: got %d, want positive default", p.MaxRequestsPerConn)
	}
}

func TestTracesEmptyInitially(t *testing.T) {
	p := &Proxy{}
	traces := p.Traces()
	if len(traces) != 0 {
		t.Errorf("Traces(): expected 0 entries, got %d", len(traces))
	}
}

func TestTracesAfterRecording(t *testing.T) {
	p := &Proxy{}

	entry1 := TraceEntry{
		Timestamp:      time.Now(),
		ProviderFamily: "openai",
		Model:          "gpt-4",
		CanonicalHash:  "abc123",
		FixtureHit:     true,
		DurationMs:     42,
	}
	entry2 := TraceEntry{
		Timestamp:      time.Now(),
		ProviderFamily: "anthropic",
		Model:          "claude-3",
		CanonicalHash:  "def456",
		FixtureHit:     false,
		DurationMs:     99,
	}

	p.recordTrace(entry1)
	p.recordTrace(entry2)

	traces := p.Traces()
	if len(traces) != 2 {
		t.Fatalf("Traces(): expected 2 entries, got %d", len(traces))
	}
	if traces[0].ProviderFamily != "openai" {
		t.Errorf("traces[0].ProviderFamily: got %q, want %q", traces[0].ProviderFamily, "openai")
	}
	if traces[1].Model != "claude-3" {
		t.Errorf("traces[1].Model: got %q, want %q", traces[1].Model, "claude-3")
	}
}

func TestTracesReturnsCopy(t *testing.T) {
	p := &Proxy{}
	p.recordTrace(TraceEntry{ProviderFamily: "test"})

	traces := p.Traces()
	traces[0].ProviderFamily = "mutated"

	original := p.Traces()
	if original[0].ProviderFamily != "test" {
		t.Error("Traces() should return a copy; mutation of returned slice affected internal state")
	}
}

func TestEnvVarsWithoutCA(t *testing.T) {
	p := &Proxy{
		Addr: "127.0.0.1:8080",
		CA:   nil,
	}

	vars := p.EnvVars("/tmp/ca.pem")

	expected := []string{
		"HTTP_PROXY=http://127.0.0.1:8080",
		"HTTPS_PROXY=http://127.0.0.1:8080",
		"ALL_PROXY=http://127.0.0.1:8080",
		"http_proxy=http://127.0.0.1:8080",
		"https_proxy=http://127.0.0.1:8080",
		"all_proxy=http://127.0.0.1:8080",
		"GAUNTLET_PROXY_PORT=8080",
		"NO_PROXY=",
		"no_proxy=",
	}

	if len(vars) != len(expected) {
		t.Fatalf("EnvVars(): expected %d entries, got %d: %v", len(expected), len(vars), vars)
	}

	for i, want := range expected {
		if vars[i] != want {
			t.Errorf("EnvVars()[%d]: got %q, want %q", i, vars[i], want)
		}
	}
}

func TestEnvVarsWithCA(t *testing.T) {
	tmpDir := t.TempDir()
	ca, err := GenerateCA(tmpDir)
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	p := &Proxy{
		Addr: "127.0.0.1:8080",
		CA:   ca,
	}

	certPath := "/tmp/ca.pem"
	vars := p.EnvVars(certPath)

	// Should have 9 proxy vars + 4 CA vars
	if len(vars) != 13 {
		t.Fatalf("EnvVars(): expected 13 entries with CA, got %d: %v", len(vars), vars)
	}

	// Check that CA env vars are included
	caVarsFound := 0
	for _, v := range vars {
		for _, prefix := range []string{"SSL_CERT_FILE=", "REQUESTS_CA_BUNDLE=", "NODE_EXTRA_CA_CERTS=", "CURL_CA_BUNDLE="} {
			if len(v) > len(prefix) && v[:len(prefix)] == prefix {
				caVarsFound++
			}
		}
	}
	if caVarsFound != 4 {
		t.Errorf("Expected 4 CA env vars, found %d in %v", caVarsFound, vars)
	}
}

func TestHeaderMap(t *testing.T) {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("Authorization", "Bearer token123")
	h.Add("Accept", "text/html")

	m := headerMap(h)

	if m["Content-Type"] != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", m["Content-Type"], "application/json")
	}
	if m["Authorization"] != "Bearer token123" {
		t.Errorf("Authorization: got %q, want %q", m["Authorization"], "Bearer token123")
	}
	if m["Accept"] != "text/html" {
		t.Errorf("Accept: got %q, want %q", m["Accept"], "text/html")
	}
}

func TestHeaderMapEmpty(t *testing.T) {
	h := http.Header{}
	m := headerMap(h)
	if len(m) != 0 {
		t.Errorf("headerMap(empty): expected 0 entries, got %d", len(m))
	}
}

func TestHeaderMapMultipleValues(t *testing.T) {
	h := http.Header{}
	h.Add("X-Custom", "first")
	h.Add("X-Custom", "second")

	m := headerMap(h)

	// headerMap only takes the first value
	if m["X-Custom"] != "first" {
		t.Errorf("X-Custom: got %q, want %q (should use first value)", m["X-Custom"], "first")
	}
}

func TestTraceEntryFields(t *testing.T) {
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	entry := TraceEntry{
		Timestamp:        ts,
		ProviderFamily:   "anthropic",
		Model:            "claude-3-opus",
		CanonicalHash:    "hash123",
		FixtureHit:       true,
		DurationMs:       150,
		PromptTokens:     123,
		CompletionTokens: 45,
	}

	if entry.Timestamp != ts {
		t.Errorf("Timestamp: got %v, want %v", entry.Timestamp, ts)
	}
	if entry.ProviderFamily != "anthropic" {
		t.Errorf("ProviderFamily: got %q, want %q", entry.ProviderFamily, "anthropic")
	}
	if entry.Model != "claude-3-opus" {
		t.Errorf("Model: got %q, want %q", entry.Model, "claude-3-opus")
	}
	if entry.CanonicalHash != "hash123" {
		t.Errorf("CanonicalHash: got %q, want %q", entry.CanonicalHash, "hash123")
	}
	if !entry.FixtureHit {
		t.Error("FixtureHit: expected true")
	}
	if entry.DurationMs != 150 {
		t.Errorf("DurationMs: got %d, want 150", entry.DurationMs)
	}
	if entry.PromptTokens != 123 {
		t.Errorf("PromptTokens: got %d, want 123", entry.PromptTokens)
	}
	if entry.CompletionTokens != 45 {
		t.Errorf("CompletionTokens: got %d, want 45", entry.CompletionTokens)
	}
}

func TestProxyCanonicalHashConsistency(t *testing.T) {
	bodyA := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role":"user","content":"hello"}],
		"request_id": "abc123",
		"stream": false,
		"sdk_new_field": "alpha"
	}`)
	bodyB := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role":"user","content":"hello"}],
		"request_id": "xyz789",
		"stream": true,
		"sdk_new_field": "alpha"
	}`)
	bodyC := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role":"user","content":"hello"}],
		"request_id": "xyz789",
		"stream": false,
		"sdk_new_field": "beta"
	}`)

	normalizer := providers.Detect("api.openai.com", "/v1/chat/completions", bodyA, providers.AllNormalizers())

	ca, err := normalizer.Normalize("api.openai.com", "/v1/chat/completions", nil, bodyA)
	if err != nil {
		t.Fatalf("normalize A: %v", err)
	}
	cb, err := normalizer.Normalize("api.openai.com", "/v1/chat/completions", nil, bodyB)
	if err != nil {
		t.Fatalf("normalize B: %v", err)
	}
	cc, err := normalizer.Normalize("api.openai.com", "/v1/chat/completions", nil, bodyC)
	if err != nil {
		t.Fatalf("normalize C: %v", err)
	}

	jA, err := fixture.CanonicalizeRequest(ca)
	if err != nil {
		t.Fatalf("canonicalize A: %v", err)
	}
	jB, err := fixture.CanonicalizeRequest(cb)
	if err != nil {
		t.Fatalf("canonicalize B: %v", err)
	}
	jC, err := fixture.CanonicalizeRequest(cc)
	if err != nil {
		t.Fatalf("canonicalize C: %v", err)
	}

	hA := fixture.HashCanonical(jA)
	hB := fixture.HashCanonical(jB)
	hC := fixture.HashCanonical(jC)

	if hA != hB {
		t.Fatalf("hash mismatch for request_id/stream-only variation: %s vs %s", hA, hB)
	}
	if hA == hC {
		t.Fatalf("hash should change when unknown field changes: %s vs %s", hA, hC)
	}
	if strings.Contains(string(jA), "request_id") || strings.Contains(string(jA), "\"stream\"") {
		t.Fatalf("canonical request should not contain denylisted fields: %s", string(jA))
	}
	if !strings.Contains(string(jA), "\"sdk_new_field\":\"alpha\"") {
		t.Fatalf("canonical request should preserve unknown fields in extra: %s", string(jA))
	}
}

func TestStopWithNilServer(t *testing.T) {
	p := &Proxy{}
	err := p.Stop()
	if err != nil {
		t.Errorf("Stop() with nil server: expected nil error, got %v", err)
	}
}

func TestHandleDecryptedConnection_LargeRequest(t *testing.T) {
	// Verify requests larger than 64KB are handled correctly.
	// The old implementation used a fixed 65536-byte buffer.
	tmpDir := t.TempDir()
	store := fixture.NewStore(tmpDir)

	p := NewProxy("127.0.0.1:0", ModeRecorded, store, nil)

	// Build a request body larger than 64KB
	largeBody := strings.Repeat("x", 80000)
	reqStr := fmt.Sprintf("POST /v1/chat/completions HTTP/1.1\r\nHost: api.openai.com\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
		len(largeBody), largeBody)

	client, server := net.Pipe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.handleDecryptedConnection(server, "api.openai.com")
	}()

	// Write in a goroutine — net.Pipe is synchronous.
	go func() {
		client.Write([]byte(reqStr))
	}()

	// Read the response — we expect a 502 (fixture miss) rather than a truncation error.
	// Read in a loop to drain the full response before closing.
	client.SetReadDeadline(time.Now().Add(5 * time.Second))
	var resp []byte
	buf := make([]byte, 4096)
	for {
		n, err := client.Read(buf)
		if n > 0 {
			resp = append(resp, buf[:n]...)
		}
		if err != nil {
			break
		}
	}

	// The proxy should have parsed the full request (not truncated) and responded
	if !strings.Contains(string(resp), "HTTP/1.1") {
		t.Errorf("expected valid HTTP response, got: %s", string(resp[:min(100, len(resp))]))
	}
	<-done
}

func TestHandleDecryptedConnection_ProperStatusText(t *testing.T) {
	// Verify response uses correct status text (not hardcoded "OK" for non-200).
	tmpDir := t.TempDir()
	store := fixture.NewStore(tmpDir)

	p := NewProxy("127.0.0.1:0", ModeRecorded, store, nil)

	// A recorded-mode request with no fixture will return a 502 error
	reqStr := "POST /v1/chat/completions HTTP/1.1\r\nHost: api.openai.com\r\nContent-Length: 2\r\nConnection: close\r\n\r\n{}"

	client, server := net.Pipe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.handleDecryptedConnection(server, "api.openai.com")
	}()

	go func() {
		client.Write([]byte(reqStr))
	}()

	// Read the full response then close.
	client.SetReadDeadline(time.Now().Add(5 * time.Second))
	var resp []byte
	buf := make([]byte, 4096)
	for {
		n, err := client.Read(buf)
		if n > 0 {
			resp = append(resp, buf[:n]...)
		}
		if err != nil {
			break
		}
	}
	respStr := string(resp)

	// Should contain "502 Bad Gateway", not "502 OK"
	if strings.Contains(respStr, "502 OK") {
		t.Error("response should not use hardcoded 'OK' for error status codes")
	}
	if !strings.Contains(respStr, "502") {
		t.Errorf("expected 502 status, got: %s", respStr[:min(100, len(respStr))])
	}
	<-done
}

func TestHandleHTTP_MalformedJSONReturnsBadRequest(t *testing.T) {
	p := NewProxy("127.0.0.1:0", ModeRecorded, fixture.NewStore(t.TempDir()), nil)
	req := httptest.NewRequest("POST", "http://api.openai.com/v1/chat/completions", strings.NewReader(`{"model":`))
	req.Host = "api.openai.com"
	w := httptest.NewRecorder()

	p.handleHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"code":"malformed_json_request"`) {
		t.Fatalf("expected malformed_json_request code, got: %s", body)
	}
}

func TestHandleHTTP_RequestBodyTooLargeReturns413(t *testing.T) {
	p := NewProxy("127.0.0.1:0", ModeRecorded, fixture.NewStore(t.TempDir()), nil)
	p.MaxBodyBytes = 8

	req := httptest.NewRequest("POST", "http://api.openai.com/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`))
	req.Host = "api.openai.com"
	w := httptest.NewRecorder()

	p.handleHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusRequestEntityTooLarge)
	}
	if !strings.Contains(w.Body.String(), `"code":"request_body_too_large"`) {
		t.Fatalf("expected request_body_too_large code, got: %s", w.Body.String())
	}
}

func TestHandleHTTP_RequestHeaderTooLargeReturns431(t *testing.T) {
	p := NewProxy("127.0.0.1:0", ModeRecorded, fixture.NewStore(t.TempDir()), nil)
	p.MaxHeaderBytes = 32

	req := httptest.NewRequest("POST", "http://api.openai.com/v1/chat/completions", strings.NewReader(`{}`))
	req.Host = "api.openai.com"
	req.Header.Set("X-Large-Header", strings.Repeat("a", 128))
	w := httptest.NewRecorder()

	p.handleHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusRequestHeaderFieldsTooLarge {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusRequestHeaderFieldsTooLarge)
	}
	if !strings.Contains(w.Body.String(), `"code":"request_header_too_large"`) {
		t.Fatalf("expected request_header_too_large code, got: %s", w.Body.String())
	}
}

func TestHandleHTTP_WebSocketUpgradeNotSupported(t *testing.T) {
	p := NewProxy("127.0.0.1:0", ModeRecorded, fixture.NewStore(t.TempDir()), nil)
	req := httptest.NewRequest("GET", "http://api.openai.com/v1/chat/completions", nil)
	req.Host = "api.openai.com"
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	w := httptest.NewRecorder()

	p.handleHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotImplemented)
	}
	if !strings.Contains(w.Body.String(), `"code":"websocket_not_supported"`) {
		t.Fatalf("expected websocket_not_supported code, got: %s", w.Body.String())
	}
}

func TestHandleHTTP_FixtureMissIncludesNearestCandidateGuidance(t *testing.T) {
	store := fixture.NewStore(t.TempDir())
	recordedBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	seedOpenAIRecordedFixture(t, store, recordedBody)

	missingBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"different"}]}`
	normalizer := providers.Detect("api.openai.com", "/v1/chat/completions", []byte(recordedBody), providers.AllNormalizers())
	canonical, err := normalizer.Normalize("api.openai.com", "/v1/chat/completions", nil, []byte(recordedBody))
	if err != nil {
		t.Fatalf("normalize recorded body: %v", err)
	}
	canonicalBytes, err := fixture.CanonicalizeRequest(canonical)
	if err != nil {
		t.Fatalf("canonicalize recorded body: %v", err)
	}
	recordedHash := fixture.HashCanonical(canonicalBytes)

	p := NewProxy("127.0.0.1:0", ModeRecorded, store, nil)
	req := httptest.NewRequest("POST", "http://api.openai.com/v1/chat/completions", strings.NewReader(missingBody))
	req.Host = "api.openai.com"
	w := httptest.NewRecorder()

	p.handleHTTP(w, req)

	if w.Result().StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", w.Result().StatusCode, http.StatusBadGateway)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"code":"fixture_miss"`) {
		t.Fatalf("expected fixture_miss code, got: %s", body)
	}
	if !strings.Contains(body, "Nearest recorded fixtures:") {
		t.Fatalf("expected nearest-candidate guidance, got: %s", body)
	}
	if !strings.Contains(body, recordedHash) {
		t.Fatalf("expected nearest candidate hash %s, got: %s", recordedHash, body)
	}
}

func TestHandleHTTP_TraceIncludesTokenUsage(t *testing.T) {
	store := fixture.NewStore(t.TempDir())
	requestBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`

	normalizer := providers.Detect("api.openai.com", "/v1/chat/completions", []byte(requestBody), providers.AllNormalizers())
	canonical, err := normalizer.Normalize("api.openai.com", "/v1/chat/completions", nil, []byte(requestBody))
	if err != nil {
		t.Fatalf("normalize request: %v", err)
	}
	canonicalBytes, err := fixture.CanonicalizeRequest(canonical)
	if err != nil {
		t.Fatalf("canonicalize request: %v", err)
	}
	hash := fixture.HashCanonical(canonicalBytes)
	if err := store.PutModelFixture(&fixture.ModelFixture{
		FixtureID:        hash,
		HashVersion:      1,
		CanonicalHash:    hash,
		ProviderFamily:   canonical.ProviderFamily,
		Model:            canonical.Model,
		CanonicalRequest: canonicalBytes,
		Response:         json.RawMessage(`{"usage":{"prompt_tokens":11,"completion_tokens":7},"choices":[{"message":{"role":"assistant","content":"ok"}}]}`),
		RecordedAt:       time.Now().UTC(),
		RecordedBy:       "test",
	}); err != nil {
		t.Fatalf("put fixture: %v", err)
	}

	p := NewProxy("127.0.0.1:0", ModeRecorded, store, nil)
	req := httptest.NewRequest("POST", "http://api.openai.com/v1/chat/completions", strings.NewReader(requestBody))
	req.Host = "api.openai.com"
	w := httptest.NewRecorder()
	p.handleHTTP(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Result().StatusCode, http.StatusOK)
	}
	traces := p.Traces()
	if len(traces) != 1 {
		t.Fatalf("trace count = %d, want 1", len(traces))
	}
	if traces[0].PromptTokens != 11 || traces[0].CompletionTokens != 7 {
		t.Fatalf("trace token usage = (%d,%d), want (11,7)", traces[0].PromptTokens, traces[0].CompletionTokens)
	}
}

func TestHandleHTTP_FixtureMissIncludesModelVersionHint(t *testing.T) {
	store := fixture.NewStore(t.TempDir())
	recordedBody := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`
	seedOpenAIRecordedFixture(t, store, recordedBody)

	missingBody := `{"model":"gpt-4.1","messages":[{"role":"user","content":"hello"}]}`
	p := NewProxy("127.0.0.1:0", ModeRecorded, store, nil)
	req := httptest.NewRequest("POST", "http://api.openai.com/v1/chat/completions", strings.NewReader(missingBody))
	req.Host = "api.openai.com"
	w := httptest.NewRecorder()

	p.handleHTTP(w, req)

	if w.Result().StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", w.Result().StatusCode, http.StatusBadGateway)
	}
	body := w.Body.String()
	if !strings.Contains(body, "may be a model version change: recorded with gpt-4o-mini, requesting gpt-4.1") {
		t.Fatalf("expected model-version hint in fixture miss body, got: %s", body)
	}
	if !strings.Contains(body, "Run: gauntlet record --suite smoke") {
		t.Fatalf("expected model-version hint record command, got: %s", body)
	}
}

func TestHandleDecryptedConnection_HTTP2PrefaceReturns505(t *testing.T) {
	p := NewProxy("127.0.0.1:0", ModeRecorded, fixture.NewStore(t.TempDir()), nil)
	client, server := net.Pipe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.handleDecryptedConnection(server, "api.openai.com")
	}()

	go func() {
		_, _ = client.Write([]byte(http2ClientPreface))
	}()

	client.SetReadDeadline(time.Now().Add(5 * time.Second))
	var resp []byte
	buf := make([]byte, 2048)
	for {
		n, err := client.Read(buf)
		if n > 0 {
			resp = append(resp, buf[:n]...)
		}
		if err != nil {
			break
		}
	}
	respStr := string(resp)
	if !strings.Contains(respStr, "505") {
		t.Fatalf("expected 505 response, got: %s", respStr)
	}
	if !strings.Contains(respStr, `"code":"http2_not_supported"`) {
		t.Fatalf("expected http2_not_supported code, got: %s", respStr)
	}
	<-done
}

func TestHandleDecryptedConnection_MaxRequestsPerConnection(t *testing.T) {
	tmpDir := t.TempDir()
	store := fixture.NewStore(tmpDir)
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	seedOpenAIRecordedFixture(t, store, body)

	p := NewProxy("127.0.0.1:0", ModeRecorded, store, nil)
	p.MaxRequestsPerConn = 1

	req1 := fmt.Sprintf("POST /v1/chat/completions HTTP/1.1\r\nHost: api.openai.com\r\nContent-Length: %d\r\nConnection: keep-alive\r\n\r\n%s", len(body), body)
	req2 := fmt.Sprintf("POST /v1/chat/completions HTTP/1.1\r\nHost: api.openai.com\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)

	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.handleDecryptedConnection(server, "api.openai.com")
	}()

	go func() {
		_, _ = client.Write([]byte(req1 + req2))
	}()

	client.SetReadDeadline(time.Now().Add(5 * time.Second))
	var resp []byte
	buf := make([]byte, 4096)
	for {
		n, err := client.Read(buf)
		if n > 0 {
			resp = append(resp, buf[:n]...)
		}
		if err != nil {
			break
		}
	}
	respStr := string(resp)
	if !strings.Contains(respStr, "200 OK") {
		t.Fatalf("expected first request success, got: %s", respStr)
	}
	if !strings.Contains(respStr, "429 Too Many Requests") {
		t.Fatalf("expected request limit response, got: %s", respStr)
	}
	if !strings.Contains(respStr, `"code":"too_many_requests_per_connection"`) {
		t.Fatalf("expected limit error code, got: %s", respStr)
	}
	<-done
}

func TestStripStreamFlag(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		provider    string
		input       string
		wantHas     string // substring that should be present
		wantNot     string // substring that should be absent
		wantOutPath string
	}{
		{
			name:        "strips stream true",
			path:        "/v1/chat/completions",
			provider:    "openai_compatible",
			input:       `{"model":"gpt-4","stream":true,"messages":[]}`,
			wantHas:     `"model"`,
			wantNot:     `"stream"`,
			wantOutPath: "/v1/chat/completions",
		},
		{
			name:        "strips stream options with stream true",
			path:        "/v1/chat/completions",
			provider:    "openai_compatible",
			input:       `{"model":"gpt-4o","stream":true,"stream_options":{"include_usage":true},"messages":[]}`,
			wantHas:     `"model"`,
			wantNot:     `"stream_options"`,
			wantOutPath: "/v1/chat/completions",
		},
		{
			name:        "keeps stream false",
			path:        "/v1/chat/completions",
			provider:    "openai_compatible",
			input:       `{"model":"gpt-4","stream":false,"messages":[]}`,
			wantHas:     `"stream"`,
			wantOutPath: "/v1/chat/completions",
		},
		{
			name:        "keeps stream string true unchanged",
			path:        "/v1/chat/completions",
			provider:    "openai_compatible",
			input:       `{"model":"gpt-4","stream":"true","messages":[]}`,
			wantHas:     `"stream":"true"`,
			wantOutPath: "/v1/chat/completions",
		},
		{
			name:        "no stream field",
			path:        "/v1/chat/completions",
			provider:    "openai_compatible",
			input:       `{"model":"gpt-4","messages":[]}`,
			wantHas:     `"model"`,
			wantOutPath: "/v1/chat/completions",
		},
		{
			name:        "invalid json unchanged",
			path:        "/v1/chat/completions",
			provider:    "openai_compatible",
			input:       `not json`,
			wantHas:     `not json`,
			wantOutPath: "/v1/chat/completions",
		},
		{
			name:        "ollama stream forced false",
			path:        "/api/chat",
			provider:    "openai_compatible",
			input:       `{"model":"llama3.2","stream":true,"messages":[]}`,
			wantHas:     `"stream":false`,
			wantOutPath: "/api/chat",
		},
		{
			name:        "gemini path rewritten",
			path:        "/v1beta/models/gemini-2.0-flash:streamGenerateContent",
			provider:    "google",
			input:       `{"contents":[]}`,
			wantHas:     `"contents"`,
			wantOutPath: "/v1beta/models/gemini-2.0-flash:generateContent",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outPath, outBody := stripStreamFlag(tt.path, tt.provider, []byte(tt.input))
			got := string(outBody)
			if tt.wantHas != "" && !strings.Contains(got, tt.wantHas) {
				t.Errorf("expected %q in result: %s", tt.wantHas, got)
			}
			if tt.wantNot != "" && strings.Contains(got, tt.wantNot) {
				t.Errorf("did not expect %q in result: %s", tt.wantNot, got)
			}
			if tt.wantOutPath != "" && outPath != tt.wantOutPath {
				t.Errorf("path = %q, want %q", outPath, tt.wantOutPath)
			}
		})
	}
}

func TestTLSCertCaching(t *testing.T) {
	tmpDir := t.TempDir()
	ca, err := GenerateCA(tmpDir)
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}

	cert1, err := ca.IssueHostCert("example.com")
	if err != nil {
		t.Fatalf("IssueHostCert: %v", err)
	}
	cert2, err := ca.IssueHostCert("example.com")
	if err != nil {
		t.Fatalf("IssueHostCert second call: %v", err)
	}

	// Same pointer means it was cached, not regenerated.
	if cert1 != cert2 {
		t.Error("expected cached cert to be returned on second call")
	}

	// Different host should get a different cert.
	cert3, err := ca.IssueHostCert("other.com")
	if err != nil {
		t.Fatalf("IssueHostCert other: %v", err)
	}
	if cert1 == cert3 {
		t.Error("different hosts should get different certs")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func seedOpenAIRecordedFixture(t *testing.T, store *fixture.Store, body string) {
	t.Helper()
	normalizer := providers.Detect("api.openai.com", "/v1/chat/completions", []byte(body), providers.AllNormalizers())
	canonical, err := normalizer.Normalize("api.openai.com", "/v1/chat/completions", nil, []byte(body))
	if err != nil {
		t.Fatalf("normalize body: %v", err)
	}
	canonicalBytes, err := fixture.CanonicalizeRequest(canonical)
	if err != nil {
		t.Fatalf("canonicalize body: %v", err)
	}
	hash := fixture.HashCanonical(canonicalBytes)
	mf := &fixture.ModelFixture{
		FixtureID:        hash,
		HashVersion:      1,
		CanonicalHash:    hash,
		ProviderFamily:   canonical.ProviderFamily,
		Model:            canonical.Model,
		CanonicalRequest: canonicalBytes,
		Response:         json.RawMessage(`{"id":"resp_1","choices":[{"message":{"role":"assistant","content":"ok"}}]}`),
		RecordedAt:       time.Now().UTC(),
		RecordedBy:       "test",
	}
	if err := store.PutModelFixture(mf); err != nil {
		t.Fatalf("put fixture: %v", err)
	}
}
