package providers

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// OpenAICompatibleNormalizer handles requests to OpenAI, Azure OpenAI,
// Together AI, Fireworks, Groq, Ollama, vLLM, and other OpenAI-compatible endpoints.
// Detected by: path contains /chat/completions
type OpenAICompatibleNormalizer struct{}

func (n *OpenAICompatibleNormalizer) Family() string { return "openai_compatible" }

func (n *OpenAICompatibleNormalizer) Detect(hostname, path string, body []byte) bool {
	return strings.Contains(path, "/chat/completions")
}

func (n *OpenAICompatibleNormalizer) Normalize(hostname, path string, headers map[string]string, body []byte) (*CanonicalRequest, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("openai_compatible: failed to parse request body: %w", err)
	}

	cr := &CanonicalRequest{
		GauntletCanonicalVersion: 1,
		ProviderFamily:           "openai_compatible",
		Extra:                    make(map[string]interface{}),
	}

	// Model
	if m, ok := raw["model"].(string); ok {
		cr.Model = m
	}

	// Messages
	if msgs, ok := raw["messages"].([]interface{}); ok {
		for _, m := range msgs {
			if msg, ok := m.(map[string]interface{}); ok {
				role, _ := msg["role"].(string)
				content := msg["content"]

				// Extract system messages into the system field
				if role == "system" {
					if s, ok := content.(string); ok {
						if cr.System != "" {
							cr.System += "\n"
						}
						cr.System += s
						continue
					}
				}

				cr.Messages = append(cr.Messages, CanonicalMessage{
					Role:    role,
					Content: content,
				})
			}
		}
	}

	// Tools — sort by name for hash stability
	if tools, ok := raw["tools"].([]interface{}); ok {
		for _, t := range tools {
			if tool, ok := t.(map[string]interface{}); ok {
				fn, _ := tool["function"].(map[string]interface{})
				if fn == nil {
					continue
				}
				name, _ := fn["name"].(string)
				desc, _ := fn["description"].(string)
				params, _ := fn["parameters"].(map[string]interface{})
				cr.Tools = append(cr.Tools, CanonicalTool{
					Name:        name,
					Description: desc,
					Parameters:  params,
				})
			}
		}
		sort.Slice(cr.Tools, func(i, j int) bool {
			return cr.Tools[i].Name < cr.Tools[j].Name
		})
	}

	// Sampling parameters
	if t, ok := raw["temperature"]; ok {
		if f, ok := toFloat64(t); ok {
			cr.Sampling.Temperature = &f
		}
	}
	if mt, ok := raw["max_tokens"]; ok {
		if i, ok := toInt(mt); ok {
			cr.Sampling.MaxTokens = &i
		}
	}
	if tp, ok := raw["top_p"]; ok {
		if f, ok := toFloat64(tp); ok {
			cr.Sampling.TopP = &f
		}
	}
	if stop, ok := raw["stop"]; ok {
		switch v := stop.(type) {
		case string:
			cr.Sampling.Stop = []string{v}
		case []interface{}:
			for _, s := range v {
				if str, ok := s.(string); ok {
					cr.Sampling.Stop = append(cr.Sampling.Stop, str)
				}
			}
		}
	}

	// Preserve unknown fields (denylist approach)
	knownFields := map[string]bool{
		"model": true, "messages": true, "tools": true,
		"temperature": true, "max_tokens": true, "top_p": true, "stop": true,
		"response_format": true, "tool_choice": true,
	}
	denyFields := map[string]bool{
		"stream": true, "user": true, "n": true,
		"request_id": true, "session_id": true,
		"logit_bias": true, "presence_penalty": true, "frequency_penalty": true,
		"logprobs": true, "echo": true, "seed": true,
	}
	for k, v := range raw {
		if knownFields[k] || denyFields[k] {
			continue
		}
		if strings.HasSuffix(k, "_id") || strings.HasSuffix(k, "_ts") ||
			strings.HasSuffix(k, "_at") || strings.HasSuffix(k, "_timestamp") {
			continue
		}
		if strings.HasPrefix(k, "metadata") || strings.HasPrefix(k, "extra_headers") ||
			strings.HasPrefix(k, "http_client") {
			continue
		}
		cr.Extra[k] = v
	}

	// Handle response_format and tool_choice as known-but-extra
	if rf, ok := raw["response_format"]; ok {
		cr.Extra["response_format"] = rf
	}
	if tc, ok := raw["tool_choice"]; ok {
		cr.Extra["tool_choice"] = tc
	}

	return cr, nil
}

func (n *OpenAICompatibleNormalizer) DenormalizeResponse(canonical []byte, providerFamily string) ([]byte, error) {
	// In v1, fixtures store the raw provider response alongside canonical.
	// Return canonical as-is — the proxy stores both forms.
	return canonical, nil
}

func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	}
	return 0, false
}
