package providers

import (
	"encoding/json"
	"fmt"
)

// UnknownNormalizer is the fallback for unrecognized providers.
// It stores the raw request body as-is with a warning about hash instability.
type UnknownNormalizer struct{}

func (n *UnknownNormalizer) Family() string { return "unknown" }

func (n *UnknownNormalizer) Detect(hostname, path string, body []byte) bool {
	// Always matches — this is the fallback.
	return true
}

func (n *UnknownNormalizer) Normalize(hostname, path string, headers map[string]string, body []byte) (*CanonicalRequest, error) {
	var rawMap map[string]interface{}
	if err := json.Unmarshal(body, &rawMap); err != nil {
		return nil, fmt.Errorf("unknown provider: failed to parse request body as JSON: %w", err)
	}

	cr := &CanonicalRequest{
		GauntletCanonicalVersion: 1,
		ProviderFamily:           "unknown",
		Extra:                    rawMap,
	}

	// Try to extract model if present
	if m, ok := rawMap["model"].(string); ok {
		cr.Model = m
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
