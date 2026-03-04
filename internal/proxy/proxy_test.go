package proxy

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gauntlet-dev/gauntlet/internal/fixture"
	"github.com/gauntlet-dev/gauntlet/internal/proxy/providers"
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

	// Should have 8 proxy vars + 4 CA vars
	if len(vars) != 12 {
		t.Fatalf("EnvVars(): expected 12 entries with CA, got %d: %v", len(vars), vars)
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
		Timestamp:      ts,
		ProviderFamily: "anthropic",
		Model:          "claude-3-opus",
		CanonicalHash:  "hash123",
		FixtureHit:     true,
		DurationMs:     150,
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

func TestStripStreamFlag(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantHas  string // substring that should be present
		wantNot  string // substring that should be absent
	}{
		{
			name:    "strips stream true",
			input:   `{"model":"gpt-4","stream":true,"messages":[]}`,
			wantHas: `"model"`,
			wantNot: `"stream"`,
		},
		{
			name:    "keeps stream false",
			input:   `{"model":"gpt-4","stream":false,"messages":[]}`,
			wantHas: `"stream"`,
		},
		{
			name:    "no stream field",
			input:   `{"model":"gpt-4","messages":[]}`,
			wantHas: `"model"`,
		},
		{
			name:    "invalid json unchanged",
			input:   `not json`,
			wantHas: `not json`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(stripStreamFlag([]byte(tt.input)))
			if tt.wantHas != "" && !strings.Contains(got, tt.wantHas) {
				t.Errorf("expected %q in result: %s", tt.wantHas, got)
			}
			if tt.wantNot != "" && strings.Contains(got, tt.wantNot) {
				t.Errorf("did not expect %q in result: %s", tt.wantNot, got)
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
