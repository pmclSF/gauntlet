package providers

import "encoding/json"

// UnknownNormalizer is the fallback for unrecognized providers.
// It stores the raw request body as-is with a warning about hash instability.
type UnknownNormalizer struct{}

func (n *UnknownNormalizer) Family() string { return "unknown" }

func (n *UnknownNormalizer) Detect(hostname, path string, body []byte) bool {
	// Always matches — this is the fallback.
	return true
}

func (n *UnknownNormalizer) Normalize(hostname, path string, headers map[string]string, body []byte) (*CanonicalRequest, error) {
	cr := &CanonicalRequest{
		GauntletCanonicalVersion: 1,
		ProviderFamily:           "unknown",
	}

	// Try to parse as JSON; if the body is non-JSON or empty, store a
	// raw-body fallback so live/passthrough modes don't fail.
	if len(body) > 0 {
		var rawMap map[string]interface{}
		if err := json.Unmarshal(body, &rawMap); err != nil {
			// Non-JSON body — store as opaque string in Extra for hashing
			cr.Extra = map[string]interface{}{
				"_raw_body": string(body),
				"_hostname": hostname,
				"_path":     path,
			}
			return cr, nil
		}
		cr.Extra = rawMap
		// Try to extract model if present
		if m, ok := rawMap["model"].(string); ok {
			cr.Model = m
		}
	} else {
		// Empty body (e.g. GET requests) — use path for differentiation
		cr.Extra = map[string]interface{}{
			"_hostname": hostname,
			"_path":     path,
		}
	}

	return cr, nil
}

func (n *UnknownNormalizer) DenormalizeResponse(canonical []byte, providerFamily string) ([]byte, error) {
	return canonical, nil
}

func (n *UnknownNormalizer) ExtractUsage(response []byte) (promptTokens int, completionTokens int) {
	return 0, 0
}

func (n *UnknownNormalizer) NormalizeResponseForFixture(response []byte) ([]byte, error) {
	return response, nil
}
