package proxy

import (
	"fmt"
	"net/http"
	"strings"
)

// handleHTTP handles plain HTTP proxy requests (non-CONNECT).
func (p *Proxy) handleHTTP(w http.ResponseWriter, r *http.Request) {
	if requestHeaderBytes(r) > p.effectiveMaxHeaderBytes() {
		p.writeHTTPError(w, &proxyRequestError{
			StatusCode: http.StatusRequestHeaderFieldsTooLarge,
			Code:       "request_header_too_large",
			Message:    fmt.Sprintf("request headers exceed proxy limit (%d bytes)", p.effectiveMaxHeaderBytes()),
		})
		return
	}
	if isWebSocketUpgrade(r.Header) {
		p.writeHTTPError(w, &proxyRequestError{
			StatusCode: http.StatusNotImplemented,
			Code:       "websocket_not_supported",
			Message:    "websocket upgrade is not supported by gauntlet proxy",
		})
		return
	}
	body, err := p.readRequestBody(r.Body)
	if err != nil {
		p.writeHTTPError(w, err)
		return
	}

	hostname := r.Host
	if strings.Contains(hostname, ":") {
		hostname = strings.Split(hostname, ":")[0]
	}

	resp, err := p.interceptRequest(r.Context(), r.Method, hostname, r.URL.Path, r.URL.RawQuery, headerMap(r.Header), body)
	if err != nil {
		p.writeHTTPError(w, err)
		return
	}

	writeResponseHeaders(w, resp.Headers)
	w.WriteHeader(resp.Status)
	if _, err := w.Write(resp.Body); err != nil {
		return
	}
}

// writeResponseHeaders sets allowlisted response headers on an http.ResponseWriter.
// If no Content-Type is present in the headers, defaults to application/json
// for backward compatibility.
func writeResponseHeaders(w http.ResponseWriter, headers map[string]string) {
	hasContentType := false
	for k, v := range headers {
		if strings.EqualFold(k, "Content-Type") {
			hasContentType = true
		}
		w.Header().Set(k, v)
	}
	if !hasContentType {
		w.Header().Set("Content-Type", "application/json")
	}
}

// setConnResponseHeaders sets allowlisted response headers on an http.Header
// (used for CONNECT tunnel responses written to net.Conn). If no Content-Type
// is present, defaults to application/json for backward compatibility.
func setConnResponseHeaders(h http.Header, headers map[string]string) {
	hasContentType := false
	for k, v := range headers {
		if strings.EqualFold(k, "Content-Type") {
			hasContentType = true
		}
		h.Set(k, v)
	}
	if !hasContentType {
		h.Set("Content-Type", "application/json")
	}
}
