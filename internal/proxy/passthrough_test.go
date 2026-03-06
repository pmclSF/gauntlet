package proxy

import (
	"context"
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

	resp, err := p.handlePassthrough(context.Background(), "GET", host, "/v1/models", "version=2", nil, nil)
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
	if resp.Status != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.Status)
	}
	if len(resp.Body) == 0 {
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

	resp, err := p.handlePassthrough(context.Background(), "POST", host, "/v1/chat/completions", "", nil, body)
	if err != nil {
		t.Fatalf("handlePassthrough POST failed: %v", err)
	}
	if receivedMethod != "POST" {
		t.Errorf("method = %s, want POST", receivedMethod)
	}
	if resp.Status != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.Status)
	}
	if string(receivedBody) != string(body) {
		t.Errorf("body = %s, want %s", receivedBody, body)
	}
}

func TestPassthrough_PreservesContentType(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("rate limited, try again later"))
	}))
	defer srv.Close()

	origTransport := defaultTransport
	defaultTransport = &upstreamTransport{client: srv.Client()}
	defer func() { defaultTransport = origTransport }()

	p := &Proxy{Mode: ModePassthrough}
	host := strings.TrimPrefix(srv.URL, "https://")

	resp, err := p.handlePassthrough(context.Background(), "POST", host, "/v1/chat/completions", "", nil, []byte(`{"model":"test"}`))
	if err != nil {
		t.Fatalf("handlePassthrough failed: %v", err)
	}
	if resp.Status != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", resp.Status)
	}
	if resp.Headers["Content-Type"] != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/plain; charset=utf-8", resp.Headers["Content-Type"])
	}
	if resp.Headers["Retry-After"] != "120" {
		t.Errorf("Retry-After = %q, want 120", resp.Headers["Retry-After"])
	}
	if string(resp.Body) != "rate limited, try again later" {
		t.Errorf("body = %q, want plain text error", string(resp.Body))
	}
}

func TestPassthrough_CancelledContext(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	origTransport := defaultTransport
	defaultTransport = &upstreamTransport{client: srv.Client()}
	defer func() { defaultTransport = origTransport }()

	p := &Proxy{Mode: ModePassthrough}
	host := strings.TrimPrefix(srv.URL, "https://")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := p.handlePassthrough(ctx, "POST", host, "/v1/chat/completions", "", nil, []byte(`{"model":"test"}`))
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}
