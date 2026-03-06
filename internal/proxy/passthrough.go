package proxy

import "context"

// handlePassthrough forwards a request to the upstream endpoint without
// recording. Used when GAUNTLET_MODEL_MODE=passthrough.
func (p *Proxy) handlePassthrough(method, hostname, path, rawQuery string, headers map[string]string, body []byte) ([]byte, int, error) {
	return defaultTransport.Forward(context.Background(), method, hostname, path, rawQuery, headers, body)
}
