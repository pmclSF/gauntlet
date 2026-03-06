package proxy

import "net"

// ProxyEnvPolicy controls how the proxy shapes NO_PROXY for the TUT process.
type ProxyEnvPolicy string

const (
	// ProxyEnvForceAll clears NO_PROXY so all traffic (including localhost)
	// is routed through the proxy. This is the default and is required for
	// intercepting local model endpoints like Ollama (localhost:11434).
	//
	// Tradeoff: local sidecars, health checks, and DB admin ports will also
	// be routed through the proxy. Use UnknownTrafficPassthrough policy to
	// let non-model traffic pass through transparently.
	ProxyEnvForceAll ProxyEnvPolicy = "force_all"

	// ProxyEnvPreserve keeps the existing NO_PROXY value from the host
	// environment. Model endpoints on localhost will NOT be intercepted
	// unless the user explicitly configures NO_PROXY to exclude them.
	// Use this when the TUT connects to local services that should not
	// traverse the proxy.
	ProxyEnvPreserve ProxyEnvPolicy = "preserve"
)

// EnvVars returns the environment variables to inject into the TUT process
// so that all HTTP/HTTPS traffic is routed through the proxy.
//
// NO_PROXY behavior depends on the EnvPolicy:
//   - force_all (default): clears NO_PROXY so localhost/loopback traffic is
//     intercepted. Required for local model endpoints (e.g. Ollama).
//   - preserve: does not modify NO_PROXY; existing host env is inherited.
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

	// Apply NO_PROXY policy. Default (zero value or force_all) clears
	// NO_PROXY to route all traffic through the proxy.
	if p.EnvPolicy != ProxyEnvPreserve {
		vars = append(vars,
			"NO_PROXY=",
			"no_proxy=",
		)
	}

	if p.CA != nil {
		vars = append(vars, p.CA.EnvVars(caCertPath)...)
	}
	return vars
}
