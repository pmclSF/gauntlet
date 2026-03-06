// Package proxy implements the Gauntlet MITM HTTP/HTTPS proxy.
// It intercepts all model calls from the TUT and routes them through
// the fixture store for deterministic replay.
package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/pmclSF/gauntlet/internal/fixture"
	"github.com/pmclSF/gauntlet/internal/proxy/providers"
	"github.com/pmclSF/gauntlet/internal/redaction"
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

const (
	defaultMaxHeaderBytes           = 64 * 1024
	defaultMaxBodyBytes       int64 = 2 * 1024 * 1024
	defaultMaxRequestsPerConn       = 100
	http2ClientPreface              = "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"
)

type proxyRequestError struct {
	StatusCode int
	Code       string
	Message    string
	Cause      error
}

func (e *proxyRequestError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *proxyRequestError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type proxyErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// TraceWriter receives trace events for intercepted calls.
type TraceWriter interface {
	WriteTrace(event TraceEntry)
}

// TraceEntry is a single intercepted call record.
type TraceEntry struct {
	Timestamp        time.Time `json:"timestamp"`
	ProviderFamily   string    `json:"provider_family"`
	Model            string    `json:"model"`
	CanonicalHash    string    `json:"canonical_hash"`
	FixtureHit       bool      `json:"fixture_hit"`
	DurationMs       int       `json:"duration_ms"`
	PromptTokens     int       `json:"prompt_tokens,omitempty"`
	CompletionTokens int       `json:"completion_tokens,omitempty"`
}

// directClient bypasses HTTP_PROXY/HTTPS_PROXY env vars to prevent the proxy
// from routing live/passthrough requests through itself in an infinite loop.
var directClient = &http.Client{
	Transport: &http.Transport{
		Proxy: nil,
	},
}

// Proxy is the local MITM HTTP/HTTPS proxy.
type Proxy struct {
	Addr               string
	Mode               Mode
	Store              *fixture.Store
	Suite              string
	ScenarioSetSHA256  string
	MaxHeaderBytes     int
	MaxBodyBytes       int64
	MaxRequestsPerConn int
	Normalizers        []providers.ProviderNormalizer
	CA                 *CA
	Redactor           *redaction.Redactor

	server   *http.Server
	mu       sync.Mutex
	traces   []TraceEntry
	listener net.Listener
}

// NewProxy creates a new proxy with default settings.
func NewProxy(addr string, mode Mode, store *fixture.Store, ca *CA) *Proxy {
	return &Proxy{
		Addr:               addr,
		Mode:               mode,
		Store:              store,
		MaxHeaderBytes:     defaultMaxHeaderBytes,
		MaxBodyBytes:       defaultMaxBodyBytes,
		MaxRequestsPerConn: defaultMaxRequestsPerConn,
		Normalizers:        providers.AllNormalizers(),
		CA:                 ca,
		Redactor:           redaction.DefaultRedactor(),
	}
}

// Start begins listening for proxy connections.
func (p *Proxy) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", p.handleHTTP)

	p.server = &http.Server{
		Addr:           p.Addr,
		Handler:        http.HandlerFunc(p.handleRequest),
		MaxHeaderBytes: p.effectiveMaxHeaderBytes(),
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
	proxyPort := ""
	if _, port, err := net.SplitHostPort(p.Addr); err == nil {
		proxyPort = port
	}

	vars := []string{
		"HTTP_PROXY=http://" + p.Addr,
		"HTTPS_PROXY=http://" + p.Addr,
		"ALL_PROXY=http://" + p.Addr,
		"http_proxy=http://" + p.Addr,
		"https_proxy=http://" + p.Addr,
		"all_proxy=http://" + p.Addr,
	}
	if proxyPort != "" {
		vars = append(vars, "GAUNTLET_PROXY_PORT="+proxyPort)
	}
	vars = append(vars,
		// Clear no_proxy so localhost/loopback requests are still routed via proxy.
		"NO_PROXY=",
		"no_proxy=",
	)
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

func (p *Proxy) handleDecryptedConnection(conn net.Conn, hostname string) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	requestCount := 0

	// Loop to support HTTP keep-alive (multiple requests per connection).
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
		respBody, statusCode, interceptErr := p.interceptRequest(hostname, req.URL.Path, headerMap(req.Header), body)
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

	respBody, statusCode, err := p.interceptRequest(hostname, r.URL.Path, headerMap(r.Header), body)
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

func (p *Proxy) interceptRequest(hostname, path string, headers map[string]string, body []byte) ([]byte, int, error) {
	start := time.Now()

	// Detect provider
	normalizer := providers.Detect(hostname, path, body, p.Normalizers)

	// Strip denylist headers
	cleanHeaders := fixture.StripDenylistHeaders(headers)

	// Normalize to canonical form
	canonical, err := normalizer.Normalize(hostname, path, cleanHeaders, body)
	if err != nil {
		if isMalformedJSONErr(err) {
			return nil, 0, &proxyRequestError{
				StatusCode: http.StatusBadRequest,
				Code:       "malformed_json_request",
				Message:    fmt.Sprintf("malformed JSON request body for provider %s", normalizer.Family()),
				Cause:      err,
			}
		}
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
		return p.handleRecorded(normalizer, canonical, canonicalBytes, hash, start)
	case ModeLive:
		return p.handleLive(normalizer, hostname, path, headers, body, canonical, canonicalBytes, hash, start)
	default:
		return p.handlePassthrough(hostname, path, headers, body)
	}
}

func (p *Proxy) handleRecorded(normalizer providers.ProviderNormalizer, canonical *providers.CanonicalRequest, canonicalBytes []byte, hash string, start time.Time) ([]byte, int, error) {
	f, err := p.Store.GetModelFixture(hash)
	if err != nil {
		return nil, 0, err
	}
	if f == nil {
		candidates, _ := p.Store.NearestModelFixtureCandidates(canonical.ProviderFamily, canonical.Model, hash, 3)
		modelVersionHint := p.modelVersionHint(canonicalBytes, canonical.Model, candidates)
		return nil, 0, &fixture.ErrFixtureMiss{
			FixtureType:      "model:" + canonical.Model,
			ProviderFamily:   canonical.ProviderFamily,
			Model:            canonical.Model,
			CanonicalHash:    hash,
			CanonicalJSON:    string(canonicalBytes),
			RecordCmd:        "GAUNTLET_MODEL_MODE=live gauntlet record --suite smoke",
			Candidates:       candidates,
			ModelVersionHint: modelVersionHint,
		}
	}
	normalizedResponse, err := normalizer.NormalizeResponseForFixture(f.Response)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to normalize recorded response: %w", err)
	}
	promptTokens, completionTokens := normalizer.ExtractUsage(normalizedResponse)

	p.recordTrace(TraceEntry{
		Timestamp:        start,
		ProviderFamily:   canonical.ProviderFamily,
		Model:            canonical.Model,
		CanonicalHash:    hash,
		FixtureHit:       true,
		DurationMs:       int(time.Since(start).Milliseconds()),
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
	})

	return normalizedResponse, 200, nil
}

func (p *Proxy) handleLive(normalizer providers.ProviderNormalizer, hostname, path string, headers map[string]string, body []byte, canonical *providers.CanonicalRequest, canonicalBytes []byte, hash string, start time.Time) ([]byte, int, error) {
	// Normalize streaming requests so recording captures single-response fixtures.
	path, body = stripStreamFlag(path, canonical.ProviderFamily, body)

	url := "https://" + hostname + path
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := directClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("live request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read live response: %w", err)
	}
	promptTokens, completionTokens := normalizer.ExtractUsage(respBody)
	normalizedResponse, err := normalizer.NormalizeResponseForFixture(respBody)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to normalize live response: %w", err)
	}
	if err := fixture.ValidateModelResponse(canonical.ProviderFamily, normalizedResponse); err != nil {
		return nil, 0, fmt.Errorf("model response schema validation failed: %w", err)
	}

	// Redact before recording
	redactedResp, _ := p.Redactor.RedactJSON(normalizedResponse)

	// Record as fixture
	f := &fixture.ModelFixture{
		FixtureID:         hash,
		HashVersion:       1,
		CanonicalHash:     hash,
		ProviderFamily:    canonical.ProviderFamily,
		Model:             canonical.Model,
		CanonicalRequest:  canonicalBytes,
		Response:          redactedResp,
		RecordedAt:        time.Now(),
		RecordedBy:        "live",
		Provenance:        fixture.BuildProvenance(headers, "proxy_live"),
		Suite:             p.Suite,
		ScenarioSetSHA256: p.ScenarioSetSHA256,
	}
	if err := p.Store.PutModelFixture(f); err != nil {
		log.Printf("WARN: failed to store fixture: %v", err)
	}

	p.recordTrace(TraceEntry{
		Timestamp:        start,
		ProviderFamily:   canonical.ProviderFamily,
		Model:            canonical.Model,
		CanonicalHash:    hash,
		FixtureHit:       false,
		DurationMs:       int(time.Since(start).Milliseconds()),
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
	})

	return normalizedResponse, resp.StatusCode, nil
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

	resp, err := directClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, err
}

