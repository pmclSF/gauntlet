package providers

import "strings"

// AllNormalizers returns the full set of provider normalizers in detection
// priority order. More specific rules come first.
func AllNormalizers() []ProviderNormalizer {
	return []ProviderNormalizer{
		// Explicit OpenAI-compatible path match should win before body-heuristic
		// Anthropic detection to avoid ambiguity on custom gateways.
		&OpenAICompatibleNormalizer{},
		&AnthropicNormalizer{},
		&GoogleNormalizer{},
		&BedrockNormalizer{},
		&CohereNormalizer{},
		&UnknownNormalizer{},
	}
}

// Detect identifies the provider family from an intercepted request.
// Returns the matching normalizer. Detection order matters — more specific first.
// Always returns a normalizer (falls back to UnknownNormalizer).
func Detect(hostname, path string, body []byte, normalizers []ProviderNormalizer) ProviderNormalizer {
	for _, n := range normalizers {
		if n.Detect(hostname, path, body) {
			return n
		}
	}
	// Should never reach here since UnknownNormalizer always matches,
	// but return it as a safety fallback.
	return &UnknownNormalizer{}
}

// isLocalhost returns true if the hostname is a loopback address.
func isLocalhost(hostname string) bool {
	h := strings.Split(hostname, ":")[0]
	return h == "localhost" || h == "127.0.0.1" || h == "::1" || h == "0.0.0.0"
}
