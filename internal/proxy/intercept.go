package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pmclSF/gauntlet/internal/fixture"
	"github.com/pmclSF/gauntlet/internal/proxy/providers"
)

// interceptRequest is the core interception pipeline. It detects the provider,
// normalizes the request to canonical form, hashes it, and dispatches to the
// appropriate mode handler (recorded, live, or passthrough).
func (p *Proxy) interceptRequest(method, hostname, path, rawQuery string, headers map[string]string, body []byte) ([]byte, int, error) {
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
		return p.handleLive(normalizer, method, hostname, path, rawQuery, headers, body, canonical, canonicalBytes, hash, start)
	default:
		return p.handlePassthrough(method, hostname, path, rawQuery, headers, body)
	}
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
