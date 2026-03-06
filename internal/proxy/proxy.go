// Package proxy implements the Gauntlet MITM HTTP/HTTPS proxy.
// It intercepts all model calls from the TUT and routes them through
// the fixture store for deterministic replay.
package proxy

import (
	"net"
	"net/http"
	"sync"

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

// UnknownTrafficPolicy controls how the proxy handles requests that do not
// match any known provider normalizer.
type UnknownTrafficPolicy string

const (
	// UnknownTrafficPassthrough forwards unknown traffic transparently.
	// This is the default for backward compatibility.
	UnknownTrafficPassthrough UnknownTrafficPolicy = "passthrough"
	// UnknownTrafficReject returns an error for unknown traffic.
	UnknownTrafficReject UnknownTrafficPolicy = "reject"
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
	Addr                 string
	Mode                 Mode
	UnknownTraffic       UnknownTrafficPolicy
	EnvPolicy            ProxyEnvPolicy
	Store                *fixture.Store
	Suite                string
	ScenarioSetSHA256    string
	MaxHeaderBytes       int
	MaxBodyBytes         int64
	MaxRequestsPerConn   int
	Normalizers          []providers.ProviderNormalizer
	CA                   *CA
	Redactor             *redaction.Redactor

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

// handleRequest is the top-level HTTP handler that dispatches CONNECT vs
// plain HTTP requests.
func (p *Proxy) handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method == "CONNECT" {
		p.handleConnect(w, r)
	} else {
		p.handleHTTP(w, r)
	}
}
