package proxy

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"
)

const (
	defaultServerReadTimeout  = 30 * time.Second
	defaultServerWriteTimeout = 60 * time.Second
	defaultServerIdleTimeout  = 120 * time.Second
)

// Start begins listening for proxy connections.
func (p *Proxy) Start(ctx context.Context) error {
	p.server = &http.Server{
		Addr:           p.Addr,
		Handler:        http.HandlerFunc(p.handleRequest),
		MaxHeaderBytes: p.effectiveMaxHeaderBytes(),
		ReadTimeout:    defaultServerReadTimeout,
		WriteTimeout:   defaultServerWriteTimeout,
		IdleTimeout:    defaultServerIdleTimeout,
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

// Stop shuts down the proxy gracefully.
func (p *Proxy) Stop() error {
	if p.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return p.server.Shutdown(ctx)
	}
	return nil
}
