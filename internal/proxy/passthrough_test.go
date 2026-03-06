package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPassthrough_GET(t *testing.T) {
	var receivedMethod string
	var receivedPath string
	var receivedQuery string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		receivedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer srv.Close()

	// Temporarily swap defaultTransport for test
	origTransport := defaultTransport
	defaultTransport = &upstreamTransport{client: srv.Client()}
	defer func() { defaultTransport = origTransport }()

	p := &Proxy{Mode: ModePassthrough}
	host := strings.TrimPrefix(srv.URL, "https://")

	respBody, status, err := p.handlePassthrough("GET", host, "/v1/models", "version=2", nil, nil)
	if err != nil {
		t.Fatalf("handlePassthrough GET failed: %v", err)
	}
	if receivedMethod != "GET" {
		t.Errorf("method = %s, want GET", receivedMethod)
	}
	if receivedPath != "/v1/models" {
		t.Errorf("path = %s, want /v1/models", receivedPath)
	}
	if receivedQuery != "version=2" {
		t.Errorf("query = %s, want version=2", receivedQuery)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if len(respBody) == 0 {
		t.Error("expected non-empty response body")
	}
}

func TestPassthrough_POST(t *testing.T) {
	var receivedMethod string
	var receivedBody []byte
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		receivedBody = buf
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	origTransport := defaultTransport
	defaultTransport = &upstreamTransport{client: srv.Client()}
	defer func() { defaultTransport = origTransport }()

	p := &Proxy{Mode: ModePassthrough}
	host := strings.TrimPrefix(srv.URL, "https://")
	body := []byte(`{"model":"test","messages":[]}`)

	_, status, err := p.handlePassthrough("POST", host, "/v1/chat/completions", "", nil, body)
	if err != nil {
		t.Fatalf("handlePassthrough POST failed: %v", err)
	}
	if receivedMethod != "POST" {
		t.Errorf("method = %s, want POST", receivedMethod)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if string(receivedBody) != string(body) {
		t.Errorf("body = %s, want %s", receivedBody, body)
	}
}
