// Package providers defines the provider-agnostic canonical form and
// the ProviderNormalizer interface for converting raw HTTP requests
// from various LLM providers into that canonical form.
package providers

// CanonicalRequest is the provider-agnostic internal representation
// used for fixture hashing. All providers normalize to this form.
type CanonicalRequest struct {
	GauntletCanonicalVersion int                    `json:"gauntlet_canonical_version"`
	ProviderFamily           string                 `json:"provider_family"`
	Model                    string                 `json:"model"`
	System                   string                 `json:"system"`
	Messages                 []CanonicalMessage     `json:"messages"`
	Tools                    []CanonicalTool        `json:"tools"`
	Sampling                 CanonicalSampling      `json:"sampling"`
	Extra                    map[string]interface{} `json:"extra,omitempty"`
}

// CanonicalMessage is a provider-agnostic chat message.
type CanonicalMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []ContentBlock
}

// ContentBlock represents a structured content block within a message.
type ContentBlock struct {
	Type      string      `json:"type"`
	Text      string      `json:"text,omitempty"`
	ToolUseID string      `json:"tool_use_id,omitempty"`
	Name      string      `json:"name,omitempty"`
	Input     interface{} `json:"input,omitempty"`
	Output    interface{} `json:"output,omitempty"`
}

// CanonicalTool is a provider-agnostic tool definition.
type CanonicalTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// CanonicalSampling holds sampling parameters in canonical form.
type CanonicalSampling struct {
	Temperature *float64 `json:"temperature"`
	MaxTokens   *int     `json:"max_tokens"`
	TopP        *float64 `json:"top_p"`
	Stop        []string `json:"stop"`
}

// ProviderNormalizer converts raw HTTP request bodies and headers
// into the provider-agnostic CanonicalRequest form.
type ProviderNormalizer interface {
	// Family returns the provider family identifier (e.g. "openai_compatible").
	Family() string
	// Detect returns true if this normalizer handles the given request.
	Detect(hostname, path string, body []byte) bool
	// Normalize converts the raw request to canonical form.
	Normalize(hostname, path string, headers map[string]string, body []byte) (*CanonicalRequest, error)
	// DenormalizeResponse converts a canonical fixture response back to the
	// provider-specific response format for returning to the TUT.
	DenormalizeResponse(canonical []byte, providerFamily string) ([]byte, error)
}
