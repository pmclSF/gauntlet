package proxy

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

func headerMap(h http.Header) map[string]string {
	m := make(map[string]string)
	for k, v := range h {
		if len(v) > 0 {
			m[k] = v[0]
		}
	}
	return m
}

func (p *Proxy) effectiveMaxHeaderBytes() int {
	if p.MaxHeaderBytes <= 0 {
		return defaultMaxHeaderBytes
	}
	return p.MaxHeaderBytes
}

func (p *Proxy) effectiveMaxBodyBytes() int64 {
	if p.MaxBodyBytes <= 0 {
		return defaultMaxBodyBytes
	}
	return p.MaxBodyBytes
}

func (p *Proxy) effectiveMaxRequestsPerConn() int {
	if p.MaxRequestsPerConn <= 0 {
		return defaultMaxRequestsPerConn
	}
	return p.MaxRequestsPerConn
}

func (p *Proxy) readRequestBody(body io.ReadCloser) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	defer body.Close()
	maxBytes := p.effectiveMaxBodyBytes()
	payload, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return nil, &proxyRequestError{
			StatusCode: http.StatusBadGateway,
			Code:       "request_read_failed",
			Message:    "failed to read request body",
			Cause:      err,
		}
	}
	if int64(len(payload)) > maxBytes {
		return nil, &proxyRequestError{
			StatusCode: http.StatusRequestEntityTooLarge,
			Code:       "request_body_too_large",
			Message:    fmt.Sprintf("request body exceeds proxy limit (%d bytes)", maxBytes),
		}
	}
	return payload, nil
}

func requestHeaderBytes(req *http.Request) int {
	if req == nil {
		return 0
	}
	total := len(req.Method) + 1 + len(req.URL.RequestURI()) + 1 + len(req.Proto) + 2
	for key, values := range req.Header {
		for _, value := range values {
			total += len(key) + 2 + len(value) + 2
		}
	}
	return total
}

func isWebSocketUpgrade(headers http.Header) bool {
	if headers == nil {
		return false
	}
	upgrade := strings.TrimSpace(headers.Get("Upgrade"))
	if !strings.EqualFold(upgrade, "websocket") {
		return false
	}
	return strings.Contains(strings.ToLower(headers.Get("Connection")), "upgrade")
}
