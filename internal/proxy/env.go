package proxy

import "net"

// EnvVars returns the environment variables to inject into the TUT process
// so that all HTTP/HTTPS traffic is routed through the proxy.
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
