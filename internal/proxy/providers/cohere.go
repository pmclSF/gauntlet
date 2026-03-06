package providers

import (
	"encoding/json"
	"fmt"
	"sort"
)

// CohereNormalizer handles requests to the Cohere API.
// Detected by: hostname == api.cohere.ai OR api.cohere.com
type CohereNormalizer struct{}

func (n *CohereNormalizer) Family() string { return "cohere" }

func (n *CohereNormalizer) Detect(hostname, path string, body []byte) bool {
	return hostname == "api.cohere.ai" || hostname == "api.cohere.com"
}

func (n *CohereNormalizer) Normalize(hostname, path string, headers map[string]string, body []byte) (*CanonicalRequest, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("cohere: failed to parse request body: %w", err)
	}

	cr := &CanonicalRequest{
		GauntletCanonicalVersion: 1,
		ProviderFamily:           "cohere",
		Extra:                    make(map[string]interface{}),
	}

	// Model
	if m, ok := raw["model"].(string); ok {
		cr.Model = m
	}

	// Preamble -> system
	if p, ok := raw["preamble"].(string); ok {
		cr.System = p
	}

	// Chat history -> messages
	if history, ok := raw["chat_history"].([]interface{}); ok {
		for _, h := range history {
			if msg, ok := h.(map[string]interface{}); ok {
				role, _ := msg["role"].(string)
				// Cohere uses "CHATBOT" and "USER"
				switch role {
				case "CHATBOT":
					role = "assistant"
				case "USER":
					role = "user"
				case "SYSTEM":
					role = "system"
				}
				message, _ := msg["message"].(string)
				cr.Messages = append(cr.Messages, CanonicalMessage{
					Role:    role,
					Content: message,
				})
			}
		}
	}

	// Current message (Cohere's "message" field is the latest user message)
	if msg, ok := raw["message"].(string); ok {
		cr.Messages = append(cr.Messages, CanonicalMessage{
			Role:    "user",
			Content: msg,
		})
	}

	// Tools
	if tools, ok := raw["tools"].([]interface{}); ok {
		for _, t := range tools {
			if tool, ok := t.(map[string]interface{}); ok {
				name, _ := tool["name"].(string)
				desc, _ := tool["description"].(string)
				params, _ := tool["parameter_definitions"].(map[string]interface{})
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
	if tp, ok := raw["p"]; ok {
		if f, ok := toFloat64(tp); ok {
			cr.Sampling.TopP = &f
		}
	}
	if stop, ok := raw["stop_sequences"].([]interface{}); ok {
		for _, s := range stop {
			if str, ok := s.(string); ok {
				cr.Sampling.Stop = append(cr.Sampling.Stop, str)
			}
		}
	}

	return cr, nil
}

func (n *CohereNormalizer) DenormalizeResponse(canonical []byte, providerFamily string) ([]byte, error) {
	return canonical, nil
}

func (n *CohereNormalizer) ExtractUsage(response []byte) (promptTokens int, completionTokens int) {
	return extractUsageTokens(
		response,
		[][]string{{"meta", "billed_units", "input_tokens"}, {"usage", "prompt_tokens"}},
		[][]string{{"meta", "billed_units", "output_tokens"}, {"usage", "completion_tokens"}},
	)
}

func (n *CohereNormalizer) NormalizeResponseForFixture(response []byte) ([]byte, error) {
	return response, nil
}