func (p *Proxy) modelVersionHint(requestedCanonical []byte, requestedModel string, candidates []fixture.FixtureMissCandidate) string {
	recordSuite := strings.TrimSpace(p.Suite)
	if recordSuite == "" {
		recordSuite = "smoke"
	}
	requestedModel = strings.TrimSpace(requestedModel)

	for _, candidate := range candidates {
		recordedModel := strings.TrimSpace(candidate.Model)
		if recordedModel == "" || strings.EqualFold(recordedModel, requestedModel) {
			continue
		}
		recordedFixture, err := p.Store.GetModelFixture(candidate.CanonicalHash)
		if err != nil || recordedFixture == nil || len(recordedFixture.CanonicalRequest) == 0 {
			continue
		}
		match, cmpErr := canonicalEquivalentIgnoringModel(requestedCanonical, recordedFixture.CanonicalRequest)
		if cmpErr != nil || !match {
			continue
		}
		return fmt.Sprintf(
			"may be a model version change: recorded with %s, requesting %s. Run: gauntlet record --suite %s",
			recordedModel,
			requestedModel,
			recordSuite,
		)
	}
	return ""
}

func canonicalEquivalentIgnoringModel(leftCanonical, rightCanonical []byte) (bool, error) {
	var left map[string]interface{}
	if err := json.Unmarshal(leftCanonical, &left); err != nil {
		return false, err
	}
	var right map[string]interface{}
	if err := json.Unmarshal(rightCanonical, &right); err != nil {
		return false, err
	}
	delete(left, "model")
	delete(right, "model")
	return reflect.DeepEqual(left, right), nil
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

	go func() {
		_, _ = io.Copy(serverConn, clientConn)
	}()
	_, _ = io.Copy(clientConn, serverConn)
}

