package proxy

import "net/http"

// headerMap converts multi-value HTTP headers to single-value map,
// keeping only the first value for each key.
func headerMap(h http.Header) map[string]string {
	m := make(map[string]string)
	for k, v := range h {
		if len(v) > 0 {
			m[k] = v[0]
		}
	}
	return m
}
