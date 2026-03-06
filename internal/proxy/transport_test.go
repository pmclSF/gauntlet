package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpstreamTransport_PreservesMethod(t *testing.T) {
	var receivedMethod string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	transport := &upstreamTransport{client: srv.Client()}
	// The test server uses https, extract host from URL
	host := strings.TrimPrefix(srv.URL, "https://")

	for _, method := range []string{"GET", "POST", "PUT", "DELETE", "PATCH"} {
		resp, err := transport.Forward(t.Context(), method, host, "/v1/chat/completions", "", nil, []byte(`{"model":"test"}`))
		if err != nil {
			t.Fatalf("Forward(%s) failed: %v", method, err)
		}
		if receivedMethod != method {
			t.Errorf("Forward(%s): upstream received %s", method, receivedMethod)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Forward(%s): status = %d, want 200", method, resp.StatusCode)
		}
	}
}

func TestUpstreamTransport_PreservesQueryString(t *testing.T) {
	var receivedQuery string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	transport := &upstreamTransport{client: srv.Client()}
	host := strings.TrimPrefix(srv.URL, "https://")

	_, err := transport.Forward(t.Context(), "POST", host, "/v1/models", "key=abc123&version=2", nil, []byte(`{}`))
	if err != nil {
		t.Fatalf("Forward failed: %v", err)
	}
	if receivedQuery != "key=abc123&version=2" {
		t.Errorf("query string not preserved: got %q", receivedQuery)
	}
}

func TestUpstreamTransport_PreservesUpstreamStatusCode(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()

	transport := &upstreamTransport{client: srv.Client()}
	host := strings.TrimPrefix(srv.URL, "https://")

	resp, err := transport.Forward(t.Context(), "POST", host, "/v1/chat/completions", "", nil, []byte(`{"model":"test"}`))
	if err != nil {
		t.Fatalf("Forward failed: %v", err)
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusTooManyRequests)
	}
}

func TestUpstreamTransport_EmptyBodyGET(t *testing.T) {
	var receivedMethod string
	var receivedContentLength int64
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedContentLength = r.ContentLength
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer srv.Close()

	transport := &upstreamTransport{client: srv.Client()}
	host := strings.TrimPrefix(srv.URL, "https://")

	_, err := transport.Forward(t.Context(), "GET", host, "/v1/models", "", nil, nil)
	if err != nil {
		t.Fatalf("Forward failed: %v", err)
	}
	if receivedMethod != "GET" {
		t.Errorf("method = %s, want GET", receivedMethod)
	}
	if receivedContentLength > 0 {
		t.Errorf("content-length = %d, want 0 for empty body GET", receivedContentLength)
	}
}

func TestUpstreamTransport_PreservesResponseHeaders(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Retry-After", "30")
		w.Header().Set("X-Custom-Header", "should-be-filtered")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	transport := &upstreamTransport{client: srv.Client()}
	host := strings.TrimPrefix(srv.URL, "https://")

	resp, err := transport.Forward(t.Context(), "POST", host, "/v1/chat/completions", "", nil, []byte(`{"model":"test"}`))
	if err != nil {
		t.Fatalf("Forward failed: %v", err)
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", resp.StatusCode)
	}
	if resp.Headers["Content-Type"] != "text/plain" {
		t.Errorf("Content-Type = %q, want text/plain", resp.Headers["Content-Type"])
	}
	if resp.Headers["Retry-After"] != "30" {
		t.Errorf("Retry-After = %q, want 30", resp.Headers["Retry-After"])
	}
	if _, ok := resp.Headers["X-Custom-Header"]; ok {
		t.Error("X-Custom-Header should not be in allowlisted headers")
	}
}
