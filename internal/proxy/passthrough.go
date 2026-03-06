package proxy

import "context"

// handlePassthrough forwards a request to the upstream endpoint without
// recording. Used when GAUNTLET_MODEL_MODE=passthrough.
func (p *Proxy) handlePassthrough(ctx context.Context, method, hostname, path, rawQuery string, headers map[string]string, body []byte) (*interceptedResponse, error) {
	upstream, err := defaultTransport.Forward(ctx, method, hostname, path, rawQuery, headers, body)
	if err != nil {
		return nil, err
	}
	return &interceptedResponse{
		Body:    upstream.Body,
		Status:  upstream.StatusCode,
		Headers: upstream.Headers,
	}, nil
}
