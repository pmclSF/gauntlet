package proxy

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

// handleConnect handles HTTPS CONNECT tunneling with MITM interception.
func (p *Proxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		log.Printf("hijack error: %v", err)
		return
	}

	hostname := r.Host
	if !strings.Contains(hostname, ":") {
		hostname += ":443"
	}
	host := strings.Split(hostname, ":")[0]

	if p.CA == nil {
		// No CA, just tunnel
		p.tunnelDirect(clientConn, hostname)
		return
	}

	// Issue a cert for this host and do MITM
	cert, err := p.CA.IssueHostCert(host)
	if err != nil {
		log.Printf("failed to issue cert for %s: %v", host, err)
		clientConn.Close()
		return
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{*cert},
		// Force HTTP/1.1 — the proxy parses requests with http.ReadRequest
		// which only supports HTTP/1.x. Without this, HTTP/2-capable clients
		// would negotiate h2 and the proxy couldn't parse the binary frames.
		NextProtos: []string{"http/1.1"},
	}

	tlsConn := tls.Server(clientConn, tlsConfig)
	if err := tlsConn.Handshake(); err != nil {
		log.Printf("TLS handshake failed for %s: %v", host, err)
		tlsConn.Close()
		return
	}

	// Read the decrypted HTTP request
	p.handleDecryptedConnection(tlsConn, host)
}

// handleDecryptedConnection processes decrypted HTTP requests from a MITM'd
// TLS connection. Supports HTTP keep-alive (multiple requests per connection).
func (p *Proxy) handleDecryptedConnection(conn net.Conn, hostname string) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	requestCount := 0

	for {
		if prefix, err := reader.Peek(len(http2ClientPreface)); err == nil && bytes.Equal(prefix, []byte(http2ClientPreface)) {
			p.writeConnError(conn, &proxyRequestError{
				StatusCode: http.StatusHTTPVersionNotSupported,
				Code:       "http2_not_supported",
				Message:    "http/2 over CONNECT tunnel is not supported; force HTTP/1.1 for proxy traffic",
			})
			return
		}

		req, err := http.ReadRequest(reader)
		if err != nil {
			return // EOF or parse error — close the connection
		}
		requestCount++
		if requestCount > p.effectiveMaxRequestsPerConn() {
			p.writeConnError(conn, &proxyRequestError{
				StatusCode: http.StatusTooManyRequests,
				Code:       "too_many_requests_per_connection",
				Message:    fmt.Sprintf("proxy connection exceeded max request count (%d)", p.effectiveMaxRequestsPerConn()),
			})
			return
		}

		if requestHeaderBytes(req) > p.effectiveMaxHeaderBytes() {
			p.writeConnError(conn, &proxyRequestError{
				StatusCode: http.StatusRequestHeaderFieldsTooLarge,
				Code:       "request_header_too_large",
				Message:    fmt.Sprintf("request headers exceed proxy limit (%d bytes)", p.effectiveMaxHeaderBytes()),
			})
			return
		}
		if isWebSocketUpgrade(req.Header) {
			p.writeConnError(conn, &proxyRequestError{
				StatusCode: http.StatusNotImplemented,
				Code:       "websocket_not_supported",
				Message:    "websocket upgrade is not supported by gauntlet proxy",
			})
			return
		}

		body, bodyErr := p.readRequestBody(req.Body)
		if bodyErr != nil {
			p.writeConnError(conn, bodyErr)
			return
		}

		// Process through interception pipeline
		respBody, statusCode, interceptErr := p.interceptRequest(req.Method, hostname, req.URL.Path, req.URL.RawQuery, headerMap(req.Header), body)
		if interceptErr != nil {
			p.writeConnError(conn, interceptErr)
			return
		}

		resp := &http.Response{
			StatusCode:    statusCode,
			ProtoMajor:    1,
			ProtoMinor:    1,
			Header:        make(http.Header),
			Body:          io.NopCloser(bytes.NewReader(respBody)),
			ContentLength: int64(len(respBody)),
		}
		resp.Header.Set("Content-Type", "application/json")
		if err := resp.Write(conn); err != nil {
			return
		}

		// If the client signaled Connection: close, stop.
		if req.Close {
			return
		}
	}
}

// tunnelDirect blindly tunnels data between client and server without MITM.
func (p *Proxy) tunnelDirect(clientConn net.Conn, hostname string) {
	defer clientConn.Close()

	serverConn, err := net.DialTimeout("tcp", hostname, 10*time.Second)
	if err != nil {
		return
	}
	defer serverConn.Close()

	go func() {
		_, _ = io.Copy(serverConn, clientConn)
	}()
	_, _ = io.Copy(clientConn, serverConn)
}
