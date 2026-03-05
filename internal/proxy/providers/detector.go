package providers

import (
	"net"
	"strings"
)

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
	if isLocalhost(hostname) && isLocalModelPath(path) {
		return &OpenAICompatibleNormalizer{}
	}
	for _, n := range normalizers {
		if n.Detect(hostname, path, body) {
			return n
		}
	}
	// Should never reach here since UnknownNormalizer always matches,
	// but return it as a safety fallback.
	return &UnknownNormalizer{}
}

// NormalizerForFamily returns a normalizer instance for a provider family.
func NormalizerForFamily(family string) ProviderNormalizer {
	switch strings.ToLower(strings.TrimSpace(family)) {
	case "openai_compatible":
		return &OpenAICompatibleNormalizer{}
	case "anthropic":
		return &AnthropicNormalizer{}
	case "google":
		return &GoogleNormalizer{}
	case "bedrock_converse":
		return &BedrockNormalizer{}
	case "cohere":
		return &CohereNormalizer{}
	default:
		return &UnknownNormalizer{}
	}
}

// isLocalhost returns true if the hostname is a loopback address.
func isLocalhost(hostname string) bool {
	h := strings.TrimSpace(hostname)
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	h = strings.TrimPrefix(h, "[")
	h = strings.TrimSuffix(h, "]")
	return h == "localhost" || h == "127.0.0.1" || h == "::1" || h == "0.0.0.0"
}

func isLocalModelPath(path string) bool {
	switch {
	case strings.HasPrefix(path, "/api/chat"):
		return true
	case strings.HasPrefix(path, "/api/generate"):
		return true
	case strings.HasPrefix(path, "/v1/chat/completions"):
		return true
	case strings.HasPrefix(path, "/v1/completions"):
		return true
	default:
		return false
	}
}
