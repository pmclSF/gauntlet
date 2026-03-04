// Package proxy implements the Gauntlet MITM HTTP/HTTPS proxy.
// It intercepts all model calls from the TUT and routes them through
// the fixture store for deterministic replay.
package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gauntlet-dev/gauntlet/internal/fixture"
	"github.com/gauntlet-dev/gauntlet/internal/proxy/providers"
	"github.com/gauntlet-dev/gauntlet/internal/redaction"
)

// Mode determines how the proxy handles intercepted requests.
type Mode string

const (
	// ModeRecorded serves fixture responses only. A miss is a hard failure.
	ModeRecorded Mode = "recorded"
	// ModeLive forwards to real endpoints and records responses.
	ModeLive Mode = "live"
	// ModePassthrough forwards without recording.
	ModePassthrough Mode = "passthrough"
)

// TraceWriter receives trace events for intercepted calls.
type TraceWriter interface {
	WriteTrace(event TraceEntry)
}

// TraceEntry is a single intercepted call record.
type TraceEntry struct {
	Timestamp      time.Time `json:"timestamp"`
	ProviderFamily string    `json:"provider_family"`
	Model          string    `json:"model"`
	CanonicalHash  string    `json:"canonical_hash"`
	FixtureHit     bool      `json:"fixture_hit"`
	DurationMs     int       `json:"duration_ms"`
}

// Proxy is the local MITM HTTP/HTTPS proxy.
type Proxy struct {
	Addr        string
	Mode        Mode
	Store       *fixture.Store
	Normalizers []providers.ProviderNormalizer
	CA          *CA
	Redactor    *redaction.Redactor

	server   *http.Server
	mu       sync.Mutex
	traces   []TraceEntry
	listener net.Listener
}

// NewProxy creates a new proxy with default settings.
func NewProxy(addr string, mode Mode, store *fixture.Store, ca *CA) *Proxy {
	return &Proxy{
		Addr:        addr,
		Mode:        mode,
		Store:       store,
		Normalizers: providers.AllNormalizers(),
		CA:          ca,
		Redactor:    redaction.DefaultRedactor(),
	}
}

// Start begins listening for proxy connections.
func (p *Proxy) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", p.handleHTTP)

	p.server = &http.Server{
		Addr:    p.Addr,
		Handler: http.HandlerFunc(p.handleRequest),
	}

	ln, err := net.Listen("tcp", p.Addr)
	if err != nil {
		return fmt.Errorf("proxy failed to listen on %s: %w", p.Addr, err)
	}
	// Persist the resolved listener address (important when configured with :0).
	p.Addr = ln.Addr().String()
	p.listener = ln

	go func() {
		if err := p.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("proxy server error: %v", err)
		}
	}()

	return nil
}

// Stop shuts down the proxy.
func (p *Proxy) Stop() error {
	if p.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return p.server.Shutdown(ctx)
	}
	return nil
}

// Traces returns all recorded trace entries.
func (p *Proxy) Traces() []TraceEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]TraceEntry{}, p.traces...)
}

// EnvVars returns the environment variables to inject into the TUT process.
func (p *Proxy) EnvVars(caCertPath string) []string {
	vars := []string{
		"HTTP_PROXY=http://" + p.Addr,
		"HTTPS_PROXY=http://" + p.Addr,
		"ALL_PROXY=http://" + p.Addr,
		"http_proxy=http://" + p.Addr,
		"https_proxy=http://" + p.Addr,
		"all_proxy=http://" + p.Addr,
		// Clear no_proxy so localhost/loopback requests are still routed via proxy.
		"NO_PROXY=",
		"no_proxy=",
	}
	if p.CA != nil {
		vars = append(vars, p.CA.EnvVars(caCertPath)...)
	}
	return vars
}

func (p *Proxy) handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method == "CONNECT" {
		p.handleConnect(w, r)
	} else {
		p.handleHTTP(w, r)
	}
}

func (p *Proxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	// HTTPS CONNECT tunneling with MITM
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

func (p *Proxy) handleDecryptedConnection(conn net.Conn, hostname string) {
	defer conn.Close()

	// Read HTTP request from TLS connection
	buf := make([]byte, 65536)
	n, err := conn.Read(buf)
	if err != nil {
		return
	}

	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(buf[:n])))
	if err != nil {
		// Not a valid HTTP request, ignore
		return
	}

	body, _ := io.ReadAll(req.Body)
	req.Body.Close()

	// Process through interception pipeline
	respBody, statusCode, err := p.interceptRequest(hostname, req.URL.Path, headerMap(req.Header), body)
	if err != nil {
		errResp := fmt.Sprintf("HTTP/1.1 502 Bad Gateway\r\nContent-Length: %d\r\n\r\n%s", len(err.Error()), err.Error())
		conn.Write([]byte(errResp))
		return
	}

	// Write response back
	resp := fmt.Sprintf("HTTP/1.1 %d OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s",
		statusCode, len(respBody), respBody)
	conn.Write([]byte(resp))
}

