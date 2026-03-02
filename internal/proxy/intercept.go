package proxy

import "bytes"

// ReadRequest is a helper that wraps bytes.Reader as an io.ReadCloser
// for http.ReadRequest. Implemented here to avoid the unused import in proxy.go.
type readCloserReader struct {
	*bytes.Reader
}

func (r *readCloserReader) Close() error { return nil }