// stripStreamFlag normalizes provider streaming requests for fixture recording.
// For OpenAI-compatible APIs, stream=true is removed (or forced false for
// Ollama-native /api endpoints). For Gemini, streaming path suffixes are
// rewritten to non-streaming equivalents.
func stripStreamFlag(path, providerFamily string, body []byte) (string, []byte) {
	if strings.EqualFold(strings.TrimSpace(providerFamily), "google") {
		return providers.RewriteGeminiStreamingPath(path), body
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(body, &parsed); err != nil {
		return path, body
	}
	raw, ok := parsed["stream"]
	if !ok {
		return path, body
	}
	val := strings.TrimSpace(string(raw))
	if val != "true" {
		return path, body
	}
	if strings.HasPrefix(path, "/api/chat") || strings.HasPrefix(path, "/api/generate") {
		parsed["stream"] = json.RawMessage("false")
	} else {
		delete(parsed, "stream")
	}
	// stream_options is only valid when stream=true on OpenAI-compatible APIs.
	// Remove it alongside stream to keep live recording requests valid.
	delete(parsed, "stream_options")
	out, err := json.Marshal(parsed)
	if err != nil {
		return path, body
	}
	return path, out
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

func (p *Proxy) writeConnError(conn net.Conn, err error) {
	statusCode, payload := proxyErrorPayload(err)
	resp := &http.Response{
		StatusCode:    statusCode,
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        make(http.Header),
		Body:          io.NopCloser(bytes.NewReader(payload)),
		ContentLength: int64(len(payload)),
	}
	resp.Header.Set("Content-Type", "application/json")
	resp.Header.Set("Connection", "close")
	_ = resp.Write(conn)
}

func (p *Proxy) writeHTTPError(w http.ResponseWriter, err error) {
	statusCode, payload := proxyErrorPayload(err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_, _ = w.Write(payload)
}

func proxyErrorPayload(err error) (int, []byte) {
	statusCode := http.StatusBadGateway
	code := "proxy_intercept_failed"
	message := "proxy interception failed"

	var perr *proxyRequestError
	if errors.As(err, &perr) {
		statusCode = perr.StatusCode
		code = perr.Code
		message = perr.Message
	} else if _, ok := err.(*fixture.ErrFixtureMiss); ok {
		code = "fixture_miss"
		message = err.Error()
	} else if err != nil {
		message = err.Error()
	}

	if code == "" {
		code = "proxy_intercept_failed"
	}
	if message == "" {
		message = "proxy interception failed"
	}

	payload, marshalErr := json.Marshal(proxyErrorResponse{
		Error: message,
		Code:  code,
	})
	if marshalErr != nil {
		return statusCode, []byte(`{"error":"proxy interception failed","code":"proxy_intercept_failed"}`)
	}
	return statusCode, payload
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

func isMalformedJSONErr(err error) bool {
	if err == nil {
		return false
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return true
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return true
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "failed to parse request body")
}