func (p *Proxy) handleHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadGateway)
		return
	}
	r.Body.Close()

	hostname := r.Host
	if strings.Contains(hostname, ":") {
		hostname = strings.Split(hostname, ":")[0]
	}

	respBody, statusCode, err := p.interceptRequest(hostname, r.URL.Path, headerMap(r.Header), body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	w.Write(respBody)
}

func (p *Proxy) interceptRequest(hostname, path string, headers map[string]string, body []byte) ([]byte, int, error) {
	start := time.Now()

	// Detect provider
	normalizer := providers.Detect(hostname, path, body, p.Normalizers)

	// Strip denylist headers
	cleanHeaders := fixture.StripDenylistHeaders(headers)

	// Normalize to canonical form
	canonical, err := normalizer.Normalize(hostname, path, cleanHeaders, body)
	if err != nil {
		return nil, 0, fmt.Errorf("normalization failed for %s: %w", normalizer.Family(), err)
	}

	// Hash canonical form
	canonicalBytes, err := fixture.CanonicalizeRequest(canonical)
	if err != nil {
		return nil, 0, fmt.Errorf("canonicalization failed: %w", err)
	}
	hash := fixture.HashCanonical(canonicalBytes)

	switch p.Mode {
	case ModeRecorded:
		return p.handleRecorded(canonical, canonicalBytes, hash, start)
	case ModeLive:
		return p.handleLive(hostname, path, headers, body, canonical, canonicalBytes, hash, start)
	default:
		return p.handlePassthrough(hostname, path, headers, body)
	}
}

func (p *Proxy) handleRecorded(canonical *providers.CanonicalRequest, canonicalBytes []byte, hash string, start time.Time) ([]byte, int, error) {
	f, err := p.Store.GetModelFixture(hash)
	if err != nil {
		return nil, 0, err
	}
	if f == nil {
		return nil, 0, &fixture.ErrFixtureMiss{
			FixtureType:   "model:" + canonical.Model,
			CanonicalHash: hash,
			CanonicalJSON: string(canonicalBytes),
			RecordCmd:     "GAUNTLET_MODEL_MODE=live gauntlet record --suite smoke",
		}
	}

	p.recordTrace(TraceEntry{
		Timestamp:      start,
		ProviderFamily: canonical.ProviderFamily,
		Model:          canonical.Model,
		CanonicalHash:  hash,
		FixtureHit:     true,
		DurationMs:     int(time.Since(start).Milliseconds()),
	})

	return f.Response, 200, nil
}

func (p *Proxy) handleLive(hostname, path string, headers map[string]string, body []byte, canonical *providers.CanonicalRequest, canonicalBytes []byte, hash string, start time.Time) ([]byte, int, error) {
	// Forward the original request
	url := "https://" + hostname + path
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("live request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read live response: %w", err)
	}

	// Redact before recording
	redactedResp, _ := p.Redactor.RedactJSON(respBody)

	// Record as fixture
	f := &fixture.ModelFixture{
		FixtureID:        hash,
		HashVersion:      1,
		CanonicalHash:    hash,
		ProviderFamily:   canonical.ProviderFamily,
		Model:            canonical.Model,
		CanonicalRequest: canonicalBytes,
		Response:         redactedResp,
		RecordedAt:       time.Now(),
		RecordedBy:       "live",
	}
	if err := p.Store.PutModelFixture(f); err != nil {
		log.Printf("WARN: failed to store fixture: %v", err)
	}

	p.recordTrace(TraceEntry{
		Timestamp:      start,
		ProviderFamily: canonical.ProviderFamily,
		Model:          canonical.Model,
		CanonicalHash:  hash,
		FixtureHit:     false,
		DurationMs:     int(time.Since(start).Milliseconds()),
	})

	return respBody, resp.StatusCode, nil
}

func (p *Proxy) handlePassthrough(hostname, path string, headers map[string]string, body []byte) ([]byte, int, error) {
	url := "https://" + hostname + path
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, err
}

func (p *Proxy) recordTrace(entry TraceEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.traces = append(p.traces, entry)
}

func (p *Proxy) tunnelDirect(clientConn net.Conn, hostname string) {
	defer clientConn.Close()

	serverConn, err := net.DialTimeout("tcp", hostname, 10*time.Second)
	if err != nil {
		return
	}
	defer serverConn.Close()

	go io.Copy(serverConn, clientConn)
	io.Copy(clientConn, serverConn)
}

func headerMap(h http.Header) map[string]string {
	m := make(map[string]string)
	for k, v := range h {
		if len(v) > 0 {
			m[k] = v[0]
		}
	}
	return m
}
