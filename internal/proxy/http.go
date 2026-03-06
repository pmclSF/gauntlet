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

	respBody, statusCode, err := p.interceptRequest(r.Method, hostname, r.URL.Path, r.URL.RawQuery, headerMap(r.Header), body)
	if err != nil {
		p.writeHTTPError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if _, err := w.Write(respBody); err != nil {
		return
	}
}
