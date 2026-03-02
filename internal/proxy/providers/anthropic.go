package providers

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// AnthropicNormalizer handles requests to the Anthropic Messages API.
// Detected by: hostname == "api.anthropic.com"
type AnthropicNormalizer struct{}

func (n *AnthropicNormalizer) Family() string { return "anthropic" }

func (n *AnthropicNormalizer) Detect(hostname, path string, body []byte) bool {
	if hostname == "api.anthropic.com" {
		return true
	}
	// Also detect by path + body structure
	if strings.Contains(path, "/v1/messages") {
		var raw map[string]interface{}
		if err := json.Unmarshal(body, &raw); err == nil {
			if m, ok := raw["model"].(string); ok && strings.HasPrefix(m, "claude-") {
				return true
			}
		}
	}
	return false
}

func (n *AnthropicNormalizer) Normalize(hostname, path string, headers map[string]string, body []byte) (*CanonicalRequest, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("anthropic: failed to parse request body: %w", err)
	}

	cr := &CanonicalRequest{
		GauntletCanonicalVersion: 1,
		ProviderFamily:           "anthropic",
		Extra:                    make(map[string]interface{}),
	}

	// Model
	if m, ok := raw["model"].(string); ok {
		cr.Model = m
	}

	// System prompt (Anthropic uses a top-level "system" field)
	switch sys := raw["system"].(type) {
	case string:
		cr.System = sys
	case []interface{}:
		// Anthropic can have system as array of content blocks
		var parts []string
		for _, block := range sys {
			if b, ok := block.(map[string]interface{}); ok {
				if text, ok := b["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		cr.System = strings.Join(parts, "\n")
	}

	// Messages
	if msgs, ok := raw["messages"].([]interface{}); ok {
		for _, m := range msgs {
			if msg, ok := m.(map[string]interface{}); ok {
				role, _ := msg["role"].(string)
				content := msg["content"]
				cr.Messages = append(cr.Messages, CanonicalMessage{
					Role:    role,
					Content: content,
				})
			}
		}
	}

	// Tools — sort by name
	if tools, ok := raw["tools"].([]interface{}); ok {
		for _, t := range tools {
			if tool, ok := t.(map[string]interface{}); ok {
				name, _ := tool["name"].(string)
				desc, _ := tool["description"].(string)
				schema, _ := tool["input_schema"].(map[string]interface{})
				cr.Tools = append(cr.Tools, CanonicalTool{
					Name:        name,
					Description: desc,
					Parameters:  schema,
				})
			}
		}
		sort.Slice(cr.Tools, func(i, j int) bool {
			return cr.Tools[i].Name < cr.Tools[j].Name
		})
	}

	// Sampling
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
	if stop, ok := raw["stop_sequences"]; ok {
		if arr, ok := stop.([]interface{}); ok {
			for _, s := range arr {
				if str, ok := s.(string); ok {
					cr.Sampling.Stop = append(cr.Sampling.Stop, str)
				}
			}
		}
	}

	// Preserve unknown fields (denylist approach)
	knownFields := map[string]bool{
		"model": true, "messages": true, "system": true, "tools": true,
		"temperature": true, "max_tokens": true, "top_p": true, "stop_sequences": true,
		"tool_choice": true,
	}
	denyFields := map[string]bool{
		"stream": true, "metadata": true,
	}
	for k, v := range raw {
		if knownFields[k] || denyFields[k] {
			continue
		}
		if strings.HasSuffix(k, "_id") || strings.HasSuffix(k, "_ts") ||
			strings.HasSuffix(k, "_at") || strings.HasSuffix(k, "_timestamp") {
			continue
		}
		cr.Extra[k] = v
	}

	if tc, ok := raw["tool_choice"]; ok {
		cr.Extra["tool_choice"] = tc
	}

	return cr, nil
}

func (n *AnthropicNormalizer) DenormalizeResponse(canonical []byte, providerFamily string) ([]byte, error) {
	return canonical, nil
}
