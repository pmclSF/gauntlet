// Package proxy implements the Gauntlet MITM HTTP/HTTPS proxy.
// It intercepts all model calls from the TUT and routes them through
// the fixture store for deterministic replay.
package proxy

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
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

// defaultTransport is the shared upstream transport for live/passthrough requests.
var defaultTransport = newUpstreamTransport()

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

// handleRequest is the top-level HTTP handler that dispatches CONNECT vs
// plain HTTP requests.
func (p *Proxy) handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method == "CONNECT" {
		p.handleConnect(w, r)
	} else {
		p.handleHTTP(w, r)
	}
}
