package providers

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// OpenAICompatibleNormalizer handles requests to OpenAI, Azure OpenAI,
// Together AI, Fireworks, Groq, Ollama, vLLM, and other OpenAI-compatible endpoints.
type OpenAICompatibleNormalizer struct{}

func (n *OpenAICompatibleNormalizer) Family() string { return "openai_compatible" }

func (n *OpenAICompatibleNormalizer) Detect(hostname, path string, body []byte) bool {
	return strings.Contains(path, "/chat/completions") ||
		strings.Contains(path, "/v1/completions")
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
		cr.Model = strings.TrimSpace(m)
	}

	// /api/generate-style prompt payloads (Ollama native)
	if prompt, ok := raw["prompt"].(string); ok && strings.TrimSpace(prompt) != "" {
		cr.Messages = append(cr.Messages, CanonicalMessage{
			Role:    "user",
			Content: prompt,
		})
	}
	if sys, ok := raw["system"].(string); ok && strings.TrimSpace(sys) != "" {
		cr.System = sys
	}

	// Messages
	if msgs, ok := raw["messages"].([]interface{}); ok {
		for _, m := range msgs {
			msg, ok := m.(map[string]interface{})
			if !ok {
				continue
			}
			role, _ := msg["role"].(string)

			content := normalizeOpenAIMessageContent(msg)

			// Extract system messages into the system field.
			if role == "system" {
				if s, ok := content.(string); ok {
					if cr.System != "" {
						cr.System += "\n"
					}
					cr.System += s
					continue
				}
				if parts, ok := content.([]CanonicalContentPart); ok && len(parts) > 0 {
					var sysParts []string
					for _, part := range parts {
						if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
							sysParts = append(sysParts, part.Text)
						}
					}
					if len(sysParts) > 0 {
						if cr.System != "" {
							cr.System += "\n"
						}
						cr.System += strings.Join(sysParts, "\n")
						continue
					}
				}
			}

			cr.Messages = append(cr.Messages, CanonicalMessage{
				Role:    role,
				Content: content,
			})
		}
	}

	// Tools — sort by name for hash stability
	if tools, ok := raw["tools"].([]interface{}); ok {
		for _, t := range tools {
			tool, ok := t.(map[string]interface{})
			if !ok {
				continue
			}
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
		"prompt": true, "system": true,
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

func (n *OpenAICompatibleNormalizer) ExtractUsage(response []byte) (promptTokens int, completionTokens int) {
	return extractUsageTokens(
		response,
		[][]string{
			{"prompt_tokens"},
			{"usage", "prompt_tokens"},
			{"usage", "input_tokens"},
			{"prompt_eval_count"},
		},
		[][]string{
			{"completion_tokens"},
			{"usage", "completion_tokens"},
			{"usage", "output_tokens"},
			{"eval_count"},
		},
	)
}

func (n *OpenAICompatibleNormalizer) NormalizeResponseForFixture(response []byte) ([]byte, error) {
	var root map[string]interface{}
	if err := json.Unmarshal(response, &root); err != nil {
		return response, nil
	}

	// Ollama-native responses include per-run timing and counter fields that
	// are non-deterministic and should not be fixture-stable.
	if looksLikeOllamaResponse(root) {
		promptTokens, _ := toInt(root["prompt_eval_count"])
		completionTokens, _ := toInt(root["eval_count"])
		for _, key := range []string{
			"created_at",
			"total_duration",
			"load_duration",
			"prompt_eval_duration",
			"eval_duration",
			"eval_count",
			"prompt_eval_count",
		} {
			delete(root, key)
		}
		if promptTokens > 0 {
			root["prompt_tokens"] = promptTokens
		}
		if completionTokens > 0 {
			root["completion_tokens"] = completionTokens
		}
	}

	out, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("openai_compatible: failed to normalize response: %w", err)
	}
	return out, nil
}

func looksLikeOllamaResponse(root map[string]interface{}) bool {
	if _, ok := root["message"]; ok {
		return true
	}
	if _, ok := root["response"]; ok {
		return true
	}
	if _, ok := root["prompt_eval_count"]; ok {
		return true
	}
	if _, ok := root["eval_count"]; ok {
		return true
	}
	return false
}

func normalizeOpenAIMessageContent(msg map[string]interface{}) interface{} {
	var parts []CanonicalContentPart

	switch content := msg["content"].(type) {
	case string:
		if strings.TrimSpace(content) != "" {
			parts = append(parts, CanonicalContentPart{Type: "text", Text: content})
		}
	case []interface{}:
		for _, rawPart := range content {
			part, ok := rawPart.(map[string]interface{})
			if !ok {
				continue
			}
			kind, _ := part["type"].(string)
			switch kind {
			case "text":
				text, _ := part["text"].(string)
				parts = append(parts, CanonicalContentPart{Type: "text", Text: text})
			case "image_url":
				imageURL := extractOpenAIImageURL(part["image_url"])
				if strings.TrimSpace(imageURL) == "" {
					continue
				}
				if hash, mime, ok := hashFromDataURI(imageURL); ok {
					parts = append(parts, CanonicalContentPart{
						Type:      "image",
						ImageHash: hash,
						MimeType:  mime,
					})
				} else {
					parts = append(parts, CanonicalContentPart{
						Type:      "image",
						ImageHash: imageURL,
					})
				}
			}
		}
	}

	// Ollama-native messages may carry base64 images at message.images[].
	if images, ok := msg["images"].([]interface{}); ok {
		for _, rawImage := range images {
			encoded, ok := rawImage.(string)
			if !ok {
				continue
			}
			decoded, err := decodeBase64Any(encoded)
			if err != nil {
				continue
			}
			sum := sha256.Sum256(decoded)
			parts = append(parts, CanonicalContentPart{
				Type:      "image",
				ImageHash: hex.EncodeToString(sum[:]),
			})
		}
	}

	return collapseCanonicalParts(parts)
}

func collapseCanonicalParts(parts []CanonicalContentPart) interface{} {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 && parts[0].Type == "text" {
		return parts[0].Text
	}
	return parts
}

func extractOpenAIImageURL(raw interface{}) string {
	switch typed := raw.(type) {
	case string:
		return typed
	case map[string]interface{}:
		url, _ := typed["url"].(string)
		return url
	default:
		return ""
	}
}

func hashFromDataURI(uri string) (hash string, mimeType string, ok bool) {
	if !strings.HasPrefix(uri, "data:") {
		return "", "", false
	}
	marker := ";base64,"
	idx := strings.Index(uri, marker)
	if idx <= len("data:") {
		return "", "", false
	}
	mimeType = strings.TrimSpace(uri[len("data:"):idx])
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	decoded, err := decodeBase64Any(uri[idx+len(marker):])
	if err != nil {
		return "", "", false
	}
	sum := sha256.Sum256(decoded)
	return hex.EncodeToString(sum[:]), mimeType, true
}

func decodeBase64Any(raw string) ([]byte, error) {
	sanitized := strings.TrimSpace(raw)
	sanitized = strings.ReplaceAll(sanitized, "\n", "")
	sanitized = strings.ReplaceAll(sanitized, "\r", "")

	if decoded, err := base64.StdEncoding.DecodeString(sanitized); err == nil {
		return decoded, nil
	}

	missingPad := len(sanitized) % 4
	if missingPad != 0 {
		sanitized += strings.Repeat("=", 4-missingPad)
	}
	if decoded, err := base64.StdEncoding.DecodeString(sanitized); err == nil {
		return decoded, nil
	}

	return base64.RawStdEncoding.DecodeString(strings.TrimRight(sanitized, "="))
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
