package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// upstreamTransport handles forwarding requests to upstream model endpoints.
// It preserves the original HTTP method, URL (including query string), and
// relevant headers. The transport bypasses HTTP_PROXY/HTTPS_PROXY to prevent
// routing through the proxy itself.
type upstreamTransport struct {
	client *http.Client
}

func newUpstreamTransport() *upstreamTransport {
	return &upstreamTransport{
		client: &http.Client{
			Transport: &http.Transport{
				Proxy: nil, // bypass proxy env to prevent loop
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
				IdleConnTimeout:       90 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
			// No global client timeout — model calls can be slow.
			// Per-request timeouts should be set via context if needed.
		},
	}
}

// upstreamResponse holds the response from an upstream endpoint.
type upstreamResponse struct {
	Body       []byte
	StatusCode int
	Headers    map[string]string
}

// responseHeaderAllowlist defines headers safe to preserve across
// replay, live recording, and passthrough. Only these headers are
// captured from upstream responses and stored in fixtures.
var responseHeaderAllowlist = []string{
	"Content-Type",
	"Content-Encoding",
	"Cache-Control",
	"Retry-After",
}

// extractAllowlistedHeaders returns a map of allowlisted headers from an
// http.Header, using canonical header names. Only non-empty values are included.
func extractAllowlistedHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(responseHeaderAllowlist))
	for _, key := range responseHeaderAllowlist {
		if v := h.Get(key); v != "" {
			out[key] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Forward sends a request to the upstream endpoint and returns the response
// body, status code, and allowlisted headers. It preserves the original HTTP
// method, path, query string, and relevant headers.
func (t *upstreamTransport) Forward(ctx context.Context, method, hostname, path, rawQuery string, headers map[string]string, body []byte) (*upstreamResponse, error) {
	url := "https://" + hostname + path
	if rawQuery != "" {
		url += "?" + rawQuery
	}

	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("upstream request construction failed: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read upstream response: %w", err)
	}

	return &upstreamResponse{
		Body:       respBody,
		StatusCode: resp.StatusCode,
		Headers:    extractAllowlistedHeaders(resp.Header),
	}, nil
}
