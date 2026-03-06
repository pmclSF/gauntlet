package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testUpstream creates a TLS test server that returns the specified status,
// content type, body, and optional extra headers.
func testUpstream(t *testing.T, status int, contentType string, body string, extraHeaders map[string]string) (*httptest.Server, func()) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		for k, v := range extraHeaders {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	origTransport := defaultTransport
	defaultTransport = &upstreamTransport{client: srv.Client()}
	cleanup := func() {
		defaultTransport = origTransport
		srv.Close()
	}
	return srv, cleanup
}

func TestIntegration_PassthroughTextPlain(t *testing.T) {
	srv, cleanup := testUpstream(t, http.StatusOK, "text/plain", "hello world", nil)
	defer cleanup()

	p := &Proxy{Mode: ModePassthrough}
	host := strings.TrimPrefix(srv.URL, "https://")

	resp, err := p.handlePassthrough(context.Background(), "GET", host, "/health", "", nil, nil)
	if err != nil {
		t.Fatalf("handlePassthrough failed: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.Status)
	}
	if resp.Headers["Content-Type"] != "text/plain" {
		t.Errorf("Content-Type = %q, want text/plain", resp.Headers["Content-Type"])
	}
	if string(resp.Body) != "hello world" {
		t.Errorf("body = %q, want hello world", string(resp.Body))
	}
}

func TestIntegration_PassthroughHTMLError(t *testing.T) {
	htmlBody := "<html><body><h1>503 Service Unavailable</h1></body></html>"
	srv, cleanup := testUpstream(t, http.StatusServiceUnavailable, "text/html; charset=utf-8", htmlBody, nil)
	defer cleanup()

	p := &Proxy{Mode: ModePassthrough}
	host := strings.TrimPrefix(srv.URL, "https://")

	resp, err := p.handlePassthrough(context.Background(), "GET", host, "/v1/models", "", nil, nil)
	if err != nil {
		t.Fatalf("handlePassthrough failed: %v", err)
	}
	if resp.Status != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.Status)
	}
	if resp.Headers["Content-Type"] != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", resp.Headers["Content-Type"])
	}
	if string(resp.Body) != htmlBody {
		t.Errorf("body = %q, want HTML error page", string(resp.Body))
	}
}

func TestIntegration_Passthrough429WithRetryAfter(t *testing.T) {
	srv, cleanup := testUpstream(t, http.StatusTooManyRequests, "application/json", `{"error":"rate limited"}`, map[string]string{
		"Retry-After": "30",
	})
	defer cleanup()

	p := &Proxy{Mode: ModePassthrough}
	host := strings.TrimPrefix(srv.URL, "https://")

	resp, err := p.handlePassthrough(context.Background(), "POST", host, "/v1/chat/completions", "", nil, []byte(`{"model":"test"}`))
	if err != nil {
		t.Fatalf("handlePassthrough failed: %v", err)
	}
	if resp.Status != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", resp.Status)
	}
	if resp.Headers["Retry-After"] != "30" {
		t.Errorf("Retry-After = %q, want 30", resp.Headers["Retry-After"])
	}
}

func TestIntegration_PassthroughEmptyBody(t *testing.T) {
	srv, cleanup := testUpstream(t, http.StatusNoContent, "", "", nil)
	defer cleanup()

	p := &Proxy{Mode: ModePassthrough}
	host := strings.TrimPrefix(srv.URL, "https://")

	resp, err := p.handlePassthrough(context.Background(), "DELETE", host, "/v1/files/abc", "", nil, nil)
	if err != nil {
		t.Fatalf("handlePassthrough failed: %v", err)
	}
	if resp.Status != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.Status)
	}
	if len(resp.Body) != 0 {
		t.Errorf("body = %q, want empty", string(resp.Body))
	}
}

func TestIntegration_UnknownProviderPassthroughPlainText(t *testing.T) {
	srv, cleanup := testUpstream(t, http.StatusOK, "text/plain", "pong", nil)
	defer cleanup()

	p := &Proxy{Mode: ModePassthrough}
	host := strings.TrimPrefix(srv.URL, "https://")

	// Request to unknown endpoint with plain text response
	resp, err := p.handlePassthrough(context.Background(), "GET", host, "/ping", "", nil, nil)
	if err != nil {
		t.Fatalf("handlePassthrough failed: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.Status)
	}
	if resp.Headers["Content-Type"] != "text/plain" {
		t.Errorf("Content-Type = %q, want text/plain", resp.Headers["Content-Type"])
	}
	if string(resp.Body) != "pong" {
		t.Errorf("body = %q, want pong", string(resp.Body))
	}
}

func TestIntegration_InterceptPassthrough_PreservesNonJSONContentType(t *testing.T) {
	srv, cleanup := testUpstream(t, http.StatusOK, "text/plain", "plain response", nil)
	defer cleanup()

	host := strings.TrimPrefix(srv.URL, "https://")
	p := &Proxy{Mode: ModePassthrough}

	resp, err := p.interceptRequest(context.Background(), "GET", host, "/health", "", nil, nil)
	if err != nil {
		t.Fatalf("interceptRequest failed: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.Status)
	}
	if resp.Headers["Content-Type"] != "text/plain" {
		t.Errorf("Content-Type = %q, want text/plain", resp.Headers["Content-Type"])
	}
}

func TestIntegration_InterceptPassthrough_429WithRetryAfter(t *testing.T) {
	srv, cleanup := testUpstream(t, http.StatusTooManyRequests, "application/json", `{"error":"slow down"}`, map[string]string{
		"Retry-After": "60",
	})
	defer cleanup()

	host := strings.TrimPrefix(srv.URL, "https://")
	p := &Proxy{Mode: ModePassthrough}

	resp, err := p.interceptRequest(context.Background(), "POST", host, "/v1/chat/completions", "", nil, []byte(`{"model":"test"}`))
	if err != nil {
		t.Fatalf("interceptRequest failed: %v", err)
	}
	if resp.Status != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", resp.Status)
	}
	if resp.Headers["Retry-After"] != "60" {
		t.Errorf("Retry-After = %q, want 60", resp.Headers["Retry-After"])
	}
}

func TestIntegration_InterceptPassthrough_HTMLErrorResponse(t *testing.T) {
	htmlError := "<html><body>Internal Server Error</body></html>"
	srv, cleanup := testUpstream(t, http.StatusInternalServerError, "text/html", htmlError, nil)
	defer cleanup()

	host := strings.TrimPrefix(srv.URL, "https://")
	p := &Proxy{Mode: ModePassthrough}

	resp, err := p.interceptRequest(context.Background(), "POST", host, "/v1/chat/completions", "", nil, []byte(`{"model":"test"}`))
	if err != nil {
		t.Fatalf("interceptRequest failed: %v", err)
	}
	if resp.Status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.Status)
	}
	if resp.Headers["Content-Type"] != "text/html" {
		t.Errorf("Content-Type = %q, want text/html", resp.Headers["Content-Type"])
	}
}
